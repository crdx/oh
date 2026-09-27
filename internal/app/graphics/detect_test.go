package graphics

import (
	"os"
	"testing"
	"time"

	"crdx.org/oh/internal/app/ptytest"
	"crdx.org/oh/internal/app/tty"
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
	if waited := time.Since(startedAt); waited >= tty.ReplyTimeout {
		t.Errorf("waited %s for a reply that had already ended", waited)
	}
}

func TestAKeyTypedBeforeTheReplyNeitherSpoilsItNorIsLost(t *testing.T) {
	controller, terminal := ptytest.OpenSized(t, 80, 24)
	if _, err := controller.WriteString("c"); err != nil {
		t.Fatal(err)
	}
	go func() {
		asked := make([]byte, 256)
		if _, err := controller.Read(asked); err != nil {
			return
		}
		_, _ = controller.WriteString("\x1b_Gi=1;OK\x1b\\\x1b[?62;4c")
	}()

	if _, _, hasGraphics := Detect(terminal, terminal); !hasGraphics {
		t.Error("a key typed before the reply hid the terminal's answer")
	}

	reader := tty.NewReader(terminal)
	t.Cleanup(reader.Close)
	kept := make(chan byte, 1)
	go func() {
		key := make([]byte, 1)
		if _, err := reader.Read(key); err == nil {
			kept <- key[0]
		}
	}()
	t.Cleanup(reader.Stop)

	select {
	case key := <-kept:
		if key != 'c' {
			t.Errorf("the next reader got %q, want the key typed before the probe", key)
		}
	case <-time.After(time.Second):
		t.Error("the key typed before the probe was lost")
	}
}
