package opencodego

import (
	"testing"

	"crdx.org/oh/pkg/agent"
)

func TestOnlyTheThreeWiresTheEndpointServesAreSpoken(t *testing.T) {
	for wire, want := range map[agent.Wire]bool{
		agent.CompletionsWire: true,
		agent.ResponsesWire:   true,
		agent.MessagesWire:    true,
		"":                    false,
		"generative-language": false,
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
		{"https://opencode.ai/zen/go/v1/chat/completions", responsesSuffix, "https://opencode.ai/zen/go/v1/responses"},
		{"https://opencode.ai/zen/go/v1/chat/completions", messagesSuffix, "https://opencode.ai/zen/go/v1/messages"},
		{"http://127.0.0.1:8000", responsesSuffix, "http://127.0.0.1:8000/responses"},
	} {
		if got := besideCompletions(test.address, test.suffix); got != test.want {
			t.Errorf("%s: got %s, want %s", test.address, got, test.want)
		}
	}
}
