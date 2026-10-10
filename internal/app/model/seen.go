package model

import (
	"fmt"
	"slices"

	"crdx.org/oh/internal/format"
	"crdx.org/oh/internal/state"
	"crdx.org/oh/pkg/agent"
)

const SeenModelsFormat = 2

type seenModels struct {
	Version   int                      `json:"version"`
	Providers map[string][]agent.Model `json:"providers"`
}

func recordSeenModels(path string, listedByProvider map[string][]agent.Model) error {
	return state.Update(path, SeenModelsFormat, func(record *seenModels) error {
		if err := requireCurrentSeenModels(*record); err != nil {
			return err
		}
		if record.Providers == nil {
			record.Providers = map[string][]agent.Model{}
		}

		for providerName, listedModels := range listedByProvider {
			record.Providers[providerName] = withSeenAgain(record.Providers[providerName], listedModels)
		}

		record.Version = SeenModelsFormat

		return nil
	})
}

func withSeenAgain(seenBefore []agent.Model, listedModels []agent.Model) []agent.Model {
	for _, model := range listedModels {
		if model.ID == "" {
			continue
		}

		index := slices.IndexFunc(seenBefore, func(seenModel agent.Model) bool {
			return seenModel.ID == model.ID
		})
		if index < 0 {
			seenBefore = append(seenBefore, model)
		} else {
			seenBefore[index] = filledFrom(model, seenBefore[index])
		}
	}

	return seenBefore
}

func seenChoices(path string) ([]Choice, error) {
	var record seenModels
	if err := state.Read(path, SeenModelsFormat, &record); err != nil {
		return nil, err
	}
	if err := requireCurrentSeenModels(record); err != nil {
		return nil, err
	}

	var choices []Choice

	for _, providerName := range ProviderNames() {
		choices = append(choices, choicesFor(providerName, plainModels(record.Providers[providerName]))...)
	}

	return choices, nil
}

func requireCurrentSeenModels(record seenModels) error {
	if record.Version == 0 {
		return nil
	}
	if err := format.Require(record.Version, SeenModelsFormat); err != nil && format.IsOlder(err) {
		return fmt.Errorf("seen models %w: run oh --ctl migrate", err)
	}

	return nil
}
