package pictures_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/pictures"
	"crdx.org/oh/internal/app/width"
)

const (
	cellWidth  = 10
	cellHeight = 20
)

func pictureBytes(t *testing.T, isJPEG bool, pictureWidth int, pictureHeight int) []byte {
	t.Helper()

	subject := image.NewRGBA(image.Rect(0, 0, pictureWidth, pictureHeight))
	for y := range pictureHeight {
		for x := range pictureWidth {
			subject.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 90, A: 255})
		}
	}

	var encoded bytes.Buffer
	if isJPEG {
		if err := jpeg.Encode(&encoded, subject, nil); err != nil {
			t.Fatal(err)
		}
	} else if err := png.Encode(&encoded, subject); err != nil {
		t.Fatal(err)
	}

	return encoded.Bytes()
}

const (
	pictureWidth  = 400
	pictureHeight = 200
)

func picturePath(t *testing.T, name string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	writePicture(t, path, pictureBytes(t, strings.HasSuffix(name, ".jpg"), pictureWidth, pictureHeight))

	return path
}

func writePicture(t *testing.T, path string, data []byte) {
	t.Helper()

	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func newDrawer(isLocal bool, root string) *pictures.Drawer {
	return pictures.NewDrawer(pictures.Display{
		CellWidth:  cellWidth,
		CellHeight: cellHeight,
		IsLocal:    isLocal,
	}, root)
}

func newScratchDrawer(scratchDirectory string) *pictures.Drawer {
	return pictures.NewDrawer(pictures.Display{
		ScratchDirectory: scratchDirectory,
		CellWidth:        cellWidth,
		CellHeight:       cellHeight,
	}, "")
}

func TestAPictureTheModelNamedUnderItsOwnScratchIsFoundOnTheHost(t *testing.T) {
	path := picturePath(t, "chart.png")

	drawer := newScratchDrawer(filepath.Dir(path))

	if _, isDrawn := drawer.DrawPicture("/tmp/chart.png", 100); !isDrawn {
		t.Error("a picture under the model's own /tmp was not found in the scratch it maps to")
	}
}

func TestAPictureUnderTheScratchIsNeverSoughtOnTheHostsOwnTmp(t *testing.T) {
	drawer := newScratchDrawer(t.TempDir())

	if _, isDrawn := drawer.DrawPicture("/tmp", 100); isDrawn {
		t.Error("the host's own /tmp was read for a path the model meant for its scratch")
	}
}

func TestAPictureNamedRelativelyIsStillSoughtUnderTheWorkspace(t *testing.T) {
	path := picturePath(t, "chart.png")

	drawer := pictures.NewDrawer(pictures.Display{
		ScratchDirectory: t.TempDir(),
		CellWidth:        cellWidth,
		CellHeight:       cellHeight,
	}, filepath.Dir(path))

	if _, isDrawn := drawer.DrawPicture("chart.png", 100); !isDrawn {
		t.Error("a relative name was taken for the scratch rather than the workspace")
	}
}

func TestAPictureUnderTmpIsTakenAsItIsWrittenWhereThereIsNoScratch(t *testing.T) {
	path := picturePath(t, "chart.png")

	drawer := newDrawer(false, "")

	if _, isDrawn := drawer.DrawPicture(path, 100); !isDrawn {
		t.Error("a picture was not found where an unconfined session named it")
	}
}

func TestAPictureNamedByPathIsDrawnAtTheShapeItsPixelsAskFor(t *testing.T) {
	path := picturePath(t, "chart.png")

	rows, isDrawn := newDrawer(false, "").DrawPicture(path, 100)

	if !isDrawn {
		t.Fatal("the picture was not drawn")
	}
	if len(rows) != 10 {
		t.Errorf("drew %d rows, want 10 for 400x200 in 10x20 cells", len(rows))
	}
	for at, row := range rows {
		if got := width.Of(row); got != 40 {
			t.Errorf("row %d measures %d cells, want 40", at, got)
		}
	}
}

func TestAPictureOnDiskIsDrawnByPathWhenTheTerminalIsLocal(t *testing.T) {
	path := picturePath(t, "chart.png")

	rows, isDrawn := newDrawer(true, "").DrawPicture(path, 100)

	if !isDrawn {
		t.Fatal("the picture was not drawn")
	}
	if !strings.Contains(rows[0], "t=f") {
		t.Error("a local terminal was not sent the path")
	}
	if len(rows[0]) > 400 {
		t.Errorf("a local placement is %d bytes, want it to stay small", len(rows[0]))
	}
}

func TestAPictureThatIsNotAPortableNetworkGraphicIsSentAsOne(t *testing.T) {
	path := picturePath(t, "chart.jpg")

	rows, isDrawn := newDrawer(true, "").DrawPicture(path, 100)

	if !isDrawn {
		t.Fatal("the picture was not drawn")
	}
	if strings.Contains(rows[0], "t=f") {
		t.Error("a picture the terminal cannot read by path was sent by path")
	}
	if !strings.Contains(rows[0], "f=100") {
		t.Error("the picture was not sent as a portable network graphic")
	}
}

func TestAPictureDrawnAgainAtTheSameWidthIsNotSentAgain(t *testing.T) {
	path := picturePath(t, "chart.png")
	drawer := newDrawer(false, "")

	first, _ := drawer.DrawPicture(path, 100)
	second, _ := drawer.DrawPicture(path, 100)

	if first[0] != second[0] {
		t.Error("the picture was transmitted again for an unchanged repaint")
	}
}

func TestAPictureIsSentAgainOnlyWhenTheWidthChanges(t *testing.T) {
	path := picturePath(t, "chart.png")
	drawer := newDrawer(false, "")

	wide, _ := drawer.DrawPicture(path, 100)
	narrow, _ := drawer.DrawPicture(path, 20)

	if wide[0] == narrow[0] {
		t.Error("the picture was not laid out again for a new width")
	}
	for at, row := range narrow {
		if got := width.Of(row); got > 20 {
			t.Errorf("row %d measures %d cells in a 20 cell screen", at, got)
		}
	}
}

func TestAPictureChangedOnDiskIsDrawnAfresh(t *testing.T) {
	path := picturePath(t, "chart.png")
	drawer := newDrawer(false, "")

	first, _ := drawer.DrawPicture(path, 100)

	writePicture(t, path, pictureBytes(t, false, 200, 400))

	second, _ := drawer.DrawPicture(path, 100)

	if len(first) == len(second) {
		t.Errorf("drew %d rows again, want the new shape", len(second))
	}
}

func TestAPictureNamedRelativelyIsFoundUnderTheWorkspace(t *testing.T) {
	path := picturePath(t, "chart.png")
	drawer := newDrawer(false, filepath.Dir(path))

	if _, isDrawn := drawer.DrawPicture("chart.png", 100); !isDrawn {
		t.Error("a picture beside the workspace was not found")
	}
}

func TestAPictureNamedWithAnEscapedSpaceIsFound(t *testing.T) {
	path := picturePath(t, "a chart.png")
	drawer := newDrawer(false, "")

	escapedPath := strings.ReplaceAll(path, " ", "%20")
	if _, isDrawn := drawer.DrawPicture(escapedPath, 100); !isDrawn {
		t.Error("a picture whose address escaped its spaces was not found")
	}
}

func TestNothingIsDrawnForWhatIsNotAPictureOnDisk(t *testing.T) {
	prose := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(prose, []byte("not a picture"), 0o600); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{
		"prose":            prose,
		"a missing file":   filepath.Join(t.TempDir(), "absent.png"),
		"a directory":      t.TempDir(),
		"a web address":    "https://example.test/chart.png",
		"nothing at all":   "",
		"a relative guess": "chart.png",
	} {
		t.Run(name, func(t *testing.T) {
			if rows, isDrawn := newDrawer(false, "").DrawPicture(path, 100); isDrawn {
				t.Errorf("drew %q for %s", rows, name)
			}
		})
	}
}

func TestAPictureIsNotDrawnWhereThereIsNoRoomForIt(t *testing.T) {
	path := picturePath(t, "chart.png")

	if _, isDrawn := newDrawer(false, "").DrawPicture(path, 0); isDrawn {
		t.Error("a picture was drawn into no columns at all")
	}
}
