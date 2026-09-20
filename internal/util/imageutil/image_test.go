package imageutil_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"crdx.org/io/internal/util/imageutil"
	"crdx.org/io/pkg/tool"
)

func pngOf(t *testing.T, subject image.Image) tool.Image {
	t.Helper()

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, subject); err != nil {
		t.Fatalf("could not encode the test image: %v", err)
	}

	return tool.Image{MediaType: "image/png", Data: encoded.Bytes()}
}

func filled(width int, height int, shade color.NRGBA) *image.NRGBA {
	subject := image.NewNRGBA(image.Rect(0, 0, width, height))

	for y := range height {
		for x := range width {
			subject.SetNRGBA(x, y, shade)
		}
	}

	return subject
}

func TestAnImageWithinBoundsIsLeftAlone(t *testing.T) {
	subject := pngOf(t, filled(64, 33, color.NRGBA{R: 10, G: 20, B: 30, A: 255}))

	if bounded := imageutil.Bound(subject); !bytes.Equal(bounded.Data, subject.Data) {
		t.Errorf("expected the original bytes, got %d of them", len(bounded.Data))
	}
}

func TestAnOversizedImageIsScaledToTheLongestEdge(t *testing.T) {
	subject := pngOf(t, filled(1600, 800, color.NRGBA{R: 10, G: 20, B: 30, A: 255}))

	bounded := imageutil.Bound(subject)

	width, height, ok := imageutil.Dimensions(bounded.Data)
	if !ok {
		t.Fatal("expected the bounded image to be readable")
	}

	if width != imageutil.MaxEdge || height != imageutil.MaxEdge/2 {
		t.Errorf("expected %dx%d, got %dx%d", imageutil.MaxEdge, imageutil.MaxEdge/2, width, height)
	}

	if len(bounded.Data) >= len(subject.Data) {
		t.Errorf("expected fewer bytes than %d, got %d", len(subject.Data), len(bounded.Data))
	}
}

func TestATallImageIsScaledByItsHeight(t *testing.T) {
	subject := pngOf(t, filled(400, 1600, color.NRGBA{R: 90, G: 20, B: 30, A: 255}))

	bounded := imageutil.Bound(subject)

	width, height, _ := imageutil.Dimensions(bounded.Data)
	if width != imageutil.MaxEdge/4 || height != imageutil.MaxEdge {
		t.Errorf("expected %dx%d, got %dx%d", imageutil.MaxEdge/4, imageutil.MaxEdge, width, height)
	}
}

func TestScalingBlendsTheSourcePixelsItCovers(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, imageutil.MaxEdge*2, 1))
	for x := range imageutil.MaxEdge * 2 {
		shade := color.NRGBA{A: 255}
		if x%2 == 1 {
			shade = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
		}

		source.SetNRGBA(x, 0, shade)
	}

	bounded := imageutil.Bound(pngOf(t, source))

	decoded, err := png.Decode(bytes.NewReader(bounded.Data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	shade := nrgbaAt(t, decoded, imageutil.MaxEdge/2)
	if shade.R != 127 && shade.R != 128 {
		t.Errorf("expected a blended midpoint, got %+v", shade)
	}
}

func TestTransparencyDoesNotBleedIntoTheColoursAroundIt(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, imageutil.MaxEdge*2, 1))
	for x := range imageutil.MaxEdge * 2 {
		shade := color.NRGBA{R: 255, G: 255, B: 255, A: 0}
		if x%2 == 1 {
			shade = color.NRGBA{R: 10, G: 20, B: 30, A: 255}
		}

		source.SetNRGBA(x, 0, shade)
	}

	bounded := imageutil.Bound(pngOf(t, source))

	decoded, err := png.Decode(bytes.NewReader(bounded.Data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	shade := nrgbaAt(t, decoded, imageutil.MaxEdge/2)
	if shade.R != 10 || shade.G != 20 || shade.B != 30 {
		t.Errorf("expected the opaque colour to survive, got %+v", shade)
	}

	if shade.A != 127 && shade.A != 128 {
		t.Errorf("expected a half-transparent pixel, got alpha %d", shade.A)
	}
}

func TestAnOversizedJPEGStaysAJPEG(t *testing.T) {
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, filled(1600, 800, color.NRGBA{R: 200, G: 40, B: 40, A: 255}), nil); err != nil {
		t.Fatalf("could not encode the test image: %v", err)
	}

	bounded := imageutil.Bound(tool.Image{MediaType: "image/jpeg", Data: encoded.Bytes()})
	if bounded.MediaType != "image/jpeg" {
		t.Errorf("expected a JPEG, got %q", bounded.MediaType)
	}

	width, height, ok := imageutil.Dimensions(bounded.Data)
	if !ok || width != imageutil.MaxEdge || height != imageutil.MaxEdge/2 {
		t.Errorf("expected %dx%d, got %dx%d", imageutil.MaxEdge, imageutil.MaxEdge/2, width, height)
	}
}

func TestAnOversizedGIFBecomesAPNG(t *testing.T) {
	var encoded bytes.Buffer
	subject := filled(1600, 800, color.NRGBA{R: 40, G: 200, B: 40, A: 255})
	if err := gif.Encode(&encoded, subject, nil); err != nil {
		t.Fatalf("could not encode the test image: %v", err)
	}

	bounded := imageutil.Bound(tool.Image{MediaType: "image/gif", Data: encoded.Bytes()})
	if bounded.MediaType != "image/png" {
		t.Errorf("expected a PNG, got %q", bounded.MediaType)
	}

	width, _, ok := imageutil.Dimensions(bounded.Data)
	if !ok || width != imageutil.MaxEdge {
		t.Errorf("expected a %d-pixel edge, got %d", imageutil.MaxEdge, width)
	}
}

