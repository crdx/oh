package subagentrecord

import (
	"encoding/json"
	"strings"

	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/pkg/agent"
)

const (
	Started            agent.Kind = "subagent_started"
	Sent               agent.Kind = "subagent_message_sent"
	Finished           agent.Kind = "subagent_finished"
	Returned           agent.Kind = "subagent_report_returned"
	ReportsDelivered   agent.Kind = "subagent_completion_submitted"
	ShellWithdrawnStop agent.Kind = "subagent_access_stop"
)

type State string

const (
	Running  State = "running"
	Done     State = "done"
	Failed   State = "failed"
	Stopping State = "stopping"
	Stopped  State = "stopped"
	Ended    State = "ended"
)

func (self State) IsLive() bool {
	return self == Running || self == Stopping
}

func (self State) Status() agent.Status {
	switch self {
	case Done:
		return agent.SuccessStatus
	case Failed:
		return agent.ErrorStatus
	case Running, Stopping, Stopped, Ended:
		return agent.CancelledStatus
	}
	return agent.CancelledStatus
}

type Origin struct {
	Choice    model.Choice `json:"choice"`
	Workspace string       `json:"workspace"`
	Intent    string       `json:"intent,omitempty"`
}

func StartedEvent(name string, sessionID string, task string, origin Origin) agent.Event {
	encodedOrigin, err := json.Marshal(origin)
	if err != nil {
		return agent.Event{}
	}
	return agent.Event{Kind: Started, Subagent: name, ID: sessionID, Text: task, State: encodedOrigin}
}

func DecodeOrigin(event agent.Event) (Origin, bool) {
	var origin Origin
	if event.Kind != Started || json.Unmarshal(event.State, &origin) != nil {
		return Origin{}, false
	}
	return origin, true
}

func SentEvent(name string, message string) agent.Event {
	return agent.Event{Kind: Sent, Subagent: name, Text: message}
}

type Outcome struct {
	Usage *agent.Usage `json:"usage,omitempty"`
}

func FinishedEvent(name string, state State, answer string, failure *agent.Failure, usage *agent.Usage) agent.Event {
	encodedOutcome, err := json.Marshal(Outcome{Usage: usage})
	if err != nil {
		return agent.Event{}
	}
	return agent.Event{Kind: Finished, Subagent: name, Name: string(state), Text: answer, Failure: failure, Status: state.Status(), State: encodedOutcome}
}

func UsageOf(event agent.Event) *agent.Usage {
	var outcome Outcome
	if event.Kind != Finished || json.Unmarshal(event.State, &outcome) != nil {
		return nil
	}
	return outcome.Usage
}

func Failure(message string) *agent.Failure {
	return &agent.Failure{Kind: agent.GenericFailure, Message: message}
}

func FailureOf(event agent.Event) string {
	if event.Failure == nil {
		return ""
	}
	return event.Failure.Text()
}

func ReturnedEvent(name string) agent.Event {
	return agent.Event{Kind: Returned, Subagent: name}
}

func AddUsage(total *agent.Usage, usage agent.Usage) {
	total.InputTokens += usage.InputTokens
	total.OutputTokens += usage.OutputTokens
	if usage.Cache == nil {
		return
	}
	if total.Cache == nil {
		total.Cache = &agent.CacheUsage{}
	}
	total.Cache.ReadTokens += usage.Cache.ReadTokens
	total.Cache.WriteTokens += usage.Cache.WriteTokens
}

func IsCounted(event agent.Event) bool {
	return event.Usage != nil && event.Kind != agent.CacheRebuildEvent
}

func ShellWithdrawnStopNotice(event agent.Event) (string, bool) {
	if event.Kind != ShellWithdrawnStop || event.Name == "" {
		return "", false
	}
	return namedSubagents(event.Name) + " stopped because shell execution was withdrawn.", true
}

func CompletionHeading(names []string) string {
	return namedSubagents(strings.Join(names, ",")) + " finished:"
}

func namedSubagents(names string) string {
	if strings.Contains(names, ",") {
		return "Subagents " + strings.ReplaceAll(names, ",", ", ")
	}
	return "Subagent " + names
}

type Report struct {
	Name    string `json:"name"`
	State   State  `json:"state"`
	Answer  string `json:"answer,omitempty"`
	Failure string `json:"failure,omitempty"`
}

type delivery struct {
	Reports []Report `json:"reports"`
}

func DeliveryEvent(names []string, text string, reports []Report) agent.Event {
	encodedDelivery, err := json.Marshal(delivery{Reports: reports})
	if err != nil {
		return agent.Event{}
	}
	return agent.Event{Kind: ReportsDelivered, Name: strings.Join(names, ","), Text: text, State: encodedDelivery}
}

func ReportsOf(event agent.Event) ([]Report, bool) {
	var record delivery
	if event.Kind != ReportsDelivered || json.Unmarshal(event.State, &record) != nil || len(record.Reports) == 0 {
		return nil, false
	}
	return record.Reports, true
}

func (self Report) Notice() string {
	var parts []string
	if self.Answer != "" {
		parts = append(parts, self.Answer)
	}
	state := strings.ToUpper(string(self.State)[:1]) + string(self.State)[1:]
	switch {
	case self.Failure != "":
		parts = append(parts, state+": "+self.Failure+".")
	case self.Answer == "":
		parts = append(parts, state+" without an answer.")
	}
	return "Subagent " + self.Name + " interjects:\n\n" + strings.Join(parts, "\n\n")
}
