package painter

import (
	"bytes"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/pkg/agent"
)

const lateReference = "See [the docs][docs] first.\n\n" +
	"A line of the answer.\n\nAnother line of the answer.\n\nA third line of the answer.\n\n" +
	"A fourth line of the answer.\n\nA fifth line of the answer.\n\nA sixth line of the answer.\n\n" +
	"[docs]: https://example.com\n\n"

func TestARedrawLeavesThePainterFresh(t *testing.T) {
	var screenOutput bytes.Buffer
	screen := output.NewTerminalOfSize(&screenOutput, 80, 8)
	paint := New(screen, true, nil, nil, output.StreamingModeASAP)

	redraws := 0
	for _, fragment := range strings.SplitAfter(lateReference+strings.Repeat("And then some prose. ", 40), " ") {
		paint.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: fragment})

		if paint.Stale() {
			redraws++
			screen.Reset()
			paint.Redraw(nil)

			if paint.Stale() {
				t.Fatal("the painter is still stale after redrawing")
			}
		}
	}

	if redraws == 0 {
		t.Fatal("the late reference never changed a committed row, so nothing was redrawn")
	}
}
