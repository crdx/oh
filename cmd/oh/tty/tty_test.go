package tty

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func TestNothingButAFileCanBeATerminal(t *testing.T) {
	for name, stream := range map[string]any{
		"a buffer":   &bytes.Buffer{},
		"nothing":    nil,
		"a pipe end": pipe(t),
	} {
		if Is(stream) {
			t.Errorf("%s: expected no terminal", name)
		}
	}
}

func TestRawRefusesAScreenThatIsNotATerminal(t *testing.T) {
	var screen bytes.Buffer

	restore, err := Raw(pty(t), &screen)
	if !errors.Is(err, ErrNotTerminal) {
		t.Errorf("expected the terminal to be refused, got %v", err)
	}

	if restore != nil {
		t.Error("expected nothing to undo")
	}

	if screen.Len() != 0 {
		t.Errorf("expected nothing written, got %q", screen.String())
	}
}

func TestSuppressEchoRestoresTheTerminalState(t *testing.T) {
	terminal := pty(t)
	before, err := unix.IoctlGetTermios(int(terminal.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}

	restore, err := SuppressEcho(terminal)
	if err != nil {
		t.Fatal(err)
	}
	during, err := unix.IoctlGetTermios(int(terminal.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	if during.Lflag&unix.ECHO != 0 {
		t.Error("terminal echo was not suppressed")
	}

	restore()
	after, err := unix.IoctlGetTermios(int(terminal.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Error("suppressing echo did not restore the terminal state")
	}
}

func TestRawRestoresTheTerminalState(t *testing.T) {
	terminal := pty(t)
	before, err := term.GetState(int(terminal.Fd()))
	if err != nil {
		t.Fatal(err)
	}

	restore, err := Raw(terminal, terminal)
	if err != nil {
		t.Fatal(err)
	}
	restore()

	after, err := term.GetState(int(terminal.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Error("raw mode did not restore the terminal state")
	}
}

func TestATerminalIsItsOwnKeyboard(t *testing.T) {
	terminal := pty(t)

	keyboard, release := Keyboard(terminal)
	defer release()

	if keyboard != terminal {
		t.Error("expected a terminal to be read from directly")
	}
}

func TestAPipedInputTakesTheControllingTerminalForItsKeyboard(t *testing.T) {
	piped := pipe(t)
	useControllingTerminal(t, pseudoTerminalPath(t))

	keyboard, release := Keyboard(piped)
	defer release()

	if keyboard == piped {
		t.Fatal("expected the controlling terminal rather than the pipe")
	}
	if !Is(keyboard) {
		t.Error("expected the controlling terminal to be a terminal")
	}
}

func TestAPipedInputKeepsItselfWhereThereIsNoControllingTerminal(t *testing.T) {
	piped := pipe(t)
	useControllingTerminal(t, filepath.Join(t.TempDir(), "absent"))

	keyboard, release := Keyboard(piped)
	defer release()

	if keyboard != piped {
		t.Error("expected the piped input back when there is no controlling terminal")
	}
}

func useControllingTerminal(t *testing.T, path string) {
	t.Helper()

	previous := controllingTerminal
	t.Cleanup(func() { controllingTerminal = previous })
	controllingTerminal = path
}

func pseudoTerminalPath(t *testing.T) string {
	t.Helper()

	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal to test against: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })

	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Skipf("could not name the pseudo-terminal: %v", err)
	}

	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Skipf("could not unlock the pseudo-terminal: %v", err)
	}

	return fmt.Sprintf("/dev/pts/%d", number)
}

func pty(t *testing.T) *os.File {
	t.Helper()

	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal to test against: %v", err)
	}

	t.Cleanup(func() { _ = master.Close() })

	if !Is(master) {
		t.Fatal("expected the master end of a pseudo-terminal to be a terminal")
	}

	return master
}

func pipe(t *testing.T) *os.File {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() {
		_ = reader.Close()
		_ = writer.Close()
	})

	return writer
}
