package sim

import (
	"net/http"
	"strings"
)

type registryEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	Provider *registryPackage `json:"provider,omitempty"`

	ReasoningOptions []registryReasoning `json:"reasoning_options"`

	Limit struct {
		Context int `json:"context"`
		Output  int `json:"output"`
	} `json:"limit"`

	Cost struct {
		Input      float64 `json:"input"`
		Output     float64 `json:"output"`
		CacheRead  float64 `json:"cache_read"`
		CacheWrite float64 `json:"cache_write"`
	} `json:"cost"`
}

type registryReasoning struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

type registryPackage struct {
	Name string `json:"npm"`
}

type registryProvider struct {
	Package string                   `json:"npm"`
	Models  map[string]registryEntry `json:"models"`
}

const registryQuery = "providers"

const compatiblePackage = "@ai-sdk/openai-compatible"

var providerPackages = map[string]string{
	"openai":    "@ai-sdk/openai",
	"anthropic": "@ai-sdk/anthropic",
}

var wirePackages = map[string]string{
	Completions: compatiblePackage,
	Responses:   "@ai-sdk/openai",
	Messages:    "@ai-sdk/anthropic",
}

func packageOf(providerName string) string {
	if name, isFound := providerPackages[providerName]; isFound {
		return name
	}

	return compatiblePackage
}

func (self *Endpoint) serveRegistry(writer http.ResponseWriter, request *http.Request) {
	entry := registryEntry{
		ID:   self.scenario.Model,
		Name: self.scenario.Model,
		ReasoningOptions: []registryReasoning{
			{Type: "effort", Values: simulatedEfforts},
		},
	}

	entry.Limit.Context = simulatedContext
	entry.Limit.Output = simulatedOutput
	entry.Cost.Input = simulatedPrices.Input
	entry.Cost.Output = simulatedPrices.Output
	entry.Cost.CacheRead = simulatedPrices.CacheRead
	entry.Cost.CacheWrite = simulatedPrices.CacheWrite

	if name, isFound := wirePackages[self.scenario.Wire]; isFound {
		entry.Provider = &registryPackage{Name: name}
	}

	registry := map[string]registryProvider{}

	for name := range strings.SplitSeq(request.URL.Query().Get(registryQuery), ",") {
		if name != "" {
			registry[name] = registryProvider{
				Package: packageOf(name),
				Models:  map[string]registryEntry{self.scenario.Model: entry},
			}
		}
	}

	respond(writer, registry)
}
