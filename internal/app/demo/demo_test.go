package demo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/internal/app/backend"
	"crdx.org/io/internal/app/location"
	"crdx.org/io/internal/app/model"
	"crdx.org/io/internal/sim"
	"crdx.org/io/pkg/agent"
)

func TestMain(runner *testing.M) {
	if err := os.Unsetenv(location.StateDirVariable); err != nil {
		panic(err)
	}

	os.Exit(runner.Run())
}

func TestTheSimulationListensOnLoopbackAndOffersItsOwnModel(t *testing.T) {
	t.Setenv(location.StateDirVariable, t.TempDir())

	session, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)

	address, err := url.Parse(session.EndpointURL)
	if err != nil {
		t.Fatal(err)
	}
	if host, _, _ := strings.Cut(address.Host, ":"); host != "127.0.0.1" {
		t.Errorf("the simulation listens at %q", address.Host)
	}

	listing, err := http.Get(address.Scheme + "://" + address.Host + "/v1/models") //nolint:noctx // the endpoint is ours
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listing.Body.Close() }()

	var offered struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(listing.Body).Decode(&offered); err != nil {
		t.Fatal(err)
	}
	if len(offered.Data) != 1 {
		t.Fatalf("the simulation offered %d models", len(offered.Data))
	}

	want := model.Selection{
		Provider: model.AnthropicProvider,
		Model:    offered.Data[0].ID,
		Effort:   demonstratedEffort,
	}.String()
	if session.Selection != want {
		t.Errorf("the selection is %q, want %q", session.Selection, want)
	}
}

func TestTheSimulationKnowsItsModelWithoutRefreshingTheList(t *testing.T) {
	t.Setenv(location.StateDirVariable, t.TempDir())

	session, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)

	cachePath := location.GetModelCachePath(true)

	var notices strings.Builder

	refresh := func(context.Context, string) ([]agent.Model, error) {
		t.Error("the simulation went looking for a model list")
		return nil, errors.New("the simulation has no list to fetch")
	}

	if err := model.Ensure(&notices, session.EndpointURL, cachePath, refresh); err != nil {
		t.Fatal(err)
	}
	if notices.String() != "" {
		t.Errorf("starting the simulation said %q", notices.String())
	}

	selection, err := model.ParseSelection(cachePath, session.Selection, model.Defaults{})
	if err != nil {
		t.Fatal(err)
	}
	if selection.String() != session.Selection {
		t.Errorf("the simulation resolved %q, want %q", selection.String(), session.Selection)
	}
}

func TestTheSimulationKeepsItsStateSomewhereItCanThrowAway(t *testing.T) {
	root := t.TempDir()
	t.Setenv(location.StateDirVariable, root)

	session, err := Start()
	if err != nil {
		t.Fatal(err)
	}

	stateDir := os.Getenv(location.StateDirVariable)
	if filepath.Dir(stateDir) != root {
		t.Fatalf("the simulation kept its state in %q, outside %q", stateDir, root)
	}

	session.Close()

	if _, err := os.Stat(stateDir); !os.IsNotExist(err) { //nolint:gosec // the path is the one the session made
		t.Errorf("the state at %q outlived the session: %v", stateDir, err)
	}
	if got := os.Getenv(location.StateDirVariable); got != root {
		t.Errorf("the state directory was left as %q, want %q", got, root)
	}
}

func TestTheSimulationAnswersTheProviderItNames(t *testing.T) {
	t.Setenv(location.StateDirVariable, t.TempDir())

	session, err := start(&sim.Scenario{Model: simulatedModel})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)

	connection, err := backend.Connect(
		model.Choice{Provider: model.AnthropicProvider, ID: simulatedModel, MaxOutputTokens: 4096},
		model.Selection{Provider: model.AnthropicProvider, Model: simulatedModel, Effort: demonstratedEffort},
		backend.EndpointSettings{OverrideURL: session.EndpointURL},
	)
	if err != nil {
		t.Fatal(err)
	}

	connection.Configure("", nil)
	connection.AddUserMessage("hello")

	var said strings.Builder
	reply, err := connection.Send(t.Context(), func(output agent.Output) bool {
		if output.Kind == agent.ModelMessageEvent {
			said.WriteString(output.Text)
		}

		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if said.Len() == 0 {
		t.Errorf("the simulation said nothing, and replied %+v", reply)
	}
}

func TestTheNextSimulationSweepsUpAfterOneThatDied(t *testing.T) {
	root := t.TempDir()
	t.Setenv(location.StateDirVariable, root)

	leftover := filepath.Join(root, stateDirPrefix+"leftover")
	if err := os.MkdirAll(leftover, 0o700); err != nil {
		t.Fatal(err)
	}

	session, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)

	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Errorf("the state left at %q outlived the simulation that made it: %v", leftover, err)
	}
}

func TestOneSimulationLeavesAnotherAlone(t *testing.T) {
	root := t.TempDir()
	t.Setenv(location.StateDirVariable, root)

	first, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(first.Close)

	firstStateDir := os.Getenv(location.StateDirVariable)

	second, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)

	if _, err := os.Stat(firstStateDir); err != nil { //nolint:gosec // the path is the one the session made
		t.Errorf("a second simulation swept away the state of the first: %v", err)
	}
}
