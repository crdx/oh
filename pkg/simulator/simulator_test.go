package simulator_test

import (
	"net/http/httptest"
	"testing"

	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/provider/ollama"
	"crdx.org/oh/pkg/simulator"
)

func TestPublicSimulatorPlaysAnAgentTurn(t *testing.T) {
	endpoint := simulator.New(&simulator.Scenario{
		Model: "fake",
		Turns: []simulator.Turn{{Say: "Hello."}},
	})
	server := httptest.NewServer(endpoint)
	defer server.Close()

	provider, err := ollama.New(server.URL, "fake", "", 1024)
	if err != nil {
		t.Fatal(err)
	}
	assistant := agent.New("Answer briefly.", provider, nil)

	answer, err := assistant.Send(t.Context(), "Hello")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "Hello." {
		t.Errorf("got %q, want Hello.", answer)
	}
	if requests := endpoint.Requests(); len(requests) != 1 || requests[0].Model != "fake" {
		t.Errorf("unexpected requests: %+v", requests)
	}
}
