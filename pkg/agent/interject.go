package agent

import (
	"strings"
	"sync"
)

const InterjectionSeparator = "\n\n"

type Interjections struct {
	mutex    sync.Mutex
	messages []string
	notes    []string
}

func (self *Interjections) Note(text string) bool {
	if self == nil || text == "" {
		return false
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.notes = append(self.notes, text)

	return true
}

func (self *Interjections) TakeNotes() (string, bool) {
	if self == nil {
		return "", false
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.notes) == 0 {
		return "", false
	}

	wholeQueue := strings.Join(self.notes, InterjectionSeparator)
	self.notes = nil

	return wholeQueue, true
}

func (self *Interjections) Add(text string) bool {
	if self == nil || text == "" {
		return false
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.messages = append(self.messages, text)

	return true
}

func (self *Interjections) Peek() []string {
	if self == nil {
		return nil
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	return append([]string(nil), self.messages...)
}

func (self *Interjections) Take() (string, bool) {
	if self == nil {
		return "", false
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.messages) == 0 {
		return "", false
	}

	wholeQueue := strings.Join(self.messages, InterjectionSeparator)
	self.messages = nil

	return wholeQueue, true
}

func (self *Interjections) TakeLast() (string, bool) {
	if self == nil {
		return "", false
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.messages) == 0 {
		return "", false
	}

	last := self.messages[len(self.messages)-1]
	self.messages = self.messages[:len(self.messages)-1]

	return last, true
}
