package tty

import (
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
	"golang.org/x/term"

	"crdx.org/io/internal/app/key"
)

var ErrNotTerminal = errors.New("not a terminal")

func Is(stream any) bool {
	file, ok := stream.(*os.File)

	return ok && term.IsTerminal(int(file.Fd()))
}

var controllingTerminal = "/dev/tty"

func Keyboard(input *os.File) (*os.File, func()) {
	if Is(input) {
		return input, func() {}
	}

	terminal, err := os.Open(controllingTerminal)
	if err != nil {
		return input, func() {}
	}

	return terminal, func() { _ = terminal.Close() }
}

func SuppressEcho(terminal *os.File) (func(), error) {
	if !Is(terminal) {
		return nil, ErrNotTerminal
	}

	state, err := unix.IoctlGetTermios(int(terminal.Fd()), unix.TCGETS)
	if err != nil {
		return nil, err
	}

	withoutEcho := *state
	withoutEcho.Lflag &^= unix.ECHO
	if err := unix.IoctlSetTermios(int(terminal.Fd()), unix.TCSETS, &withoutEcho); err != nil {
		return nil, err
	}

	return func() { _ = unix.IoctlSetTermios(int(terminal.Fd()), unix.TCSETS, state) }, nil
}

func Raw(terminal *os.File, screen io.Writer) (func(), error) {
	if !Is(terminal) || !Is(screen) {
		return nil, ErrNotTerminal
	}

	terminalState, err := term.MakeRaw(int(terminal.Fd()))
	if err != nil {
		return nil, err
	}

	_, _ = io.WriteString(screen, key.Enable)

	return func() {
		_, _ = io.WriteString(screen, key.Disable)
		_ = term.Restore(int(terminal.Fd()), terminalState)
	}, nil
}
