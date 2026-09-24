package model

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"crdx.org/oh/pkg/agent"
)

const (
	codexProvider      = CodexProvider
	opencodeGoProvider = OpencodeGoProvider
	anthropicProvider  = AnthropicProvider
	ollamaProvider     = OllamaProvider
)

func modelCachePath() string {
	return filepath.Join(os.Getenv("XDG_STATE_HOME"), "models.json")
}

func seenModelsPath() string {
	return filepath.Join(os.Getenv("XDG_STATE_HOME"), "seen_models.json")
}

func chosenAnthropicModel(model string) (Choice, error) {
	return Chosen(modelCachePath(), seenModelsPath(), anthropicProvider, model)
}

func parseModelSelection(writtenSelection string) (string, string, string, error) {
	selection, err := ParseSelection(modelCachePath(), writtenSelection, Defaults{})

	return selection.Provider, selection.Model, selection.Effort, err
}

func updateModelsWithoutProviderListings(output io.Writer, endpoint string, path string) error {
	return Update(output, endpoint, path, seenModelsPath(), func(context.Context, string) ([]agent.Model, error) {
		return nil, nil
	}, true)
}

func ensureModelsWithoutProviderListings(output io.Writer, endpoint string, path string) error {
	return Ensure(output, endpoint, path, seenModelsPath(), func(context.Context, string) ([]agent.Model, error) {
		return nil, nil
	})
}
