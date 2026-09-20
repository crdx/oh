package interrupt

import (
	"errors"

	"crdx.org/oh/internal/stop"
	"crdx.org/oh/pkg/agent"
)

type Cause string

const (
	Escape       Cause = "escape"
	ControlD     Cause = "ctrl_d"
	Replacement  Cause = "replacement"
	AccessChange Cause = "access_change"
	SessionClose Cause = "session_close"
)

var sentences = map[Cause]string{
	Escape:       "the user pressed escape",
	ControlD:     "the user pressed ctrl+d",
	Replacement:  "the user sent another message",
	AccessChange: "access changed",
	SessionClose: "the session closed",
}

func Sentence(cause Cause) string {
	return sentences[cause]
}

func CauseSaying(sentence string) (Cause, bool) {
	for cause, causeSentence := range sentences {
		if causeSentence == sentence {
			return cause, true
		}
	}

	return "", false
}

type interruption struct {
	cause Cause
	err   error
}

func (self interruption) Error() string { return self.err.Error() }
func (self interruption) Unwrap() error { return self.err }
func (self interruption) Cause() Cause  { return self.cause }

func Because(cause Cause) error {
	return interruption{cause: cause, err: stop.Because(Sentence(cause))}
}

func CauseOf(err error) (Cause, bool) {
	if found, isInterruption := errors.AsType[interruption](err); isInterruption {
		return found.cause, true
	}

	return "", false
}

func Event(cause Cause) agent.Event {
	return agent.Event{Kind: agent.InterruptionEvent, Name: string(cause)}
}

func Reason(event agent.Event) string {
	if sentence := Sentence(Cause(event.Name)); sentence != "" {
		return sentence
	}

	return event.Text
}

func IsAnnounced(event agent.Event) bool {
	switch Cause(event.Name) {
	case Replacement, AccessChange, SessionClose:
		return false
	case Escape, ControlD:
		return true
	}

	return true
}

func Notice(event agent.Event) string {
	if reason := Reason(event); reason != "" {
		return "Turn stopped because " + reason + "."
	}

	return "Turn stopped."
}
