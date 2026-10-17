package opencodego

import (
	"testing"

	"crdx.org/oh/pkg/agent"
)

func TestOnlyTheFourWiresTheEndpointServesAreSpoken(t *testing.T) {
	for wire, want := range map[agent.Wire]bool{
		agent.CompletionsWire:     true,
		agent.ResponsesWire:       true,
		agent.MessagesWire:        true,
		agent.GenerateContentWire: true,
		"":                        false,
		"converse":                false,
	} {
		if got := Speaks(wire); got != want {
			t.Errorf("%q: speaks is %t, want %t", wire, got, want)
		}
	}
}

func TestEveryOtherAddressStandsBesideTheCompletionsAddress(t *testing.T) {
	for _, test := range []struct {
		address string
		suffix  string
		want    string
	}{
		{"https://opencode.ai/inference/go/v1/chat/completions", responsesSuffix, "https://opencode.ai/inference/go/v1/responses"},
		{"https://opencode.ai/inference/go/v1/chat/completions", messagesSuffix, "https://opencode.ai/inference/go/v1/messages"},
		{"http://127.0.0.1:8000", responsesSuffix, "http://127.0.0.1:8000/responses"},
	} {
		if got := besideCompletions(test.address, test.suffix); got != test.want {
			t.Errorf("%s: got %s, want %s", test.address, got, test.want)
		}
	}
}

func TestGoogleIsSpokenToBesideTheVersionedCompletionsAddress(t *testing.T) {
	for address, want := range map[string]string{
		"https://opencode.ai/inference/go/v1/chat/completions": "https://opencode.ai/inference/go/google/v1beta",
		"http://127.0.0.1:8000/v1/chat/completions":            "http://127.0.0.1:8000/google/v1beta",
		"http://127.0.0.1:8000":                                "http://127.0.0.1:8000/google/v1beta",
	} {
		if got := googleAddress(address); got != want {
			t.Errorf("%s: got %s, want %s", address, got, want)
		}
	}
}
