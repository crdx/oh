package caps

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"crdx.org/oh/internal/app/access"
	"crdx.org/oh/internal/app/markdown"
	"crdx.org/oh/pkg/agent"
)

const ModeChange agent.Kind = "mode_change"

type modeRecord struct {
	Flags            string     `json:"flags"`
	Groups           string     `json:"groups,omitempty"`
	ToolGroups       ToolGroups `json:"tool_groups,omitempty"`
	FrozenToolGroups ToolGroups `json:"frozen_tool_groups,omitempty"`
	IsUnconfined     bool       `json:"unconfined,omitempty"`
}

func ModeEvent(grantedCaps Set) agent.Event {
	return agent.Event{Kind: ModeChange, State: encodeFlags(grantedCaps)}
}

func ModeToggleEvent(swappedCaps Set, grantedCaps Set) agent.Event {
	return agent.Event{
		Kind:  ModeChange,
		Name:  swappedCaps.Flag(),
		State: encodeFlags(grantedCaps),
	}
}

func (self *Mode) Event(swappedFlag string) agent.Event {
	current := self.state.GetCurrent()
	status := self.groupStatus(current, self.toolGroups)
	return agent.Event{
		Kind: ModeChange,
		Name: swappedFlag,
		State: encodeMode(modeRecord{
			Flags:            current.caps.Flags(),
			Groups:           status.GrantedFlags,
			ToolGroups:       self.activeToolGroups,
			FrozenToolGroups: self.toolGroups,
			IsUnconfined:     current.isUnconfined,
		}),
	}
}

func decodeMode(event agent.Event) (modeRecord, error) {
	var flags string
	if err := json.Unmarshal(event.State, &flags); err == nil {
		return modeRecord{Flags: flags}, nil
	}

	var record modeRecord
	if err := json.Unmarshal(event.State, &record); err != nil {
		return modeRecord{}, err
	}
	if _, _, err := ParseWithGroups(record.Flags+record.Groups, record.frozenToolGroups().CustomFlags()); err != nil {
		return modeRecord{}, err
	}
	return record, nil
}

func (self modeRecord) frozenToolGroups() ToolGroups {
	if len(self.FrozenToolGroups) > 0 {
		return self.FrozenToolGroups
	}
	return self.ToolGroups
}

func GrantedBy(event agent.Event) (Set, error) {
	record, err := decodeMode(event)
	if err != nil {
		return 0, err
	}

	return Parse(record.Flags)
}

func FlagsBy(event agent.Event) (string, error) {
	record, err := decodeMode(event)
	if err != nil {
		return "", err
	}
	grantedCaps, err := Parse(record.Flags)
	if err != nil {
		return "", err
	}
	return grantedCaps.Flags() + record.Groups, nil
}

func Notice(swappedCaps Set, grantedCaps Set) ([]string, bool) {
	notices := changeNotices(swappedCaps, grantedCaps, false)

	return notices, len(notices) > 0
}

func ModeNotice(event agent.Event) ([]string, bool) {
	record, err := decodeMode(event)
	if err != nil || event.Name == "" {
		return nil, false
	}

	grantedCaps, err := Parse(record.Flags)
	if err != nil {
		return nil, false
	}

	var notices []string
	if swappedCaps, isBuiltIn := Named(event.Name); isBuiltIn {
		notices = changeNotices(swappedCaps, grantedCaps, record.IsUnconfined)
	}
	if toolNames, isKnown := record.ToolGroups[event.Name]; isKnown {
		isGranted := strings.Contains(record.Groups, event.Name)
		if groupedCaps, isBuiltIn := Named(event.Name); isBuiltIn {
			isGranted = grantedCaps.Has(groupedCaps)
		}
		notices = append(notices, toolAccessNotices(toolNames, isGranted)...)
	}

	return notices, len(notices) > 0
}

func ModeWithout(event agent.Event, swappedCaps Set) agent.Event {
	return ModeWithoutFlag(event, swappedCaps.Flag())
}

