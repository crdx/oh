package record

import (
	"encoding/json"
	"errors"
	"fmt"

	"crdx.org/oh/pkg/session"

	"crdx.org/oh/pkg/agent"
)

type Session interface {
	IsPersisted() bool
	Name() string
	Event(event agent.Event) error
	Item(item json.RawMessage) error
	CompleteTurn(summary session.TurnSummary) error
	TakeWarnings() []error
}

type Recorder struct {
	session       Session
	flushBoundary int
}

func New(session Session) *Recorder {
	return &Recorder{session: session}
}

func (self *Recorder) IsPersisted() bool {
	return self.session.IsPersisted()
}

func (self *Recorder) Name() string {
	return self.session.Name()
}

func (self *Recorder) Resume(storedItems int) {
	self.flushBoundary = storedItems
}

func (self *Recorder) Event(event agent.Event) error {
	return self.session.Event(event)
}

func (self *Recorder) StoreItems(items []json.RawMessage) error {
	if len(items) < self.flushBoundary {
		return errors.New("the provider replaced append-only conversation state")
	}

	for _, item := range items[self.flushBoundary:] {
		if err := self.session.Item(item); err != nil {
			return fmt.Errorf("the conversation state could not be stored: %w", err)
		}
		self.flushBoundary++
	}

	return nil
}

func (self *Recorder) CompleteTurn(summary session.TurnSummary) error {
	return self.session.CompleteTurn(summary)
}

func (self *Recorder) TakeWarnings() []error {
	return self.session.TakeWarnings()
}
