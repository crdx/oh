package model

import (
	"encoding/json"
	"slices"

	"crdx.org/io/pkg/agent"
)

const FastModeStateKey = "fast-mode"

type fastModeState struct {
	IsFast bool `json:"fast"`
}

func FastModeEvent(isFast bool) agent.Event {
	state := json.RawMessage(`{"fast":false}`)
	if isFast {
		state = json.RawMessage(`{"fast":true}`)
	}

	return agent.Event{Kind: agent.StateChangeEvent, Name: FastModeStateKey, State: state}
}

func FastModeFromEvent(event agent.Event) (bool, bool) {
	if event.Kind != agent.StateChangeEvent || event.Name != FastModeStateKey {
		return false, false
	}

	var state fastModeState
	if err := json.Unmarshal(event.State, &state); err != nil {
		return false, false
	}
	return state.IsFast, true
}

func LastRecordedFastMode(events []agent.Event) (bool, bool) {
	for _, event := range slices.Backward(events) {
		if isFast, isFound := FastModeFromEvent(event); isFound {
			return isFast, true
		}
	}

	return false, false
}
