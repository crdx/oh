package interaction

import (
	"io"
	"testing"
	"time"

	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/ptytest"
)

func TestAReleasedKeyboardLetsSomebodyElseRead(t *testing.T) {
	terminal := openTerminal(t)

	keyboard := NewKeyboard(terminal)
	keyboard.Release()

	if _, err := io.WriteString(terminal, "x"); err != nil {
		t.Fatal(err)
	}

	var taken [1]byte
	if _, err := terminal.Read(taken[:]); err != nil {
		t.Fatal(err)
	}
	if taken[0] != 'x' {
		t.Errorf("the editor got %q, so the keyboard was still reading", taken[0])
	}

	select {
	case keypress, isOpen := <-keyboard.Keys():
		if isOpen {
			t.Errorf("a released keyboard handed on %+v", keypress)
		} else {
			t.Error("releasing the keyboard closed its keys, which would end the session")
		}
	case <-time.After(decodingPause):
	}
}

func TestAResumedKeyboardHandsKeysOnThroughTheSameChannel(t *testing.T) {
	terminal := openTerminal(t)

	keyboard := NewKeyboard(terminal)
	t.Cleanup(keyboard.Release)
	keys := keyboard.Keys()

	keyboard.Release()
	keyboard.Resume()
	keyboard.Resume()

	if _, err := io.WriteString(terminal, "a"); err != nil {
		t.Fatal(err)
	}

	select {
	case keypress := <-keys:
		if keypress.Code != key.Rune || keypress.Value != 'a' {
			t.Errorf("got %+v", keypress)
		}
	case <-time.After(time.Second):
		t.Fatal("the resumed keyboard never handed the keypress on")
	}
}

func TestAKeyboardWhoseTerminalGoesAwayClosesItsKeys(t *testing.T) {
	controller, terminal := ptytest.Open(t)

	keyboard := NewKeyboard(terminal)
	t.Cleanup(keyboard.Release)
	_ = controller.Close()

	select {
	case _, isOpen := <-keyboard.Keys():
		if isOpen {
			t.Error("a keypress arrived from a terminal that went away")
		}
	case <-time.After(time.Second):
		t.Fatal("the keys stayed open after the terminal went away")
	}
}
