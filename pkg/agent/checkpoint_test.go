package agent_test

import (
	"encoding/json"
	"testing"

	"crdx.org/oh/pkg/agent"
)

func TestAConversationCheckpointRestoresAnIndependentProviderSnapshot(t *testing.T) {
	provider := &stateProvider{items: []json.RawMessage{json.RawMessage(`{"kept":true}`)}}
	assistant := agent.New("", provider, nil)
	restore, err := assistant.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	provider.items = append(provider.items, json.RawMessage(`{"discarded":true}`))
	if _, err := assistant.Dump(); err != nil {
		t.Fatal(err)
	}
	if err := restore(); err != nil {
		t.Fatal(err)
	}
	items, err := assistant.Dump()
	if err != nil || len(items) != 1 || string(items[0]) != `{"kept":true}` {
		t.Fatalf("checkpoint did not restore the provider: %s, %v", items, err)
	}
	provider.items = append(provider.items, json.RawMessage(`{"replacement":true}`))
	if _, err := assistant.Dump(); err != nil {
		t.Fatalf("restored state was not append-only: %v", err)
	}
}
