package key

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"golang.org/x/sys/unix"
)

type Code int

const (
	Rune Code = iota
	Enter
	Backspace
	Delete
	Left
	Right
	Up
	Down
	Home
	End
	PageUp
	PageDown
	Escape
	PasteStart
	PasteEnd
	Clipboard
	FocusIn
	FocusOut
	Unknown
)

type Modifier int

const (
	Shift Modifier = 1 << iota
	Alt
	Ctrl
)

func (self Modifier) Has(mask Modifier) bool {
	return self&mask == mask
}

type Key struct {
	Code      Code
	Value     rune
	Mod       Modifier
	Clipboard *ClipboardReport
}

type Decoder struct {
	reader                *bufio.Reader
	hasEscapeContinuation func() bool
}

func NewDecoder(reader *bufio.Reader) *Decoder {
	return newDecoder(reader, nil)
}

func NewTerminalDecoder(reader *bufio.Reader, terminal *os.File) *Decoder {
	return newDecoder(reader, func() bool {
		return hasTerminalInput(terminal, escapeSequenceTimeout)
	})
}

func newDecoder(reader *bufio.Reader, hasEscapeContinuation func() bool) *Decoder {
	return &Decoder{reader: reader, hasEscapeContinuation: hasEscapeContinuation}
}

const (
	escapeByte            = '\x1b'
	bellByte              = '\x07'
	delByte               = '\x7f'
	escapeSequenceTimeout = 25 * time.Millisecond
)

func hasTerminalInput(terminal *os.File, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)

	for {
		remainingTime := time.Until(deadline)
		if remainingTime <= 0 {
			return false
		}

		descriptors := []unix.PollFd{{
			Fd:     int32(terminal.Fd()), //nolint:gosec // Unix file descriptors fit PollFd
			Events: unix.POLLIN,
		}}
		ready, err := unix.Poll(descriptors, max(1, int(remainingTime.Milliseconds())))
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil || ready == 0 {
			return false
		}

		return descriptors[0].Revents&unix.POLLIN != 0
	}
}

const (
	Enable  = "\x1b[>1u\x1b[?2004h\x1b[?1004h\x1b[?5522h"
	Disable = "\x1b[?5522l\x1b[?1004l\x1b[?2004l\x1b[<u"
)

func (self *Decoder) Next() (Key, error) {
	first, _, err := self.reader.ReadRune()
	if err != nil {
		return Key{}, err
	}

	if first == escapeByte {
		return self.escape()
	}

	if first == '\r' {
		self.swallow('\n')

		return Key{Code: Enter}, nil
	}

	return plain(first), nil
}

func (self *Decoder) swallow(value rune) {
	if self.reader.Buffered() == 0 {
		return
	}

	if next, err := self.reader.Peek(1); err == nil && rune(next[0]) == value {
		_, _ = self.reader.Discard(1)
	}
}

func plain(value rune) Key {
	switch {
	case value == '\n':
		return Key{Code: Enter}
	case value == '\t':
		return Key{Code: Rune, Value: '\t'}
	case value == delByte:
		return Key{Code: Backspace}
	case value >= 1 && value <= 26:
		return Key{Code: Rune, Value: value + 'a' - 1, Mod: Ctrl}
	case value < ' ':
		return Key{Code: Unknown}
	}

	return Key{Code: Rune, Value: value}
}

func (self *Decoder) escape() (Key, error) {
	if !self.escapeContinues() {
		return Key{Code: Escape}, nil
	}

	next, _, err := self.reader.ReadRune()
	if err != nil {
		return Key{}, err
	}

	switch next {
	case '[':
		return self.parameters()
	case 'O':
		return self.applicationCursor()
	case ']':
		return self.osCommand()
	}

	return self.alt(next), nil
}

const maxCommandBytes = 1 << 16

