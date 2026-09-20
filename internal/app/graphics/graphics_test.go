package graphics

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"image"
	"image/color"
	"io"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
)

func flatPicture(width int, height int) *image.RGBA {
	picture := image.NewRGBA(image.Rect(0, 0, width, height))

	for y := range height {
		for x := range width {
			picture.SetRGBA(x, y, color.RGBA{R: 0x20, G: 0x40, B: 0x60, A: 0xff})
		}
	}

	return picture
}

func noisyPicture(width int, height int) *image.RGBA {
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	noise := rand.NewChaCha8([32]byte{})

	_, _ = noise.Read(picture.Pix)

	return picture
}

func TestAPictureIsTransmittedAndPlacedWhereTheCellsAre(t *testing.T) {
	drawn, isDrawn := Place(flatPicture(160, 20), 16)
	if !isDrawn {
		t.Fatal("the picture was not placed")
	}

	command, rest, found := strings.Cut(drawn, closeCommand)
	if !found {
		t.Fatalf("nothing terminated the command in %q", drawn)
	}

	for _, wanted := range []string{"a=T", "U=1", "q=2", "f=32", "o=z", "t=d", "s=160", "v=20", "c=16", "r=1", "m=0;"} {
		if !strings.Contains(command, wanted) {
			t.Errorf("the command %q says nothing of %q", command, wanted)
		}
	}

	if strings.Count(rest, placeholder) != 16 {
		t.Errorf("the placement holds %d cells, want 16", strings.Count(rest, placeholder))
	}

	if !strings.Contains(rest, placeholder+string(rowMarks[0])+string(rowMarks[0])) {
		t.Error("the first cell does not say where the picture starts")
	}
}

func TestALargePictureIsSentInChunks(t *testing.T) {
	drawn, isDrawn := Place(noisyPicture(320, 40), 32)
	if !isDrawn {
		t.Fatal("the picture was not placed")
	}

	commands := strings.Count(drawn, openCommand)
	if commands < 2 {
		t.Fatalf("the picture went in %d command(s), want more than one", commands)
	}

	if got := strings.Count(drawn, "m=1;"); got != commands-1 {
		t.Errorf("%d chunk(s) said more was coming, want %d", got, commands-1)
	}

	if got := strings.Count(drawn, "m=0;"); got != 1 {
		t.Errorf("%d chunk(s) closed the picture, want 1", got)
	}
}

func TestWhatIsSentUnpacksBackIntoThePixelsItWasGiven(t *testing.T) {
	picture := noisyPicture(40, 20)

	payload, err := encode(picture)
	if err != nil {
		t.Fatal(err)
	}

	packed, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatal(err)
	}

	reader, err := zlib.NewReader(bytes.NewReader(packed))
	if err != nil {
		t.Fatal(err)
	}

	pixels, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}

	if len(pixels) != 40*20*4 {
		t.Fatalf("unpacked %d bytes, want %d", len(pixels), 40*20*4)
	}

	if !bytes.Equal(pixels, picture.Pix) {
		t.Error("what unpacked is not what was drawn")
	}
}

func TestEachPictureIsPlacedUnderAnIdentifierOfItsOwn(t *testing.T) {
	first, _ := Place(flatPicture(20, 10), 2)
	second, _ := Place(flatPicture(20, 30), 2)

	if identifierOf(t, first) == identifierOf(t, second) {
		t.Errorf("both pictures were placed as %q", identifierOf(t, first))
	}
}

func identifierOf(t *testing.T, drawn string) string {
	t.Helper()

	_, rest, found := strings.Cut(drawn, "i=")
	if !found {
		t.Fatalf("no identifier in %q", drawn)
	}

	identifier, _, _ := strings.Cut(rest, ",")

	return identifier
}

func TestAPictureWithNowhereToGoIsNotPlaced(t *testing.T) {
	for _, test := range []struct {
		name    string
		picture *image.RGBA
		cells   int
	}{
		{name: "no picture at all", cells: 4},
		{name: "no cells to fill", picture: flatPicture(20, 20)},
		{name: "an empty picture", picture: image.NewRGBA(image.Rect(0, 0, 0, 0)), cells: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, isDrawn := Place(test.picture, test.cells); isDrawn {
				t.Error("expected nothing to be placed")
			}
		})
	}
}

func TestAPlacementNamesItsPictureInItsColour(t *testing.T) {
	placement := Placement(0x0a0b0c, 2)

	if !strings.Contains(placement, "\x1b[38;2;10;11;12m") {
		t.Errorf("placed as %q", placement)
	}

	if strings.Count(placement, placeholder) != 2 {
		t.Errorf("placed %d cells, want 2", strings.Count(placement, placeholder))
	}
}

func TestIdentifiersRunWellBeyondAByte(t *testing.T) {
	if maximumImageID <= 0xff {
		t.Errorf("identifiers stop at %d, which a long session would wrap", maximumImageID)
	}
}

func TestSomethingThatIsNotATerminalDrawsNoPictures(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		_ = reader.Close()
		_ = writer.Close()
	}()

	if _, _, hasGraphics := Detect(reader, writer); hasGraphics {
		t.Error("expected a pipe to support no graphics")
	}

	cellWidth, cellHeight := CellSize(writer)
	if cellWidth != defaultCellWidth || cellHeight != defaultCellHeight {
		t.Errorf("a pipe measures %dx%d, want the fallback", cellWidth, cellHeight)
	}
}
