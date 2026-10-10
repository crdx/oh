package agent

import (
	"context"
	"strings"
	"sync"
)

const InterjectionSeparator = "\n\n"

type Interjections struct {
	mutex    sync.Mutex
	messages []string
	notes    []Note
	reminder Reminder
	arrival  chan struct{}
}

type interjectionsKey struct{}

func MessageArrival(ctx context.Context) <-chan struct{} {
	interjections, _ := ctx.Value(interjectionsKey{}).(*Interjections)
	return interjections.Arrival()
}

func (self *Interjections) Arrival() <-chan struct{} {
	if self == nil {
		return nil
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.arrival == nil {
		self.arrival = make(chan struct{})
	}
	if len(self.messages) > 0 {
		signal := make(chan struct{})
		close(signal)
		return signal
	}

	return self.arrival
}

type Reminder struct {
	Note  Note
	Until func(Event) bool
}

func (self *Interjections) Remind(reminder Reminder) bool {
	if self == nil || reminder.Note.Text == "" || reminder.Until == nil {
		return false
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.reminder = reminder

	return true
}

func (self *Interjections) Note(note Note) bool {
	if self == nil || note.Text == "" {
		return false
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.notes = append(self.notes, note)

	return true
}

func (self *Interjections) TakeNotes() ([]Note, bool) {
	if self == nil {
		return nil, false
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.notes) == 0 {
		return nil, false
	}

	notes := self.notes
	self.notes = nil

	return notes, true
}

func (self *Interjections) Add(text string) bool {
	if self == nil || text == "" {
		return false
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.messages = append(self.messages, text)
	if self.arrival != nil {
		close(self.arrival)
		self.arrival = nil
	}

	return true
}

func (self *Interjections) HasMessages() bool {
	if self == nil {
		return false
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	return len(self.messages) > 0
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

func (self *Interjections) observe(event Event) {
	if self == nil {
		return
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.reminder.Until != nil && self.reminder.Until(event) {
		self.reminder = Reminder{}
	}
}

func (self *Interjections) takeReminder() (Note, bool) {
	if self == nil {
		return Note{}, false
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.reminder.Note.Text == "" {
		return Note{}, false
	}

	note := self.reminder.Note
	self.reminder = Reminder{}

	return note, true
}