func (self *Decoder) osCommand() (Key, error) {
	var command strings.Builder
	isOverlong := false

	for {
		next, _, err := self.reader.ReadRune()
		if err != nil {
			return Key{}, err
		}

		if next == bellByte {
			break
		}

		if next == escapeByte {
			terminator, _, err := self.reader.ReadRune()
			if err != nil {
				return Key{}, err
			}
			if terminator == '\\' {
				break
			}
			return Key{Code: Unknown}, nil
		}

		if command.Len() >= maxCommandBytes {
			isOverlong = true
			continue
		}

		command.WriteRune(next)
	}

	if isOverlong {
		return Key{Code: Unknown}, nil
	}

	if keypress, isClipboard := clipboardReport(command.String()); isClipboard {
		return keypress, nil
	}

	return Key{Code: Unknown}, nil
}

func (self *Decoder) escapeContinues() bool {
	return self.reader.Buffered() > 0 ||
		self.hasEscapeContinuation != nil && self.hasEscapeContinuation()
}

func (self *Decoder) alt(value rune) Key {
	if value == '\r' {
		self.swallow('\n')
		return Key{Code: Enter, Mod: Alt}
	}

	keypress := plain(value)
	if keypress.Code != Unknown {
		keypress.Mod |= Alt
	}

	return keypress
}

func (self *Decoder) applicationCursor() (Key, error) {
	final, _, err := self.reader.ReadRune()
	if err != nil {
		return Key{}, err
	}

	code, isFound := letters[final]
	if !isFound {
		return Key{Code: Unknown}, nil
	}

	return Key{Code: code}, nil
}

func (self *Decoder) parameters() (Key, error) {
	var parameters strings.Builder

	for {
		next, _, err := self.reader.ReadRune()
		if err != nil {
			return Key{}, err
		}

		if (next >= '0' && next <= '9') || next == ';' || next == ':' {
			parameters.WriteRune(next)
			continue
		}

		return csi(parameters.String(), next), nil
	}
}

func csi(parameters string, final rune) Key {
	switch final {
	case 'u':
		return unicodeKey(parameters)
	case '~':
		return tilde(parameters)
	case 'I':
		return Key{Code: FocusIn}
	case 'O':
		return Key{Code: FocusOut}
	}

	code, isFound := letters[final]
	if !isFound {
		return Key{Code: Unknown}
	}

	return Key{Code: code, Mod: modifiers(parameters)}
}

var letters = map[rune]Code{
	'A': Up,
	'B': Down,
	'C': Right,
	'D': Left,
	'F': End,
	'H': Home,
}

var codepoints = map[int]Code{
	13:  Enter,
	27:  Escape,
	127: Backspace,
}

func unicodeKey(parameters string) Key {
	number, found := field(parameters, 0)
	if !found {
		return Key{Code: Unknown}
	}

	modifier := modifiers(parameters)

	if code, isFound := codepoints[number]; isFound {
		return Key{Code: code, Mod: modifier}
	}

	if number < 0 || number > unicode.MaxRune {
		return Key{Code: Unknown}
	}

	return Key{Code: Rune, Value: rune(number), Mod: modifier}
}

func tilde(parameters string) Key {
	number, _ := field(parameters, 0)

	switch number {
	case 1:
		return Key{Code: Home, Mod: modifiers(parameters)}
	case 3:
		return Key{Code: Delete, Mod: modifiers(parameters)}
	case 4:
		return Key{Code: End, Mod: modifiers(parameters)}
	case 5:
		return Key{Code: PageUp, Mod: modifiers(parameters)}
	case 6:
		return Key{Code: PageDown, Mod: modifiers(parameters)}
	case 200:
		return Key{Code: PasteStart}
	case 201:
		return Key{Code: PasteEnd}
	}

	return Key{Code: Unknown}
}

func modifiers(parameters string) Modifier {
	encodedModifier, found := field(parameters, 1)
	if !found || encodedModifier < 1 {
		return 0
	}

	return Modifier(encodedModifier - 1)
}

func field(parameters string, index int) (int, bool) {
	fields := strings.Split(parameters, ";")
	if index >= len(fields) {
		return 0, false
	}

	head, _, _ := strings.Cut(fields[index], ":")

	value, err := strconv.Atoi(head)
	if err != nil {
		return 0, false
	}

	return value, true
}
