package opencodego

import "testing"

func TestOnlyChatCompletionsModelsAreSupported(t *testing.T) {
	for _, modelID := range []string{
		"grok-4.6",
		"minimax-m3",
		"qwen3.8-max",
		"muse-spark-1.2-contributor",
		"gpt-5.6-luna",
	} {
		if SupportsCompletions(modelID) {
			t.Errorf("expected %s not to support Chat Completions", modelID)
		}
	}

	for _, modelID := range []string{
		"ox-alpha-free",
		"mimo-v2-omni",
		"deepseek-v4-flash",
		"glm-5.3",
		"kimi-k3",
	} {
		if !SupportsCompletions(modelID) {
			t.Errorf("expected %s to support Chat Completions", modelID)
		}
	}
}
