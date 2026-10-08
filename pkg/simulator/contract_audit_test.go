package simulator_test

import (
	"net/http/httptest"
	"testing"

	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/provider/ollama"
	"crdx.org/oh/pkg/simulator"
)

func TestNewSnapshotsTheScenario(t *testing.T) {
	scenario := &simulator.Scenario{Model: "fake", Turns: []simulator.Turn{{Say: "original"}}}
	endpoint := simulator.New(scenario)
	scenario.Turns[0].Say = "replaced"
	server := httptest.NewServer(endpoint)
	defer server.Close()
	provider, err := ollama.New(server.URL, "fake", "", 1024)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := agent.New("Answer briefly", provider, nil).Send(t.Context(), "Hello")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "original" {
		t.Fatalf("scenario changed after New: %q", answer)
	}
}

func TestRequestsReturnsIndependentHistory(t *testing.T) {
	endpoint := simulator.New(&simulator.Scenario{Model: "fake", Turns: []simulator.Turn{{Say: "done"}}})
	server := httptest.NewServer(endpoint)
	defer server.Close()
	provider, err := ollama.New(server.URL, "fake", "", 1024)
	if err != nil {
		t.Fatal(err)
	}
	_, err = agent.New("Answer briefly", provider, nil).Send(t.Context(), "Hello")
	if err != nil {
		t.Fatal(err)
	}
	first := endpoint.Requests()
	if len(first) != 1 || len(first[0].Input) == 0 {
		t.Fatalf("unexpected request: %+v", first)
	}
	original := first[0].Input[0].Content
	first[0].Input[0].Content = "modified by caller"
	if got := endpoint.Requests()[0].Input[0].Content; got != original {
		t.Fatalf("caller changed endpoint history: %q", got)
	}
}

func TestNewRefusesANilScenarioAtConstruction(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("nil scenario was accepted")
		}
	}()
	simulator.New(nil)
}
