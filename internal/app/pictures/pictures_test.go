package pictures_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/internal/app/pictures"
	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/tool"
)

func drawnPNG(t *testing.T, width int, height int) []byte {
	t.Helper()

	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			picture.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, picture); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}

func session(t *testing.T) (string, func() error) {
	t.Helper()

	directory := filepath.Join(t.TempDir(), "tame-impala")

	return directory, func() error { return os.MkdirAll(directory, 0o700) }
}

func TestTheOriginalIsKeptBesideTheCappedCopyItIsDrawnFrom(t *testing.T) {
	directory, ensure := session(t)
	data := drawnPNG(t, pictures.DisplayWidth*2, pictures.DisplayWidth)

	reference, err := pictures.Store(directory, ensure, tool.Image{MediaType: "image/png", Data: data})
	if err != nil {
		t.Fatal(err)
	}

	original := filepath.Join(pictures.GetDirectory(directory), reference.Digest+".png")
	stored, err := os.ReadFile(original) //nolint:gosec // a path this test just created
	if err != nil {
		t.Fatalf("the original was not kept: %v", err)
	}
	if !bytes.Equal(stored, data) {
		t.Error("the stored original is not the picture that was read")
	}

	if reference.Width != pictures.DisplayWidth {
		t.Errorf("drawn width is %d, want it capped at %d", reference.Width, pictures.DisplayWidth)
	}
	if reference.Height != pictures.DisplayWidth/2 {
		t.Errorf("drawn height is %d, want the aspect ratio kept", reference.Height)
	}
}

func TestASmallPictureIsNotEnlargedToTheCap(t *testing.T) {
	directory, ensure := session(t)

	reference, err := pictures.Store(directory, ensure, tool.Image{
		MediaType: "image/png",
		Data:      drawnPNG(t, 40, 20),
	})
	if err != nil {
		t.Fatal(err)
	}

	if reference.Width != 40 || reference.Height != 20 {
		t.Errorf("drawn size is %dx%d, want it left alone", reference.Width, reference.Height)
	}
}

func TestTheDrawnCopyIsFoundAgainByItsReference(t *testing.T) {
	directory, ensure := session(t)

	reference, err := pictures.Store(directory, ensure, tool.Image{
		MediaType: "image/png",
		Data:      drawnPNG(t, 60, 30),
	})
	if err != nil {
		t.Fatal(err)
	}

	drawing, isStored := pictures.Prepare(directory, reference)
	if !isStored {
		t.Fatal("the drawn copy was not found")
	}
	if !strings.HasSuffix(drawing.Path, ".png") {
		t.Errorf("the drawn copy is %q, want a PNG", drawing.Path)
	}
	if drawing.Width != 60 || drawing.Height != 30 {
		t.Errorf("drawn size is %dx%d, want 60x30", drawing.Width, drawing.Height)
	}

	data, isRead := pictures.Read(drawing.Path)
	if !isRead || len(data) == 0 {
		t.Error("the drawn copy could not be read back")
	}
}

func TestNothingIsFoundForAPictureThatWasNeverStored(t *testing.T) {
	directory, _ := session(t)

	for name, reference := range map[string]*agent.Picture{
		"no reference": nil,
		"no digest":    {},
		"unknown":      {Digest: "0000000000000000000000000000000000000000000000000000000000000000"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, isStored := pictures.Prepare(directory, reference); isStored {
				t.Error("a picture was found anyway")
			}
		})
	}
}

func TestAPictureTooLargeToKeepIsRefusedRatherThanStored(t *testing.T) {
	directory, ensure := session(t)

	_, err := pictures.Store(directory, ensure, tool.Image{
		MediaType: "image/png",
		Data:      make([]byte, pictures.MaxStoredBytes+1),
	})
	if err == nil {
		t.Fatal("an oversized picture was stored")
	}
	if _, statErr := os.Stat(pictures.GetDirectory(directory)); !os.IsNotExist(statErr) {
		t.Error("a refused picture prepared the pictures directory anyway")
	}
}

func TestAnUnsupportedTypeIsRefused(t *testing.T) {
	directory, ensure := session(t)

	if _, err := pictures.Store(directory, ensure, tool.Image{
		MediaType: "image/tiff",
		Data:      []byte("II*\x00"),
	}); err == nil {
		t.Fatal("an unsupported type was stored")
	}
}

func TestTheSamePictureReadTwiceIsStoredOnce(t *testing.T) {
	directory, ensure := session(t)
	data := drawnPNG(t, 50, 25)

	first, err := pictures.Store(directory, ensure, tool.Image{MediaType: "image/png", Data: data})
	if err != nil {
		t.Fatal(err)
	}
	second, err := pictures.Store(directory, ensure, tool.Image{MediaType: "image/png", Data: data})
	if err != nil {
		t.Fatal(err)
	}

	if first.Digest != second.Digest {
		t.Errorf("digests are %q and %q, want the same picture named once", first.Digest, second.Digest)
	}

	entries, err := os.ReadDir(pictures.GetDirectory(directory))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("stored %d files, want the original and its drawn copy", len(entries))
	}
}

func TestADrawnCopyIsBuiltAgainFromTheOriginalWhenItIsNotThere(t *testing.T) {
	directory, ensure := session(t)
	data := drawnPNG(t, 300, 150)

	reference, err := pictures.Store(directory, ensure, tool.Image{MediaType: "image/png", Data: data})
	if err != nil {
		t.Fatal(err)
	}

	drawing, isStored := pictures.Prepare(directory, reference)
	if !isStored {
		t.Fatal("the drawn copy was not found")
	}
	if err := os.Remove(drawing.Path); err != nil {
		t.Fatal(err)
	}

	rebuilt, isRebuilt := pictures.Prepare(directory, reference)
	if !isRebuilt {
		t.Fatal("the drawn copy was not built again from the original")
	}
	if rebuilt.Width != 300 || rebuilt.Height != 150 {
		t.Errorf("rebuilt size is %dx%d, want 300x150", rebuilt.Width, rebuilt.Height)
	}
	if _, err := os.Stat(rebuilt.Path); err != nil {
		t.Errorf("the rebuilt copy was not written back: %v", err)
	}
}

func TestADrawnCopyIsMeasuredAfreshRatherThanTrustingAStaleReference(t *testing.T) {
	directory, ensure := session(t)

	reference, err := pictures.Store(directory, ensure, tool.Image{
		MediaType: "image/png",
		Data:      drawnPNG(t, 300, 150),
	})
	if err != nil {
		t.Fatal(err)
	}

	drawing, _ := pictures.Prepare(directory, reference)
	if err := os.Remove(drawing.Path); err != nil {
		t.Fatal(err)
	}

	reference.Width, reference.Height = 9999, 9999

	rebuilt, isRebuilt := pictures.Prepare(directory, reference)
	if !isRebuilt {
		t.Fatal("the drawn copy was not built again")
	}
	if rebuilt.Width != 300 || rebuilt.Height != 150 {
		t.Errorf("rebuilt size is %dx%d, want the size it was actually drawn at", rebuilt.Width, rebuilt.Height)
	}
}

func TestNothingIsDrawnWhenTheOriginalIsGoneToo(t *testing.T) {
	directory, ensure := session(t)

	reference, err := pictures.Store(directory, ensure, tool.Image{
		MediaType: "image/png",
		Data:      drawnPNG(t, 300, 150),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := os.RemoveAll(pictures.GetDirectory(directory)); err != nil {
		t.Fatal(err)
	}

	if _, isStored := pictures.Prepare(directory, reference); isStored {
		t.Error("a picture was drawn with nothing left to draw it from")
	}
}
