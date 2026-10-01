package opencodego

import "testing"

func TestEachModelIsSpokenToOverTheWireItsEndpointServes(t *testing.T) {
	for modelID, want := range map[string]wireFormat{
		"minimax-m3":                 messagesWire,
		"minimax-m2.7":               messagesWire,
		"qwen3.8-max":                messagesWire,
		"qwen3.7-plus":               messagesWire,
		"grok-4.7":                   responsesWire,
		"muse-spark-1.3-contributor": responsesWire,
		"gpt-6-luna":                 responsesWire,
		"ox-alpha-free":              completionsWire,
		"mimo-v2-omni":               completionsWire,
		"deepseek-v4-flash":          completionsWire,
		"glm-5.3":                    completionsWire,
		"kimi-k3":                    completionsWire,
	} {
		if got := wireFor(modelID); got != want {
			t.Errorf("%s: got wire %d, want %d", modelID, got, want)
		}
	}
}

func TestEveryOtherAddressStandsBesideTheCompletionsAddress(t *testing.T) {
	for _, test := range []struct {
		address string
		suffix  string
		want    string
	}{
		{"https://opencode.ai/zen/go/v1/chat/completions", responsesSuffix, "https://opencode.ai/zen/go/v1/responses"},
		{"https://opencode.ai/zen/go/v1/chat/completions", messagesSuffix, "https://opencode.ai/zen/go/v1/messages"},
		{"http://127.0.0.1:8000", responsesSuffix, "http://127.0.0.1:8000/responses"},
	} {
		if got := besideCompletions(test.address, test.suffix); got != test.want {
			t.Errorf("%s: got %s, want %s", test.address, got, test.want)
		}
	}
}