func TestAnImageThatCannotBeDecodedIsCarriedAsItIs(t *testing.T) {
	subject := tool.Image{MediaType: "image/webp", Data: webP(1600, 800)}

	if bounded := imageutil.Bound(subject); !bytes.Equal(bounded.Data, subject.Data) {
		t.Errorf("expected the original bytes, got %d of them", len(bounded.Data))
	}
}

func TestTheSameImageIsBoundedTheSameWayTwice(t *testing.T) {
	subject := pngOf(t, filled(1600, 800, color.NRGBA{R: 1, G: 2, B: 3, A: 255}))

	first := imageutil.Bound(subject)
	if second := imageutil.Bound(subject); !bytes.Equal(first.Data, second.Data) {
		t.Errorf("expected %d identical bytes, got %d", len(first.Data), len(second.Data))
	}
}

func TestFitLeavesAnImageWithinBoundsAlone(t *testing.T) {
	if width, height := imageutil.Fit(100, 200); width != 100 || height != 200 {
		t.Errorf("expected 100x200, got %dx%d", width, height)
	}
}

func TestFitNeverReportsAnEdgeShorterThanAPixel(t *testing.T) {
	if width, height := imageutil.Fit(100_000, 1); width != imageutil.MaxEdge || height != 1 {
		t.Errorf("expected %dx1, got %dx%d", imageutil.MaxEdge, width, height)
	}
}

func TestDimensionsReadsEachFormatItSupports(t *testing.T) {
	var encodedPNG, encodedJPEG, encodedGIF bytes.Buffer
	subject := filled(64, 33, color.NRGBA{R: 5, G: 5, B: 5, A: 255})

	if err := png.Encode(&encodedPNG, subject); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&encodedJPEG, subject, nil); err != nil {
		t.Fatal(err)
	}
	if err := gif.Encode(&encodedGIF, subject, nil); err != nil {
		t.Fatal(err)
	}

	tests := map[string]tool.Image{
		"png":  {MediaType: "image/png", Data: encodedPNG.Bytes()},
		"jpeg": {MediaType: "image/jpeg", Data: encodedJPEG.Bytes()},
		"gif":  {MediaType: "image/gif", Data: encodedGIF.Bytes()},
		"webp": {MediaType: "image/webp", Data: webP(64, 33)},
	}

	for name, subject := range tests {
		t.Run(name, func(t *testing.T) {
			width, height, ok := imageutil.Dimensions(subject.Data)
			if !ok || width != 64 || height != 33 {
				t.Errorf("expected 64x33, got %dx%d (%t)", width, height, ok)
			}
		})
	}
}

func TestDimensionsRefusesWhatItCannotRead(t *testing.T) {
	if _, _, ok := imageutil.Dimensions([]byte("not an image")); ok {
		t.Error("expected something that is not an image to be refused")
	}

	if _, _, ok := imageutil.Dimensions(nil); ok {
		t.Error("expected nothing at all to be refused")
	}
}

func TestOnlyImageMediaTypesAreSupported(t *testing.T) {
	for _, mediaType := range []string{"image/gif", "image/jpeg", "image/png", "image/webp"} {
		if !imageutil.IsSupported(mediaType) {
			t.Errorf("expected %q to be supported", mediaType)
		}
	}

	for _, mediaType := range []string{"text/plain", "image/tiff", ""} {
		if imageutil.IsSupported(mediaType) {
			t.Errorf("expected %q not to be supported", mediaType)
		}
	}
}

func webP(width int, height int) []byte {
	const (
		chunkLength  = 10
		headerLength = 12
	)

	chunk := make([]byte, chunkLength)
	putUint24(chunk[4:7], width-1)
	putUint24(chunk[7:10], height-1)

	data := []byte("RIFF____WEBPVP8X")
	binary.LittleEndian.PutUint32(data[4:8], chunkLength+headerLength)
	data = binary.LittleEndian.AppendUint32(data, chunkLength)

	return append(data, chunk...)
}

func putUint24(target []byte, value int) {
	target[0] = byte(value & 0xff)
	target[1] = byte(value >> 8 & 0xff)
	target[2] = byte(value >> 16 & 0xff)
}

func nrgbaAt(t *testing.T, subject image.Image, x int) color.NRGBA {
	t.Helper()

	shade, ok := color.NRGBAModel.Convert(subject.At(x, 0)).(color.NRGBA)
	if !ok {
		t.Fatalf("expected an eight-bit colour, got %T", subject.At(x, 0))
	}

	return shade
}

func TestAnOversizedWebPIsScaledIntoAPNG(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "oversized.webp"))
	if err != nil {
		t.Fatal(err)
	}

	if width, height, ok := imageutil.Dimensions(data); !ok || width != 1600 || height != 800 {
		t.Fatalf("expected a 1600x800 fixture, got %dx%d (%t)", width, height, ok)
	}

	bounded := imageutil.Bound(tool.Image{MediaType: "image/webp", Data: data})
	if bounded.MediaType != "image/png" {
		t.Errorf("expected a PNG, got %q", bounded.MediaType)
	}

	width, height, ok := imageutil.Dimensions(bounded.Data)
	if !ok || width != imageutil.MaxEdge || height != imageutil.MaxEdge/2 {
		t.Errorf("expected %dx%d, got %dx%d", imageutil.MaxEdge, imageutil.MaxEdge/2, width, height)
	}
}
