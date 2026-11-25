package model

import (
	"testing"
)

func TestASubagentModelIsAvailableBeneathItsOwnProviderUnlessOfferedToAll(t *testing.T) {
	for _, test := range []struct {
		name             string
		parent           Choice
		candidate        Choice
		isAvailableToAll bool
		want             bool
	}{
		{"same provider", Choice{Provider: AnthropicProvider}, Choice{Provider: AnthropicProvider, ID: "claude-haiku-5-5"}, false, true},
		{"another provider", Choice{Provider: CodexProvider}, Choice{Provider: AnthropicProvider, ID: "claude-haiku-5-5"}, false, false},
		{"another provider offered to all", Choice{Provider: CodexProvider}, Choice{Provider: AnthropicProvider, ID: "claude-haiku-5-5"}, true, true},
		{"a free model of another provider", Choice{Provider: CodexProvider}, Choice{Provider: OpencodeGoProvider, ID: "cat-jam-free"}, false, true},
		{"a local model beneath a local parent", Choice{Provider: OllamaProvider}, Choice{Provider: OllamaProvider, ID: "qwen3.8:27b"}, false, true},
		{"a local model offered to all beneath a hosted parent", Choice{Provider: CodexProvider}, Choice{Provider: OllamaProvider, ID: "qwen3.8:27b"}, true, false},
		{"a hosted model offered to all beneath a local parent", Choice{Provider: OllamaProvider}, Choice{Provider: AnthropicProvider, ID: "claude-haiku-5-5"}, true, false},
		{"a free model beneath a local parent", Choice{Provider: OllamaProvider}, Choice{Provider: OpencodeGoProvider, ID: "cat-jam-free"}, true, false},
	} {
		if got := CanDelegateTo(test.parent, test.candidate, test.isAvailableToAll); got != test.want {
			t.Errorf("%s: CanDelegateTo is %t, want %t", test.name, got, test.want)
		}
	}
}

func TestFreeSubagentModelsAreDiscoveredBySuffixAcrossHostedProviders(t *testing.T) {
	for _, provider := range []string{CodexProvider, AnthropicProvider, OpencodeGoProvider} {
		if !IsFreeSubagentModel(Choice{Provider: provider, ID: "future-free"}) {
			t.Errorf("%s/future-free is not free", provider)
		}
	}
	for _, id := range []string{"free", "free-model", "model-free-preview", "model-Free"} {
		if IsFreeSubagentModel(Choice{Provider: OpencodeGoProvider, ID: id}) {
			t.Errorf("%q incorrectly matched -free$", id)
		}
	}
	if IsFreeSubagentModel(Choice{Provider: OllamaProvider, ID: "local-free"}) {
		t.Error("a local model counted as free through its suffix")
	}
}
