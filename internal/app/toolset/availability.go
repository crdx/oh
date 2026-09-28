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

type VersionChange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type AvailabilityRestoration struct {
	Change    agent.Event
	IsChanged bool
}

const AvailabilityChange agent.Kind = "tool_availability_change"

type availabilityEventState struct {
	KnownAvailability   Availability             `json:"known"`
	CurrentAvailability Availability             `json:"current"`
	VersionChanges      map[string]VersionChange `json:"version_changes,omitempty"`
}

func RestoreAvailability(
	events []agent.Event,
	current Availability,
	versionChanges ...map[string]VersionChange,
) (AvailabilityRestoration, error) {
	knownAvailability := make(Availability, len(current))
	for name := range current {
		knownAvailability[name] = ToolAvailable
	}
	if recordedAvailability, isFound := LastRecordedAvailability(events); isFound {
		knownAvailability = recordedAvailability
	}

	change, err := AvailabilityChangeEvent(knownAvailability, current, versionChanges...)
	if err != nil {
		return AvailabilityRestoration{}, err
	}
	_, isChanged := AvailabilityNotice(change)
	return AvailabilityRestoration{Change: change, IsChanged: isChanged}, nil
}

func AvailabilityChangeEvent(
	knownAvailability Availability,
	current Availability,
	versionChanges ...map[string]VersionChange,
) (agent.Event, error) {
	var recordedVersionChanges map[string]VersionChange
	if len(versionChanges) > 0 {
		recordedVersionChanges = maps.Clone(versionChanges[0])
	}
	state, err := json.Marshal(availabilityEventState{
		KnownAvailability:   maps.Clone(knownAvailability),
		CurrentAvailability: maps.Clone(current),
		VersionChanges:      recordedVersionChanges,
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
		notices = append(notices, availabilityNotice(name, currentStatus, state.VersionChanges[name]))
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

func availabilityNotice(name string, status ToolStatus, versionChange VersionChange) string {
	markedName := markdown.CodeSpan(name)
	switch status {
	case ToolAvailable:
		return "The " + markedName + " tool is available again."
	case ToolChanged:
		if versionChange.From != "" && versionChange.To != "" {
			return "The " + markedName + " tool changed from version " + markdown.CodeSpan(versionChange.From) +
				" to version " + markdown.CodeSpan(versionChange.To) + " and is disabled for the remainder of this conversation."
		}
		return "The " + markedName + " tool changed since this conversation began and is disabled for this conversation."
	case ToolMissing:
		return "The " + markedName + " tool is no longer installed and is disabled for this conversation."
	default:
		return ""
	}
}

func changedToolReason(name string, fromVersion string, toVersion string) string {
	return "the " + name + " tool changed from version " + fromVersion + " to version " + toVersion +
		" and is disabled for the remainder of this conversation"
}

func missingToolReason(name string) string {
	return "the " + name + " tool is no longer installed and is disabled for this conversation"
}
