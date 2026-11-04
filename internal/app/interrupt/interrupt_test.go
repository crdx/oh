package interrupt_test

import (
	"testing"

	"crdx.org/oh/internal/app/interrupt"
)

func TestUndoKeepsItsReasonWithoutAnnouncingIt(t *testing.T) {
	event := interrupt.Event(interrupt.ControlZ)
	if interrupt.IsAnnounced(event) {
		t.Fatal("an internal undo interruption was announced")
	}
	if interrupt.Reason(event) != "the user pressed ctrl+z" {
		t.Fatal("hiding the undo notice removed its recorded reason")
	}
	if !interrupt.IsAnnounced(interrupt.Event(interrupt.Escape)) {
		t.Fatal("hiding the undo notice also hid explicit stops")
	}
}
