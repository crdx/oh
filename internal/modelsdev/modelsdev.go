package modelsdev

import (
	"context"
	"slices"
	"strings"
	"time"

	"crdx.org/io/internal/req"
	"crdx.org/io/pkg/agent"
)

const Endpoint = "https://models.dev/api.json"

const fetchTimeout = 60 * time.Second

type Registry map[string]map[string]agent.Model

type entry struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	ReasoningOptions []struct {
		Type   string   `json:"type"`
		Values []string `json:"values"`
	} `json:"reasoning_options"`

	Limit struct {
		ContextWindowTokens int `json:"context"`
		MaxInputTokens      int `json:"input"`
		MaxOutputTokens     int `json:"output"`
	} `json:"limit"`

	Cost struct {
		Input      float64 `json:"input"`
		Output     float64 `json:"output"`
		CacheRead  float64 `json:"cache_read"`
		CacheWrite float64 `json:"cache_write"`
	} `json:"cost"`
}

func (self entry) getContextWindowTokens() int {
	if self.Limit.MaxInputTokens > 0 {
		return self.Limit.MaxInputTokens
	}

	return self.Limit.ContextWindowTokens
}

func (self entry) getEffortLevels() []string {
	for _, option := range self.ReasoningOptions {
		if option.Type == "effort" && len(option.Values) > 0 {
			return slices.Clone(option.Values)
		}
	}

	return nil
}

func (self entry) getPrices() *agent.TokenPrices {
	prices := agent.TokenPrices{
		Input:      self.Cost.Input,
		Output:     self.Cost.Output,
		CacheRead:  self.Cost.CacheRead,
		CacheWrite: self.Cost.CacheWrite,
	}

	if !prices.IsKnown() {
		return nil
	}

	return &prices
}

func (self entry) model(name string) agent.Model {
	id := self.ID
	if id == "" {
		id = name
	}

	return agent.Model{
		ID:                  id,
		Name:                self.Name,
		EffortLevels:        self.getEffortLevels(),
		ContextWindowTokens: self.getContextWindowTokens(),
		MaxOutputTokens:     self.Limit.MaxOutputTokens,
		Prices:              self.getPrices(),
	}
}

func hasDatedVersionSuffix(id string) bool {
	separator := strings.LastIndexByte(id, '-')
	if separator == -1 {
		return false
	}

	_, err := time.Parse("20060102", id[separator+1:])

	return err == nil
}

func Fetch(ctx context.Context, address string, observer req.Observer) (Registry, error) {
	requests := req.New(fetchTimeout)
	if observer != nil {
		requests.Observe(observer)
	}

	if address == "" {
		address = Endpoint
	}

	var payload map[string]struct {
		Models map[string]entry `json:"models"`
	}

	if err := requests.Get(ctx, address, nil, &payload); err != nil {
		return nil, err
	}

	registry := make(Registry, len(payload))

	for providerName, describedProvider := range payload {
		models := make(map[string]agent.Model, len(describedProvider.Models))
		for modelName, describedModel := range describedProvider.Models {
			model := describedModel.model(modelName)
			if hasDatedVersionSuffix(model.ID) {
				continue
			}

			models[modelName] = model
		}

		registry[providerName] = models
	}

	return registry, nil
}

func (self Registry) Provider(name string) map[string]agent.Model {
	return self[name]
}
