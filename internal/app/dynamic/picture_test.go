package dynamic

import (
	"strings"
	"testing"

	"crdx.org/oh/internal/app/graphics"
	"crdx.org/oh/internal/app/width"
)

func blockWithPicture(t *testing.T, picture Picture) *Block {
	t.Helper()

	block := NewBlock(func() {})
	t.Cleanup(block.Stop)

	index := block.Add(rowLabel("read", "screenshot.png"), 0)
	block.AttachPicture(index, picture)

	return block
}

func drawnPicture() Picture {
	return Picture{
		Data:       []byte("\x89PNG"),
		Width:      400,
		Height:     200,
		CellWidth:  10,
		CellHeight: 20,
	}
}

func TestAPictureIsDrawnUnderTheCallThatReadIt(t *testing.T) {
	block := blockWithPicture(t, drawnPicture())

	rows := block.Rows(100)
	if len(rows) < 2 {
		t.Fatalf("drew %d rows, want the call and its picture", len(rows))
	}
	if !strings.Contains(rows[0], "screenshot.png") {
		t.Errorf("the first row is %q, want the call", rows[0])
	}
	if !strings.Contains(rows[1], "\x1b_G") {
		t.Error("the picture was not drawn under the call")
	}
}

func TestAPictureTakesAsManyRowsAsItsShapeNeeds(t *testing.T) {
	block := blockWithPicture(t, drawnPicture())

	rows := block.Rows(100)

	if got := len(rows) - 1; got != 10 {
		t.Errorf("the picture took %d rows, want 10 for 400x200 in 10x20 cells", got)
	}
	for at, row := range rows[1:] {
		if got := width.Of(row); got != 40 {
			t.Errorf("picture row %d measures %d cells, want 40", at, got)
		}
	}
}

func TestAPictureIsNarrowedToFitTheScreenRatherThanWrapping(t *testing.T) {
	block := blockWithPicture(t, drawnPicture())

	for _, row := range block.Rows(20)[1:] {
		if got := width.Of(row); got > 20 {
			t.Errorf("a picture row measures %d cells in a 20 cell screen", got)
		}
	}
}

func TestATallPictureIsHeldToTheRowsAPlacementAllows(t *testing.T) {
	picture := drawnPicture()
	picture.Width, picture.Height = 100, 100000

	block := blockWithPicture(t, picture)

	if got := len(block.Rows(200)) - 1; got > graphics.MaxRows {
		t.Errorf("the picture took %d rows, want no more than %d", got, graphics.MaxRows)
	}
}

func TestAPictureDrawnAgainAtTheSameWidthIsNotSentAgain(t *testing.T) {
	block := blockWithPicture(t, drawnPicture())

	first := block.Rows(100)
	second := block.Rows(100)

	if first[1] != second[1] {
		t.Error("the picture was transmitted again for an unchanged repaint")
	}
	if strings.Count(second[1], "\x1b_G") == 0 {
		t.Error("the repaint dropped the picture it was told to keep")
	}
}

func TestAPictureIsSentAgainOnlyWhenTheWidthChanges(t *testing.T) {
	block := blockWithPicture(t, drawnPicture())

	wide := block.Rows(100)
	narrow := block.Rows(30)

	if wide[1] == narrow[1] {
		t.Error("the picture was not laid out again for a new width")
	}
}

func TestAPictureOnDiskIsDrawnByPathWhenTheTerminalIsLocal(t *testing.T) {
	picture := drawnPicture()
	picture.Path = "/state/sessions/tame-impala/images/abc-w800.png"
	picture.IsLocal = true

	block := blockWithPicture(t, picture)

	row := block.Rows(100)[1]
	if !strings.Contains(row, "t=f") {
		t.Error("a local terminal was not sent the path")
	}
	if len(row) > 400 {
		t.Errorf("a local placement is %d bytes, want it to stay small", len(row))
	}
}

func TestAPictureWithNoShapeIsNotDrawnAtAll(t *testing.T) {
	picture := drawnPicture()
	picture.Width, picture.Height = 0, 0

	block := blockWithPicture(t, picture)

	if got := len(block.Rows(100)); got != 1 {
		t.Errorf("drew %d rows, want the call alone", got)
	}
}
