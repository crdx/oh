package drops_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/internal/app/drops"
)

func persisted(directory string) func() error {
	return func() error { return os.MkdirAll(directory, 0o700) }
}

func TestAPastedImageIsWrittenIntoTheDropsDirectory(t *testing.T) {
	sessionDirectory := filepath.Join(t.TempDir(), "tame-impala")

	path, err := drops.SaveImage(
		sessionDirectory,
		persisted(sessionDirectory),
		"image/png",
		[]byte("\x89PNG\r\n"),
	)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := filepath.Dir(path), drops.GetDirectory(sessionDirectory); got != want {
		t.Errorf("image was written to %q, want it under %q", got, want)
	}
	if got := filepath.Base(path); !strings.HasPrefix(got, "image-") || !strings.HasSuffix(got, ".png") {
		t.Errorf("image is named %q, want an image-*.png", got)
	}

	written, err := os.ReadFile(path) //nolint:gosec // a path this test just created
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != "\x89PNG\r\n" {
		t.Errorf("image holds %q, want the pasted bytes", written)
	}
}

func TestEachSupportedImageTypeTakesItsOwnExtension(t *testing.T) {
	for mediaType, want := range map[string]string{
		"image/png":  ".png",
		"image/jpeg": ".jpg",
		"image/webp": ".webp",
		"image/gif":  ".gif",
	} {
		t.Run(mediaType, func(t *testing.T) {
			sessionDirectory := filepath.Join(t.TempDir(), "tame-impala")

			path, err := drops.SaveImage(
				sessionDirectory,
				persisted(sessionDirectory),
				mediaType,
				[]byte("data"),
			)
			if err != nil {
				t.Fatal(err)
			}

			if got := filepath.Ext(path); got != want {
				t.Errorf("extension is %q, want %q", got, want)
			}
		})
	}
}

func TestAnUnsupportedImageTypeIsRefusedWithoutPreparingAnything(t *testing.T) {
	sessionDirectory := filepath.Join(t.TempDir(), "tame-impala")

	_, err := drops.SaveImage(
		sessionDirectory,
		persisted(sessionDirectory),
		"image/tiff",
		[]byte("II*\x00"),
	)
	if err == nil {
		t.Fatal("an unsupported image type was accepted")
	}
	if !strings.Contains(err.Error(), "image/tiff") {
		t.Errorf("error is %q, want it to name the type", err)
	}
	if _, err := os.Stat(drops.GetDirectory(sessionDirectory)); !os.IsNotExist(err) {
		t.Error("a refused image prepared the drops directory anyway")
	}
}

func TestTheSameImagePastedTwiceIsDroppedOnce(t *testing.T) {
	sessionDirectory := filepath.Join(t.TempDir(), "tame-impala")
	data := []byte("\x89PNG\r\n the very same bytes")

	first, err := drops.SaveImage(sessionDirectory, persisted(sessionDirectory), "image/png", data)
	if err != nil {
		t.Fatal(err)
	}
	second, err := drops.SaveImage(sessionDirectory, persisted(sessionDirectory), "image/png", data)
	if err != nil {
		t.Fatal(err)
	}

	if first != second {
		t.Errorf("the same image was dropped at %q and %q", first, second)
	}

	entries, err := os.ReadDir(drops.GetDirectory(sessionDirectory))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("the drops directory holds %d files, want one", len(entries))
	}
}

func TestTwoDifferentImagesAreDroppedSeparately(t *testing.T) {
	sessionDirectory := filepath.Join(t.TempDir(), "tame-impala")

	first, err := drops.SaveImage(sessionDirectory, persisted(sessionDirectory), "image/png", []byte("one"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := drops.SaveImage(sessionDirectory, persisted(sessionDirectory), "image/png", []byte("two"))
	if err != nil {
		t.Fatal(err)
	}

	if first == second {
		t.Errorf("two different images were dropped at the same path %q", first)
	}
}

func TestAnImageIsNamedAfterItsContentsSoAPasteIsRecognisedAgain(t *testing.T) {
	sessionDirectory := filepath.Join(t.TempDir(), "tame-impala")
	data := []byte("\x89PNG named by what it holds")

	path, err := drops.SaveImage(sessionDirectory, persisted(sessionDirectory), "image/png", data)
	if err != nil {
		t.Fatal(err)
	}

	digest := sha256.Sum256(data)
	want := "image-" + hex.EncodeToString(digest[:])[:16] + ".png"

	if got := filepath.Base(path); got != want {
		t.Errorf("the image is named %q, want %q", got, want)
	}
}

func TestAnImageWhoseNameIsTakenByOtherContentsIsDroppedBeside(t *testing.T) {
	sessionDirectory := filepath.Join(t.TempDir(), "tame-impala")
	data := []byte("\x89PNG the real image")

	digest := sha256.Sum256(data)
	name := "image-" + hex.EncodeToString(digest[:])[:16] + ".png"

	if err := persisted(sessionDirectory)(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(drops.GetDirectory(sessionDirectory), 0o700); err != nil {
		t.Fatal(err)
	}
	taken := filepath.Join(drops.GetDirectory(sessionDirectory), name)
	if err := os.WriteFile(taken, []byte("something else entirely"), 0o600); err != nil {
		t.Fatal(err)
	}

	path, err := drops.SaveImage(sessionDirectory, persisted(sessionDirectory), "image/png", data)
	if err != nil {
		t.Fatal(err)
	}

	if path == taken {
		t.Fatal("the image was written over contents that were not its own")
	}

	written, err := os.ReadFile(path) //nolint:gosec // a path this test just created
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written, data) {
		t.Error("the image beside it does not hold the pasted bytes")
	}
}
