package conditions

import (
	"encoding/json"
	"strings"

	"crdx.org/io/internal/app/access"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/pkg/agent"
)

type Conditions struct {
	UnixSockets bool `json:"unix_sockets"`
	IPv6        bool `json:"ipv6"`
	Interactive bool `json:"interactive"`
}

func Probe(isYolo bool, isInteractive bool) Conditions {
	return Conditions{
		UnixSockets: isYolo || sandbox.AreUnixSocketsReachable(),
		IPv6:        sandbox.IsIPv6Reachable(),
		Interactive: isInteractive,
	}
}

type State struct {
	state *access.State[Conditions]
}

func NewRestored(current Conditions, knownConditions Conditions) *State {
	return &State{state: access.NewRestored(current, knownConditions, definition())}
}

type Restoration struct {
	State     *State
	Change    agent.Event
	IsChanged bool
}

func Restore(
	createdConditions *Conditions,
	events []agent.Event,
	current Conditions,
) (Restoration, error) {
	knownConditions := current
	if createdConditions != nil {
		knownConditions = *createdConditions
	}
	if recordedConditions, found := LastRecorded(events); found {
		knownConditions = recordedConditions
	}

	change, err := ChangeEvent(knownConditions, current)
	if err != nil {
		return Restoration{}, err
	}

	_, isChanged := Notice(change)

	return Restoration{
		State:     NewRestored(current, knownConditions),
		Change:    change,
		IsChanged: isChanged,
	}, nil
}

func (self *State) Peek() string {
	return self.state.Peek()
}

func (self *State) Inject() string {
	return self.state.Inject()
}

func definition() access.Definition[Conditions] {
	return access.Definition[Conditions]{
		Clone:    func(current Conditions) Conditions { return current },
		Describe: describeChanges,
	}
}

func describeChanges(knownConditions Conditions, current Conditions) string {
	return strings.Join(changeNotices(knownConditions, current), " ")
}

func changeNotices(knownConditions Conditions, current Conditions) []string {
	var notices []string

	if knownConditions.UnixSockets != current.UnixSockets {
		notices = append(notices, unixSocketNotice(current.UnixSockets))
	}

	if knownConditions.IPv6 != current.IPv6 {
		notices = append(notices, addressNotice(current.IPv6))
	}

	if knownConditions.Interactive != current.Interactive {
		notices = append(notices, interactionNotice(current.Interactive))
	}

	return notices
}

func unixSocketNotice(areTheyReachable bool) string {
	if areTheyReachable {
		return "Unix sockets now work under /tmp, not the workspace."
	}

	return "Unix sockets no longer work."
}

func addressNotice(isIPv6Reachable bool) string {
	if isIPv6Reachable {
		return "IPv6 available; ::1 reaches the sandbox loopback."
	}

	return "IPv6 unavailable; use 127.0.0.1."
}

func interactionNotice(isInteractive bool) string {
	if isInteractive {
		return "Session is interactive; questions and approvals are available."
	}

	return "Session is non-interactive; questions and approvals are unavailable."
}

const Change agent.Kind = "conditions_change"

type eventState struct {
	KnownConditions Conditions `json:"known"`
	Current         Conditions `json:"current"`
}

func ChangeEvent(knownConditions Conditions, current Conditions) (agent.Event, error) {
	state, err := json.Marshal(eventState{KnownConditions: knownConditions, Current: current})
	if err != nil {
		return agent.Event{}, err
	}

	return agent.Event{Kind: Change, State: state}, nil
}

func decodeEvent(event agent.Event) (Conditions, error) {
	var state eventState
	if err := json.Unmarshal(event.State, &state); err != nil {
		return Conditions{}, err
	}

	return state.Current, nil
}

func LastRecorded(events []agent.Event) (Conditions, bool) {
	return access.LastRecorded(events, Change, decodeEvent)
}

func Summary(event agent.Event) (string, bool) {
	if event.Kind != Change {
		return "", false
	}

	var state eventState
	if err := json.Unmarshal(event.State, &state); err != nil {
		return "", false
	}

	var facilities []string
	if state.KnownConditions.UnixSockets != state.Current.UnixSockets {
		facilities = append(facilities, "unix sockets")
	}
	if state.KnownConditions.IPv6 != state.Current.IPv6 {
		facilities = append(facilities, "ipv6")
	}
	if state.KnownConditions.Interactive != state.Current.Interactive {
		facilities = append(facilities, "asking the user")
	}

	if len(facilities) == 0 {
		return "", false
	}

	return strings.Join(facilities, ", "), true
}

func Notice(event agent.Event) ([]string, bool) {
	if event.Kind != Change {
		return nil, false
	}

	var state eventState
	if err := json.Unmarshal(event.State, &state); err != nil {
		return nil, false
	}

	notices := changeNotices(state.KnownConditions, state.Current)

	return notices, len(notices) > 0
}
