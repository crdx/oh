package record

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/session"
)

type testSession struct {
	name            string
	isPersisted     bool
	items           []string
	failAt          int
	warnings        []error
	eventWrites     []agent.Event
	turnCompletions int
}

func (self *testSession) IsPersisted() bool { return self.isPersisted }
func (self *testSession) Name() string      { return self.name }

func (self *testSession) Event(event agent.Event) error {
	self.eventWrites = append(self.eventWrites, event)
	return nil
}

func (self *testSession) Item(item json.RawMessage) error {
	if len(self.items) == self.failAt {
		return errors.New("item failed")
	}
	self.items = append(self.items, string(item))
	return nil
}

func (self *testSession) CompleteTurn(summary session.TurnSummary) error {
	self.turnCompletions++
	return nil
}

func (self *testSession) TakeWarnings() []error {
	warnings := self.warnings
	self.warnings = nil
	return warnings
}

func TestRecorderRetriesProviderItemsFromTheLastSuccessfulBoundary(t *testing.T) {
	session := &testSession{failAt: 1}
	recorder := New(session)
	items := []json.RawMessage{json.RawMessage(`{"id":1}`), json.RawMessage(`{"id":2}`), json.RawMessage(`{"id":3}`)}

	if err := recorder.StoreItems(items); err == nil || !strings.Contains(err.Error(), "item failed") {
		t.Fatalf("got %v, want the item failure", err)
	}
	if !slices.Equal(session.items, []string{`{"id":1}`}) {
		t.Fatalf("stored %v before the failure", session.items)
	}

	session.failAt = -1
	if err := recorder.StoreItems(items); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(session.items, []string{`{"id":1}`, `{"id":2}`, `{"id":3}`}) {
		t.Errorf("stored %v after retry", session.items)
	}
}

func TestRecorderRejectsReplacedProviderHistory(t *testing.T) {
	recorder := New(&testSession{failAt: -1})
	recorder.Resume(2)

	err := recorder.StoreItems([]json.RawMessage{json.RawMessage(`{"id":1}`)})
	if err == nil || !strings.Contains(err.Error(), "replaced append-only conversation state") {
		t.Errorf("got %v", err)
	}
}

func TestRecorderForwardsSessionIdentityEventsAndWarnings(t *testing.T) {
	warning := errors.New("transcript disabled")
	session := &testSession{name: "tame-impala", isPersisted: true, failAt: -1, warnings: []error{warning}}
	recorder := New(session)
	event := agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}

	if !recorder.IsPersisted() || recorder.Name() != "tame-impala" {
		t.Errorf("lost session identity")
	}
	if err := recorder.Event(event); err != nil {
		t.Fatal(err)
	}
	if len(session.eventWrites) != 1 || session.eventWrites[0].Kind != event.Kind || session.eventWrites[0].Text != event.Text {
		t.Errorf("wrote events %v", session.eventWrites)
	}
	if warnings := recorder.TakeWarnings(); len(warnings) != 1 || !errors.Is(warnings[0], warning) {
		t.Errorf("got warnings %v", warnings)
	}
	if warnings := recorder.TakeWarnings(); len(warnings) != 0 {
		t.Errorf("warnings were not cleared: %v", warnings)
	}
}
