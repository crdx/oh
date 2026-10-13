package model

import (
	"slices"
	"testing"

	"crdx.org/oh/pkg/agent"
)

func TestTheTrialSubagentPoolsAreProviderSpecific(t *testing.T) {
	choices := []Choice{
		{Provider: CodexProvider, ID: "gpt-6.1-sol"},
		{Provider: CodexProvider, ID: "gpt-6-astra"},
		{Provider: CodexProvider, ID: "gpt-6-luna"},
		{Provider: CodexProvider, ID: "gpt-5.6-terra"},
		{Provider: AnthropicProvider, ID: "claude-opus-5-5"},
		{Provider: AnthropicProvider, ID: "claude-sonnet-5-5"},
		{Provider: AnthropicProvider, ID: "claude-haiku-5-5"},
		{Provider: AnthropicProvider, ID: "claude-fable-5-1"},
		{Provider: OpencodeGoProvider, ID: "glm-5.3"},
		{Provider: OpencodeGoProvider, ID: "deepseek-v4.1-flash"},
		{Provider: OpencodeGoProvider, ID: "muse-spark-1.3-contributor"},
		{Provider: OpencodeGoProvider, ID: "gpt-6-luna"},
		{Provider: OpencodeGoProvider, ID: "claude-haiku-5-5"},
		{Provider: OpencodeGoProvider, ID: "longcat-2.5-preview-free"},
		{Provider: OpencodeGoProvider, ID: "step-5-preview-free"},
		{Provider: OpencodeGoProvider, ID: "cat-jam-free"},
		{Provider: OllamaProvider, ID: "muse-glimmer-128k:latest"},
		{Provider: OllamaProvider, ID: "qwen3.8:27b-mtp-q8_0-256k"},
		{Provider: OllamaProvider, ID: "future-local-model"},
	}
	freeModels := []string{
		"opencode-go/longcat-2.5-preview-free",
		"opencode-go/step-5-preview-free",
		"opencode-go/cat-jam-free",
	}
	for _, test := range []struct {
		parent Choice
		want   []string
	}{
		{Choice{Provider: CodexProvider, ID: "gpt-6.1-sol"}, append([]string{"codex/gpt-6-luna"}, freeModels...)},
		{Choice{Provider: AnthropicProvider, ID: "claude-opus-5-5"}, append([]string{"anthropic/claude-haiku-5-5"}, freeModels...)},
		{Choice{Provider: OpencodeGoProvider, ID: "glm-5.3"}, append([]string{"opencode-go/deepseek-v4.1-flash"}, freeModels...)},
		{Choice{Provider: OllamaProvider, ID: "muse-glimmer-128k:latest"}, []string{"ollama/muse-glimmer-128k:latest", "ollama/qwen3.8:27b-mtp-q8_0-256k", "ollama/future-local-model"}},
	} {
		t.Run(test.parent.Provider, func(t *testing.T) {
			var got []string
			for _, candidate := range choices {
				if CanDelegateTo(test.parent, candidate) {
					got = append(got, candidate.Provider+"/"+candidate.ID)
				}
			}
			if !slices.Equal(got, test.want) {
				t.Errorf("%s can delegate to %v, want %v", test.parent.Provider, got, test.want)
			}
		})
	}
}

func TestTheTrialMappingDoesNotInferNewCheapModels(t *testing.T) {
	parent := Choice{Provider: CodexProvider, ID: "gpt-6.1-sol"}
	for _, candidate := range []Choice{
		parent,
		{Provider: CodexProvider, ID: "gpt-future-mini", Prices: &agent.TokenPrices{Input: 0.000001}},
		{Provider: OllamaProvider, ID: "gpt-6-luna"},
	} {
		if CanDelegateTo(parent, candidate) {
			t.Errorf("allowed an unmapped paid or local model: %+v", candidate)
		}
	}
}

func TestFreeSubagentModelsAreDiscoveredBySuffixAcrossProviders(t *testing.T) {
	for _, provider := range []string{CodexProvider, AnthropicProvider, OpencodeGoProvider} {
		candidate := Choice{Provider: provider, ID: "future-free"}
		for _, parentProvider := range []string{CodexProvider, AnthropicProvider, OpencodeGoProvider} {
			if !CanDelegateTo(Choice{Provider: parentProvider}, candidate) {
				t.Errorf("%s could not delegate to %s/%s", parentProvider, provider, candidate.ID)
			}
		}
		if CanDelegateTo(Choice{Provider: OllamaProvider}, candidate) {
			t.Errorf("a local parent could reach %s/%s", provider, candidate.ID)
		}
	}
	for _, id := range []string{"free", "free-model", "model-free-preview", "model-Free"} {
		if IsFreeSubagentModel(Choice{Provider: OpencodeGoProvider, ID: id}) {
			t.Errorf("%q incorrectly matched -free$", id)
		}
	}
	if CanDelegateTo(Choice{Provider: CodexProvider}, Choice{Provider: OllamaProvider, ID: "local-free"}) {
		t.Error("a hosted parent could reach a local model through the free suffix")
	}
}
