package graphics

import (
	"encoding/base64"
	"strings"
	"testing"

	"crdx.org/io/internal/app/width"
)

func TestAPictureIsPlacedAsOneRowOfCellsForEachRowItOccupies(t *testing.T) {
	rows, isPlaced := PlacePNG([]byte("\x89PNG"), Box{Cells: 12, Rows: 3})
	if !isPlaced {
		t.Fatal("the picture was not placed")
	}

	if len(rows) != 3 {
		t.Fatalf("placed %d rows, want 3", len(rows))
	}

	for at, row := range rows {
		if got := width.Of(row); got != 12 {
			t.Errorf("row %d measures %d cells, want 12", at, got)
		}
	}
}

func TestOnlyTheFirstRowCarriesThePicture(t *testing.T) {
	rows, _ := PlacePNG([]byte("\x89PNG"), Box{Cells: 4, Rows: 2})

	if !strings.Contains(rows[0], openCommand) {
		t.Error("the first row does not transmit the picture")
	}
	if strings.Contains(rows[1], openCommand) {
		t.Error("a later row transmits the picture again")
	}
}

func TestEachRowIsMarkedWithItsOwnPositionInThePicture(t *testing.T) {
	rows, _ := PlacePNG([]byte("\x89PNG"), Box{Cells: 4, Rows: 3})

	for at, row := range rows {
		if !strings.Contains(row, placeholder+string(rowMarks[at])+string(rowMarks[0])) {
			t.Errorf("row %d is not marked as row %d of the picture", at, at)
		}
	}
}

func TestAPNGIsSentAsItIsRatherThanDecodedFirst(t *testing.T) {
	data := []byte("\x89PNG\r\n\x1a\n and some more bytes")

	rows, _ := PlacePNG(data, Box{Cells: 4, Rows: 1})

	if !strings.Contains(rows[0], "f=100") || !strings.Contains(rows[0], "t=d") {
		t.Errorf("transmission is %q, want a direct PNG", rows[0])
	}
	if !strings.Contains(rows[0], base64.StdEncoding.EncodeToString(data)) {
		t.Error("the PNG bytes were not sent verbatim")
	}
}

func TestAPictureOnDiskIsSentAsItsPathRatherThanItsBytes(t *testing.T) {
	const path = "/state/sessions/tame-impala/images/abc.png"

	rows, isPlaced := PlaceFile(path, Box{Cells: 4, Rows: 1})
	if !isPlaced {
		t.Fatal("the picture was not placed")
	}

	if !strings.Contains(rows[0], "f=100") || !strings.Contains(rows[0], "t=f") {
		t.Errorf("transmission is %q, want a PNG read from a file", rows[0])
	}
	if !strings.Contains(rows[0], base64.StdEncoding.EncodeToString([]byte(path))) {
		t.Error("the path was not sent")
	}
	if len(rows[0]) > 300 {
		t.Errorf("a placement by path is %d bytes, want it to stay small", len(rows[0]))
	}
}

func TestABoxThatCannotBeDrawnIsRefused(t *testing.T) {
	for name, box := range map[string]Box{
		"no cells":      {Cells: 0, Rows: 1},
		"no rows":       {Cells: 4, Rows: 0},
		"too many rows": {Cells: 4, Rows: MaxRows + 1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, isPlaced := PlacePNG([]byte("\x89PNG"), box); isPlaced {
				t.Error("the picture was placed anyway")
			}
		})
	}
}

func TestAnEmptyPictureIsRefused(t *testing.T) {
	if _, isPlaced := PlacePNG(nil, Box{Cells: 4, Rows: 1}); isPlaced {
		t.Error("an empty PNG was placed")
	}
	if _, isPlaced := PlaceFile("", Box{Cells: 4, Rows: 1}); isPlaced {
		t.Error("an empty path was placed")
	}
}

func TestEveryRowMarkIsDistinctSoNoRowIsDrawnTwice(t *testing.T) {
	seen := map[rune]bool{}

	for at, mark := range rowMarks {
		if seen[mark] {
			t.Fatalf("row %d reuses a mark", at)
		}
		seen[mark] = true
	}
}
