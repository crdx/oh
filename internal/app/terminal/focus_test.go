package terminal_test

import (
	"io"
	"testing"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/terminal"
	"crdx.org/oh/internal/app/work"
)

func TestFocusFollowsTheInteractiveTerminal(t *testing.T) {
	trackedTerminal := terminal.New(io.Discard, work.At("/workspace"))
	if trackedTerminal.IsFocused() {
		t.Fatal("a terminal that has not begun was focused")
	}

	restore := trackedTerminal.Begin(caps.Read)
	if !trackedTerminal.IsFocused() {
		t.Error("a newly begun terminal was not focused")
	}

	if !trackedTerminal.ObserveFocus(key.FocusOut) || trackedTerminal.IsFocused() {
		t.Error("the terminal remained focused after focus out")
	}
	if !trackedTerminal.ObserveFocus(key.FocusIn) || !trackedTerminal.IsFocused() {
		t.Error("the terminal remained unfocused after focus in")
	}
	if trackedTerminal.ObserveFocus(key.Enter) {
		t.Error("an ordinary key was treated as a focus change")
	}

	restore()
	if trackedTerminal.IsFocused() {
		t.Error("a restored terminal remained focused")
	}
}
