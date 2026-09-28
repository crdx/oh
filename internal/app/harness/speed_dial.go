package harness

import (
	"slices"

	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/key"
)

type speedDial struct {
	entries      []string
	nextIndex    int
	selectedText string
	isCycling    bool
}

func (self *speedDial) Configure(entries []string) {
	if slices.Equal(self.entries, entries) {
		return
	}
	self.entries = slices.Clone(entries)
	self.stop()
}

func (self *speedDial) Apply(inputLine *edit.Input, keypress key.Key) bool {
	if keypress != tabKey {
		return false
	}
	if len(self.entries) == 0 {
		self.stop()
		return false
	}

	if self.isCycling && inputLine.Text() != self.selectedText {
		self.stop()
	}
	if !self.isCycling && inputLine.Text() != "" {
		return false
	}

	self.selectedText = self.entries[self.nextIndex]
	self.nextIndex = (self.nextIndex + 1) % len(self.entries)
	self.isCycling = true
	inputLine.SetText(self.selectedText)

	return true
}

func (self *speedDial) stop() {
	self.nextIndex = 0
	self.selectedText = ""
	self.isCycling = false
}
