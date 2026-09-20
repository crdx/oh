package model

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"crdx.org/oh/pkg/agent"
)

const (
	endpointVariable   = "OH_ENDPOINT_URL"
	codexProvider      = CodexProvider
	opencodeGoProvider = OpencodeGoProvider
	anthropicProvider  = AnthropicProvider
	ollamaProvider     = OllamaProvider
)

func modelCachePath() string {
	name := "models.json"
	if os.Getenv(endpointVariable) != "" {
		name = "models.sim.json"
	}
	return filepath.Join(os.Getenv("XDG_STATE_HOME"), name)
}

func chosenModel(providerName string, model string) (Choice, error) {
	return Chosen(modelCachePath(), providerName, model)
}

func parseModelSelection(writtenSelection string) (string, string, string, error) {
	selection, err := ParseSelection(modelCachePath(), writtenSelection, Defaults{})

	return selection.Provider, selection.Model, selection.Effort, err
}

func updateModelsWithoutProviderListings(output io.Writer, endpoint string, path string) error {
	return Update(output, endpoint, path, func(context.Context, string) ([]agent.Model, error) {
		return nil, nil
	}, true)
}

func ensureModelsWithoutProviderListings(output io.Writer, endpoint string, path string) error {
	return Ensure(output, endpoint, path, func(context.Context, string) ([]agent.Model, error) {
		return nil, nil
	})
}
