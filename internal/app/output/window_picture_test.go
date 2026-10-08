package output

import (
	"strings"
	"testing"

	"crdx.org/oh/internal/app/graphics"
)

func TestOneVisiblePictureRowStillTransmitsItsImage(t *testing.T) {
	pictureRows, isPlaced := graphics.PlacePNG([]byte("picture"), graphics.Box{Cells: 3, Rows: 3})
	if !isPlaced {
		t.Fatal("picture was not placed")
	}

	var drawn strings.Builder
	screen := &Screen{writer: &drawn, isTerminal: true, canRepaint: true, columns: 40, lines: 3}
	screen.Footer([]string{">"}, 0, 1)
	screen.OpenTool(&rowsBlock{rows: append([]string{"read"}, pictureRows...)})

	if !strings.Contains(drawn.String(), "f=100") {
		t.Fatal("a one-row window showed a picture placeholder without transmitting its image")
	}
}
