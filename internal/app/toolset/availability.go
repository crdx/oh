package toolset

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"crdx.org/oh/internal/app/access"
	"crdx.org/oh/internal/app/markdown"
	"crdx.org/oh/pkg/agent"
)

type ToolStatus string

const (
	ToolAvailable ToolStatus = "available"
	ToolChanged   ToolStatus = "changed"
	ToolMissing   ToolStatus = "missing"
)

type Availability map[string]ToolStatus

type CompatibilityTransition struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type AvailabilityRestoration struct {
	Change    agent.Event
	IsChanged bool
}

const AvailabilityChange agent.Kind = "tool_availability_change"

type availabilityEventState struct {
	KnownAvailability   Availability                       `json:"known"`
	CurrentAvailability Availability                       `json:"current"`
	Transitions         map[string]CompatibilityTransition `json:"transitions,omitempty"`
}

func RestoreAvailability(
	events []agent.Event,
	current Availability,
	transitions ...map[string]CompatibilityTransition,
) (AvailabilityRestoration, error) {
	knownAvailability := make(Availability, len(current))
	for name := range current {
		knownAvailability[name] = ToolAvailable
	}
	if recordedAvailability, isFound := LastRecordedAvailability(events); isFound {
		knownAvailability = recordedAvailability
	}

	change, err := AvailabilityChangeEvent(knownAvailability, current, transitions...)
	if err != nil {
		return AvailabilityRestoration{}, err
	}
	_, isChanged := AvailabilityNotice(change)
	return AvailabilityRestoration{Change: change, IsChanged: isChanged}, nil
}

func AvailabilityChangeEvent(
	knownAvailability Availability,
	current Availability,
	transitions ...map[string]CompatibilityTransition,
) (agent.Event, error) {
	var compatibilityTransitions map[string]CompatibilityTransition
	if len(transitions) > 0 {
		compatibilityTransitions = maps.Clone(transitions[0])
	}
	state, err := json.Marshal(availabilityEventState{
		KnownAvailability:   maps.Clone(knownAvailability),
		CurrentAvailability: maps.Clone(current),
		Transitions:         compatibilityTransitions,
	})
	if err != nil {
		return agent.Event{}, err
	}
	return agent.Event{Kind: AvailabilityChange, State: state}, nil
}

func LastRecordedAvailability(events []agent.Event) (Availability, bool) {
	return access.LastRecorded(events, AvailabilityChange, decodeAvailability)
}

func decodeAvailability(event agent.Event) (Availability, error) {
	var state availabilityEventState
	if err := json.Unmarshal(event.State, &state); err != nil {
		return nil, err
	}
	return state.CurrentAvailability, nil
}

func AvailabilityNotice(event agent.Event) ([]string, bool) {
	if event.Kind != AvailabilityChange {
		return nil, false
	}

	var state availabilityEventState
	if err := json.Unmarshal(event.State, &state); err != nil {
		return nil, false
	}

	var notices []string
	for _, name := range availabilityNames(state.KnownAvailability, state.CurrentAvailability) {
		knownStatus := state.KnownAvailability[name]
		currentStatus := state.CurrentAvailability[name]
		if knownStatus == currentStatus {
			continue
		}
		notices = append(notices, availabilityNotice(name, currentStatus, state.Transitions[name]))
	}
	return notices, len(notices) > 0
}

func AvailabilitySummary(event agent.Event) (string, bool) {
	if event.Kind != AvailabilityChange {
		return "", false
	}

	var state availabilityEventState
	if err := json.Unmarshal(event.State, &state); err != nil {
		return "", false
	}

	var names []string
	for _, name := range availabilityNames(state.KnownAvailability, state.CurrentAvailability) {
		if state.KnownAvailability[name] != state.CurrentAvailability[name] {
			names = append(names, name)
		}
	}
	return strings.Join(names, ", "), len(names) > 0
}

func availabilityNames(left Availability, right Availability) []string {
	names := make(map[string]struct{}, len(left)+len(right))
	for name := range left {
		names[name] = struct{}{}
	}
	for name := range right {
		names[name] = struct{}{}
	}
	return slices.Sorted(maps.Keys(names))
}

func availabilityNotice(name string, status ToolStatus, transition CompatibilityTransition) string {
	markedName := markdown.CodeSpan(name)
	switch status {
	case ToolAvailable:
		return "The " + markedName + " tool is available again."
	case ToolChanged:
		notice := "The " + markedName + " tool changed since this conversation began and is disabled for this conversation."
		if transition.From != "" && transition.To != "" {
			notice += " Compatibility changed from " + markdown.CodeSpan(transition.From) +
				" to " + markdown.CodeSpan(transition.To) + "."
		}
		return notice
	case ToolMissing:
		return "The " + markedName + " tool is no longer installed and is disabled for this conversation."
	default:
		return ""
	}
}

func changedToolReason(name string) string {
	return "the " + name + " tool changed since this conversation began and is disabled for this conversation"
}

func missingToolReason(name string) string {
	return "the " + name + " tool is no longer installed and is disabled for this conversation"
}
