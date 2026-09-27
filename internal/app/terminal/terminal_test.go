package terminal

import (
	"bytes"
	"testing"
	"time"

	"crdx.org/oh/internal/app/ptytest"
)

func TestScrollbackIsErasedOnlyOnATerminal(t *testing.T) {
	var written bytes.Buffer
	ResetScrollback(&written)
	if written.Len() != 0 {
		t.Errorf("erased %q into something that is not a terminal", written.String())
	}

	controller, terminal := ptytest.Open(t)
	screen := ptytest.Record(controller)
	ResetScrollback(terminal)
	if !screen.WaitForCount(eraseDisplay, 1, time.Second) {
		t.Errorf("the terminal was sent %q, want the scrollback erased", screen.String())
	}
}
