package graphics

import (
	"strings"
	"testing"
)

func TestHiddenPictureTransmissions(t *testing.T) {
	first, _ := PlacePNG([]byte("first"), Box{Cells: 3, Rows: 3})
	second, _ := PlacePNG([]byte("second"), Box{Cells: 3, Rows: 3})

	for _, testCase := range []struct {
		name        string
		hiddenRows  []string
		visibleRows []string
		wantRows    []string
	}{
		{name: "first picture partly hidden", hiddenRows: first[:2], visibleRows: first[2:], wantRows: first[:1]},
		{name: "picture entirely hidden", hiddenRows: first, visibleRows: []string{"next"}},
		{
			name:        "two pictures partly visible",
			hiddenRows:  []string{first[0], second[0]},
			visibleRows: []string{first[1], second[1]},
			wantRows:    []string{first[0], second[0]},
		},
		{name: "picture beginning in the visible window", hiddenRows: []string{"read"}, visibleRows: first},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := HiddenPictureTransmissions(testCase.hiddenRows, testCase.visibleRows)
			var want strings.Builder
			for _, row := range testCase.wantRows {
				beforeColour, _, _ := strings.Cut(row, "\x1b[38;2;")
				want.WriteString(beforeColour)
			}
			if got != want.String() {
				t.Errorf("transmissions %q, want %q", got, want.String())
			}
		})
	}
}
