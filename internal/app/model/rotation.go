package model

import (
	"errors"
	"fmt"

	"crdx.org/io/internal/roundrobin"
	"crdx.org/io/internal/state"
)

type Selection struct {
	Provider string
	Model    string
	Effort   string
	IsFast   bool
}

func (self Selection) String() string {
	writtenSelection := self.Provider + "/" + self.Model + "@" + self.Effort
	if self.IsFast {
		writtenSelection += "+fast"
	}

	return writtenSelection
}

const roundRobinStateVersion = 1

type roundRobinState struct {
	Version   int       `json:"version"`
	Last      Selection `json:"last"`
	LastIndex int       `json:"last_index"`
}

var ErrNoSelection = errors.New(
	"no model selected: use -m provider/model@effort[+fast] or configure model.round_robin",
)

func ReserveRoundRobin(path string, selections []Selection) (Selection, error) {
	return ReserveAvailableRoundRobin(path, selections, func(Selection) bool { return true })
}

func ReserveAvailableRoundRobin(
	path string, selections []Selection, isAvailable func(Selection) bool,
) (Selection, error) {
	if len(selections) == 0 {
		return Selection{}, ErrNoSelection
	}
	if len(selections) == 1 {
		return selections[0], nil
	}

	var selectedModel Selection
	err := state.Update(path, roundRobinStateVersion, func(storedState *roundRobinState) error {
		if storedState.Version != 0 && storedState.Version != roundRobinStateVersion {
			return fmt.Errorf("model round-robin state has version %d, expected %d", storedState.Version, roundRobinStateVersion)
		}

		selectedIndex := roundrobin.NextIndexWhere(
			selections, storedState.Last, storedState.LastIndex, isAvailable,
		)
		if selectedIndex < 0 {
			selectedIndex = roundrobin.NextIndex(selections, storedState.Last, storedState.LastIndex)
		}
		selectedModel = selections[selectedIndex]
		*storedState = roundRobinState{
			Version:   roundRobinStateVersion,
			Last:      selectedModel,
			LastIndex: selectedIndex,
		}

		return nil
	})

	return selectedModel, err
}

func ParseRoundRobin(path string, writtenSelections []string, defaults Defaults) ([]Selection, error) {
	selections := make([]Selection, 0, len(writtenSelections))

	for _, writtenSelection := range writtenSelections {
		selection, err := ParseSelection(path, writtenSelection, defaults)
		if err != nil {
			return nil, fmt.Errorf("model.round_robin: %q: %w", writtenSelection, err)
		}

		selections = append(selections, selection)
	}

	return selections, nil
}
