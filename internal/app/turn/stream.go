package turn

import (
	"context"
	"time"

	"crdx.org/oh/internal/util"
	"crdx.org/oh/pkg/agent"
)

type Event struct {
	Update agent.Update
	Err    error
}

type State struct {
	Running     bool
	IsCancelled bool
	Err         error
	Reason      error
	StartedAt   time.Time
	FinishedAt  time.Time
	Timing      Timing
}

type Stream struct {
	events        chan Event
	cancel        context.CancelCauseFunc
	state         State
	interjections *agent.Interjections
}

type Timing struct {
	UserTurn  time.Duration
	ModelTurn time.Duration
}

func Start(assistant *agent.Agent, message string, timing Timing) *Stream {
	streamContext, cancel := context.WithCancelCause(context.Background())
	stream := Adopt(make(chan Event), cancel, State{Running: true, StartedAt: util.WallClock(time.Now()), Timing: timing})

	go func() {
		defer close(stream.events)
		defer cancel(nil)
		for update, err := range assistant.Stream(streamContext, message, stream.interjections) {
			stream.events <- Event{Update: update, Err: err}
			if err != nil {
				return
			}
		}
	}()
	return stream
}

func Adopt(events chan Event, cancel context.CancelCauseFunc, state State) *Stream {
	return &Stream{
		events:        events,
		cancel:        cancel,
		state:         state,
		interjections: &agent.Interjections{},
	}
}

func (self *Stream) Events() <-chan Event {
	if self == nil {
		return nil
	}
	return self.events
}

func (self *Stream) Running() bool   { return self != nil && self.state.Running }
func (self *Stream) Cancelled() bool { return self != nil && self.state.IsCancelled }

func (self *Stream) Interjections() *agent.Interjections {
	if self == nil {
		return nil
	}
	return self.interjections
}

func (self *Stream) Interject(text string) bool {
	if !self.Running() {
		return false
	}

	return self.interjections.Add(text)
}

func (self *Stream) Note(text string) bool {
	if !self.Running() {
		return false
	}

	return self.interjections.Note(text)
}

func (self *Stream) TakeNotes() (string, bool) {
	return self.Interjections().TakeNotes()
}

func (self *Stream) GetInterjections() []string {
	return self.Interjections().Peek()
}

func (self *Stream) TakeInterjections() (string, bool) {
	return self.Interjections().Take()
}

func (self *Stream) TakeLastInterjection() (string, bool) {
	return self.Interjections().TakeLast()
}

func (self *Stream) Error() error {
	if self == nil {
		return nil
	}
	return self.state.Err
}

func (self *Stream) Timing() (Timing, bool) {
	if self == nil || self.state.StartedAt.IsZero() {
		return Timing{}, false
	}
	timing := self.state.Timing
	if self.state.FinishedAt.IsZero() {
		timing.ModelTurn = time.Since(self.state.StartedAt)
		return timing, true
	}
	timing.UserTurn = time.Since(self.state.FinishedAt)
	timing.ModelTurn = self.state.FinishedAt.Sub(self.state.StartedAt)
	return timing, true
}

func (self *Stream) Interrupt(reason error) bool {
	if !self.Running() {
		return false
	}
	self.state.IsCancelled = true
	self.state.Reason = reason
	self.cancel(reason)
	return true
}

func (self *Stream) Reason() error {
	if self == nil {
		return nil
	}
	return self.state.Reason
}

func (self *Stream) Observe(event Event) bool {
	if event.Err != nil {
		self.state.Err = event.Err
		return false
	}
	return true
}

func (self *Stream) SetCancelled(isCancelled bool) { self.state.IsCancelled = isCancelled }
func (self *Stream) MarkFinished(at time.Time)     { self.state.FinishedAt = util.WallClock(at) }

func (self *Stream) Finish() {
	self.state.Running = false
	self.events = nil
}
