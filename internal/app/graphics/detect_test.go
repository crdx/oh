package graphics

import (
	"os"
	"testing"
	"time"

	"crdx.org/oh/internal/app/ptytest"
)

func answeringTerminal(t *testing.T, reply string) *os.File {
	t.Helper()

	controller, terminal := ptytest.OpenSized(t, 80, 24)
	go func() {
		asked := make([]byte, 256)
		if _, err := controller.Read(asked); err != nil {
			return
		}
		_, _ = controller.WriteString(reply)
	}()

	return terminal
}

func TestATerminalThatAnswersTheProbeIsDrawnInto(t *testing.T) {
	terminal := answeringTerminal(t, "\x1b_Gi=1;OK\x1b\\\x1b[?62;4c")

	cellWidth, cellHeight, hasGraphics := Detect(terminal, terminal)
	if !hasGraphics {
		t.Fatal("a terminal that answered the probe was not drawn into")
	}
	if cellWidth != defaultCellWidth || cellHeight != defaultCellHeight {
		t.Errorf("got %dx%d cells from a terminal naming no pixels, want the defaults", cellWidth, cellHeight)
	}
}

func TestATerminalThatOnlyNamesItselfIsNotDrawnInto(t *testing.T) {
	terminal := answeringTerminal(t, "\x1b[?62;4c")

	startedAt := time.Now()
	if _, _, hasGraphics := Detect(terminal, terminal); hasGraphics {
		t.Error("a terminal that ignored the probe was drawn into")
	}
	if waited := time.Since(startedAt); waited >= replyTimeout {
		t.Errorf("waited %s for a reply that had already ended", waited)
	}
}