func ModeWithoutFlag(event agent.Event, swappedFlag string) agent.Event {
	record, err := decodeMode(event)
	if err != nil {
		return event
	}

	if swappedCaps, isBuiltIn := Named(swappedFlag); isBuiltIn {
		grantedCaps, parseErr := Parse(record.Flags)
		if parseErr != nil {
			return event
		}
		record.Flags = (grantedCaps ^ swappedCaps).Flags()
	} else {
		record.Groups = toggleGroupFlag(record.Groups, swappedFlag)
	}
	event.State = encodeMode(record)

	return event
}

func toggleGroupFlag(flags string, swappedFlag string) string {
	set := make(map[string]struct{})
	for _, flag := range flags {
		set[string(flag)] = struct{}{}
	}
	if _, isPresent := set[swappedFlag]; isPresent {
		delete(set, swappedFlag)
	} else {
		set[swappedFlag] = struct{}{}
	}
	return strings.Join(slices.Sorted(maps.Keys(set)), "")
}

func LastRecordedMode(events []agent.Event) (Set, bool) {
	return access.LastRecorded(events, ModeChange, GrantedBy)
}

func LastRecordedToolGroups(events []agent.Event) (string, ToolGroups, bool) {
	record, found := access.LastRecorded(events, ModeChange, decodeMode)
	return record.Groups, record.frozenToolGroups().Clone(), found
}

func encodeFlags(grantedCaps Set) json.RawMessage {
	return encodeMode(modeRecord{Flags: grantedCaps.Flags()})
}

func encodeMode(record modeRecord) json.RawMessage {
	var value any = record
	if record.Groups == "" && len(record.ToolGroups) == 0 && len(record.FrozenToolGroups) == 0 && !record.IsUnconfined {
		value = record.Flags
	}
	encodedMode, err := json.Marshal(value)
	if err != nil {
		return nil
	}

	return encodedMode
}

type taskStopReason struct {
	Path string `json:"path,omitempty"`
}

const TaskStop agent.Kind = "task_stop"

func TaskStopEvent(taskID string, withdrawnCaps Set) agent.Event {
	return agent.Event{Kind: TaskStop, Name: taskID, State: encodeFlags(withdrawnCaps)}
}

func TaskStoppedForPathEvent(taskID string, path string) agent.Event {
	encodedReason, err := json.Marshal(taskStopReason{Path: path})
	if err != nil {
		return agent.Event{}
	}

	return agent.Event{Kind: TaskStop, Name: taskID, State: encodedReason}
}

func TaskStoppedForGroupEvent(taskID string, group string) agent.Event {
	encodedReason, err := json.Marshal(struct {
		Group string `json:"group"`
	}{Group: group})
	if err != nil {
		return agent.Event{}
	}
	return agent.Event{Kind: TaskStop, Name: taskID, State: encodedReason}
}

func TaskStoppedByUserEvent(taskID string) agent.Event {
	return agent.Event{Kind: TaskStop, Name: taskID}
}

func TaskStopNotice(event agent.Event) (string, bool) {
	if event.Name == "" {
		return "", false
	}

	if len(event.State) == 0 {
		return fmt.Sprintf("Task %s stopped from the keyboard.", markdown.CodeSpan(event.Name)), true
	}

	reason := TaskStopReason(event)
	if reason == "" {
		return "", false
	}

	return fmt.Sprintf("Task %s stopped because %s.", markdown.CodeSpan(event.Name), reason), true
}

func TaskStopReason(event agent.Event) string {
	if len(event.State) == 0 {
		return "the user stopped it"
	}

	var stopReason taskStopReason
	if err := json.Unmarshal(event.State, &stopReason); err == nil && stopReason.Path != "" {
		return "access to " + stopReason.Path + " was revoked"
	}
	var groupReason struct {
		Group string `json:"group"`
	}
	if err := json.Unmarshal(event.State, &groupReason); err == nil && groupReason.Group != "" {
		return "tool group " + groupReason.Group + " was withdrawn"
	}

	withdrawnCaps, err := GrantedBy(event)
	if err != nil {
		return ""
	}

	return withdrawal(withdrawnCaps)
}
