package pictures

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"crdx.org/oh/internal/util/imageutil"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
)

const (
	directoryName = "images"

	MaxStoredBytes = 16 << 20
	DisplayWidth   = 800
)

func GetDirectory(sessionDirectory string) string {
	return filepath.Join(sessionDirectory, directoryName)
}

func Store(sessionDirectory string, ensureSession func() error, picture tool.Image) (*agent.Picture, error) {
	if len(picture.Data) > MaxStoredBytes || !imageutil.IsSupported(picture.MediaType) {
		return nil, fmt.Errorf("the picture cannot be stored: %d bytes of %s", len(picture.Data), picture.MediaType)
	}

	digest := hex.EncodeToString(digestOf(picture.Data))

	directory, err := prepare(sessionDirectory, ensureSession)
	if err != nil {
		return nil, err
	}

	if err := write(filepath.Join(directory, digest+extensionFor(picture.MediaType)), picture.Data); err != nil {
		return nil, err
	}

	displayCopy, width, height, err := shrink(picture.Data)
	if err != nil {
		return nil, err
	}

	if err := write(displayPath(directory, digest), displayCopy); err != nil {
		return nil, err
	}

	return &agent.Picture{
		Digest:    digest,
		MediaType: picture.MediaType,
		Width:     width,
		Height:    height,
	}, nil
}

type Drawing struct {
	Path   string
	Width  int
	Height int
}

func Prepare(sessionDirectory string, reference *agent.Picture) (Drawing, bool) {
	if reference == nil || !isDigest(reference.Digest) {
		return Drawing{}, false
	}

	directory := GetDirectory(sessionDirectory)
	path := displayPath(directory, reference.Digest)

	if _, err := os.Stat(path); err == nil && reference.Width > 0 && reference.Height > 0 {
		return Drawing{Path: path, Width: reference.Width, Height: reference.Height}, true
	}

	return rebuild(directory, path, reference)
}

func rebuild(directory string, path string, reference *agent.Picture) (Drawing, bool) {
	originalPath := filepath.Join(directory, reference.Digest+extensionFor(reference.MediaType))

	original, err := os.ReadFile(originalPath) //nolint:gosec // a path built from a stored digest
	if err != nil {
		return Drawing{}, false
	}

	displayCopy, width, height, err := shrink(original)
	if err != nil {
		return Drawing{}, false
	}

	if err := write(path, displayCopy); err != nil {
		return Drawing{}, false
	}

	return Drawing{Path: path, Width: width, Height: height}, true
}

func Read(path string) ([]byte, bool) {
	data, err := os.ReadFile(path) //nolint:gosec // a path built from a stored digest
	if err != nil {
		return nil, false
	}

	return data, true
}

func isDigest(text string) bool {
	if len(text) != sha256.Size*2 {
		return false
	}

	_, err := hex.DecodeString(text)

	return err == nil
}

func displayPath(directory string, digest string) string {
	return filepath.Join(directory, digest+"-w"+strconv.Itoa(DisplayWidth)+".png")
}

func digestOf(data []byte) []byte {
	sum := sha256.Sum256(data)

	return sum[:]
}

func extensionFor(mediaType string) string {
	switch mediaType {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}

func shrink(data []byte) ([]byte, int, int, error) {
	picture, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("decode the picture: %w", err)
	}

	bounds := picture.Bounds()
	width, height := bounds.Dx(), bounds.Dy()

	if width > DisplayWidth {
		height = max(height*DisplayWidth/width, 1)
		width = DisplayWidth

		smaller := image.NewRGBA(image.Rect(0, 0, width, height))
		draw.CatmullRom.Scale(smaller, smaller.Bounds(), picture, bounds, draw.Over, nil)
		picture = smaller
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, picture); err != nil {
		return nil, 0, 0, fmt.Errorf("encode the picture: %w", err)
	}

	return buffer.Bytes(), width, height, nil
}

func prepare(sessionDirectory string, ensureSession func() error) (string, error) {
	if err := ensureSession(); err != nil {
		return "", fmt.Errorf("prepare the session directory: %w", err)
	}

	directory := GetDirectory(sessionDirectory)

	info, err := os.Lstat(directory)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.Mkdir(directory, 0o700); err != nil {
			return "", fmt.Errorf("prepare the pictures directory: %w", err)
		}

		return directory, nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect the pictures directory: %w", err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("the pictures path is a symbolic link")
	}
	if !info.IsDir() {
		return "", errors.New("the pictures path is not a directory")
	}

	return directory, nil
}

func write(path string, data []byte) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}

	//nolint:gosec // a path built from a validated digest under the session directory
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write the picture: %w", err)
	}

	return nil
}
