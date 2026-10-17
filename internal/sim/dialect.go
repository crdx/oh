package sim

import (
	"net/http"
	"strings"
	"time"
)

const (
	Responses       = "responses"
	Completions     = "completions"
	Messages        = "messages"
	GenerateContent = "generate-content"
)

type Dialect interface {
	Name() string

	Path() string

	Read(request *http.Request, raw []byte) (Request, bool)

	Check(scenario *Scenario, asked Request) string

	Play(stream *Stream, scenario *Scenario, turn Turn)

	Exhausted(stream *Stream, message string)
}

func Dialects() []Dialect {
	return []Dialect{responsesDialect{}, completionsDialect{}, messagesDialect{}, generateContentDialect{}}
}

type Stream struct {
	send   func(string)
	pace   time.Duration
	nextID func(prefix string) string
}

func (self *Stream) Send(event string) {
	self.send(event)
}

func (self *Stream) Type(as func(string) string, text string) {
	for i, word := range strings.SplitAfter(text, " ") {
		if word == "" {
			continue
		}

		if i > 0 {
			time.Sleep(self.pace)
		}

		self.Send(as(word))
	}
}

func (self *Stream) ID(prefix string) string {
	return self.nextID(prefix)
}

func unansweredCall(input []Entry) string {
	answeredCalls := map[string]bool{}

	for _, entry := range input {
		if entry.Type == CallOutput {
			answeredCalls[entry.CallID] = true
		}
	}

	for _, entry := range input {
		if entry.Type == CallMade && !answeredCalls[entry.CallID] {
			return entry.CallID
		}
	}

	return ""
}
