package sim

import (
	"encoding/json"
	"net/http"

	"crdx.org/io/pkg/agent"
)

const (
	simulatedContext = 200_000
	simulatedOutput  = 64_000
)

var simulatedEfforts = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

var simulatedPrices = agent.TokenPrices{
	Input:      3,
	Output:     15,
	CacheRead:  0.3,
	CacheWrite: 3.75,
}

func OfferedModel(name string) agent.Model {
	prices := simulatedPrices

	return agent.Model{
		ID:                  name,
		Name:                name,
		EffortLevels:        simulatedEfforts,
		ContextWindowTokens: simulatedContext,
		MaxOutputTokens:     simulatedOutput,
		Prices:              &prices,
	}
}

type listing struct {
	Data []listedModel `json:"data"`
}

type listedModel struct {
	Type           string `json:"type"`
	ID             string `json:"id"`
	DisplayName    string `json:"display_name"`
	MaxInputTokens int    `json:"max_input_tokens"`
	MaxTokens      int    `json:"max_tokens"`

	Capabilities struct {
		Effort effortCapability `json:"effort"`
	} `json:"capabilities"`
}

type effortCapability struct {
	IsSupported bool           `json:"supported"`
	Low         levelSupported `json:"low"`
	Medium      levelSupported `json:"medium"`
	High        levelSupported `json:"high"`
	XHigh       levelSupported `json:"xhigh"`
	Max         levelSupported `json:"max"`
}

type levelSupported struct {
	IsSupported bool `json:"supported"`
}

func (self *Endpoint) serveListing(writer http.ResponseWriter) {
	offeredModel := listedModel{
		Type:           "model",
		ID:             self.scenario.Model,
		DisplayName:    self.scenario.Model,
		MaxInputTokens: simulatedContext,
		MaxTokens:      simulatedOutput,
	}

	offeredModel.Capabilities.Effort = effortCapability{
		IsSupported: true,
		Low:         levelSupported{IsSupported: true},
		Medium:      levelSupported{IsSupported: true},
		High:        levelSupported{IsSupported: true},
		XHigh:       levelSupported{IsSupported: true},
		Max:         levelSupported{IsSupported: true},
	}

	respond(writer, listing{Data: []listedModel{offeredModel}})
}

type codexListing struct {
	Models []codexListedModel `json:"models"`
}

type codexListedModel struct {
	Slug          string                `json:"slug"`
	DisplayName   string                `json:"display_name"`
	Visibility    string                `json:"visibility"`
	ContextWindow int                   `json:"context_window"`
	Levels        []codexReasoningLevel `json:"supported_reasoning_levels"`
}

type codexReasoningLevel struct {
	Effort      string `json:"effort"`
	Description string `json:"description"`
}

func (self *Endpoint) serveCodexListing(writer http.ResponseWriter) {
	levels := make([]codexReasoningLevel, 0, len(simulatedEfforts))

	for _, effort := range simulatedEfforts {
		levels = append(levels, codexReasoningLevel{
			Effort:      effort,
			Description: effort + " reasoning",
		})
	}

	respond(writer, codexListing{
		Models: []codexListedModel{
			{
				Slug:          self.scenario.Model,
				DisplayName:   self.scenario.Model,
				Visibility:    "list",
				ContextWindow: simulatedContext,
				Levels:        levels,
			},
			{
				Slug:          self.scenario.Model + "-unlisted",
				DisplayName:   self.scenario.Model + " (unlisted)",
				Visibility:    "none",
				ContextWindow: simulatedContext,
				Levels:        levels,
			},
		},
	})
}

type ollamaListing struct {
	Models []ollamaListedModel `json:"models"`
}

type ollamaListedModel struct {
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
	Details      struct {
		ContextWindowTokens int `json:"context_length"`
	} `json:"details"`
}

func (self *Endpoint) serveOllamaListing(writer http.ResponseWriter) {
	offeredModel := ollamaListedModel{
		Name:         self.scenario.Model,
		Capabilities: []string{"completion", "thinking", "tools"},
	}
	offeredModel.Details.ContextWindowTokens = simulatedContext

	respond(writer, ollamaListing{Models: []ollamaListedModel{offeredModel}})
}

func respond(writer http.ResponseWriter, document any) {
	writer.Header().Set("Content-Type", "application/json")

	_ = json.NewEncoder(writer).Encode(document) //nolint:errchkjson // the documents encode safely
}
