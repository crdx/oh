package model

import (
	"slices"

	"crdx.org/oh/internal/state"
	"crdx.org/oh/pkg/agent"
)

const seenModelsFormat = 1

type seenModels struct {
	Version   int                      `json:"version"`
	Providers map[string][]agent.Model `json:"providers"`
}

func recordSeenModels(path string, listedByProvider map[string][]agent.Model) error {
	return state.Update(path, seenModelsFormat, func(record *seenModels) error {
		if record.Providers == nil {
			record.Providers = map[string][]agent.Model{}
		}

		for providerName, listedModels := range listedByProvider {
			record.Providers[providerName] = withSeenAgain(record.Providers[providerName], listedModels)
		}

		record.Version = seenModelsFormat

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

func seenChoices(path string) []Choice {
	var record seenModels
	if state.Read(path, seenModelsFormat, &record) != nil {
		return nil
	}

	var choices []Choice

	for _, providerName := range ProviderNames() {
		choices = append(choices, choicesFor(providerName, plainModels(record.Providers[providerName]))...)
	}

	return choices
}
