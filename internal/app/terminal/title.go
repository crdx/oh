package terminal

import (
	"io"
	"strings"
	"sync"
	"unicode"

	"crdx.org/io/internal/app/ansi"
	"crdx.org/io/internal/app/tty"
)

const (
	pushTitle = ansi.PushTitle
	popTitle  = ansi.PopTitle
)

type title struct {
	writer io.Writer
	mutex  sync.Mutex

	isTerminal bool
}

func newTitle(writer io.Writer) *title {
	return &title{writer: writer, isTerminal: tty.Is(writer)}
}

func (self *title) Begin(text string) func() {
	if !self.isTerminal {
		return func() {}
	}

	self.mutex.Lock()
	self.write(pushTitle + titleSequence(text))
	self.mutex.Unlock()

	return func() {
		self.mutex.Lock()
		defer self.mutex.Unlock()

		self.write(popTitle)
	}
}

func (self *title) Set(text string) {
	if !self.isTerminal {
		return
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.write(titleSequence(text))
}

func titleSequence(text string) string {
	text = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, text)

	return "\x1b]2;" + text + "\x1b\\"
}

func (self *title) write(text string) {
	_, _ = io.WriteString(self.writer, text)
}
