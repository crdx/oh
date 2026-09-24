package backend

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/sim"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/provider/codex"
	"crdx.org/oh/pkg/provider/ollama"

	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/model"
)

const (
	codexProvider      = model.CodexProvider
	opencodeGoProvider = model.OpencodeGoProvider
	anthropicProvider  = model.AnthropicProvider
	ollamaProvider     = model.OllamaProvider
)

func TestMain(testingMain *testing.M) {
	unsetInheritedStateDirectory()
	os.Exit(testingMain.Run())
}

func unsetInheritedStateDirectory() {
	if err := os.Unsetenv(location.StateDirVariable); err != nil {
		panic(err)
	}
}

func testSelection() model.Selection {
	return model.Selection{Effort: "high"}
}

func TestUpdatingAgainstAStandInEndpointDescribesEveryProvider(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	endpoint := sim.New(&sim.Scenario{Model: "fake", Turns: []sim.Turn{{Say: "Hello."}}})
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)

	address := endpoint.Addresses(server.URL)[sim.Messages]
	if address == "" {
		t.Fatal("expected the Messages API to be served")
	}

	endpoints := EndpointSettings{OverrideURL: address}
	listProviderModels := func(ctx context.Context, providerName string) ([]agent.Model, error) {
		return ListModels(ctx, providerName, endpoints)
	}

	var output bytes.Buffer
	if err := model.Update(
		&output,
		address,
		location.GetModelCachePath(os.Getenv(EndpointVariable) != ""),
		location.GetSeenModelsPath(os.Getenv(EndpointVariable) != ""),
		listProviderModels,
		false,
	); err != nil {
		t.Fatalf("unexpected error: %v, output %q", err, output.String())
	}

	choices := model.Choices(location.GetModelCachePath(os.Getenv(EndpointVariable) != ""))
	for _, providerName := range model.ProviderNames() {
		var matches []model.Choice
		for _, choice := range choices {
			if choice.Provider == providerName {
				matches = append(matches, choice)
			}
		}

		if len(matches) != 1 || matches[0].ID != "fake" {
			t.Errorf("expected %s to offer the scenario's model, got %v", providerName, matches)

			continue
		}

		if matches[0].MaxOutputTokens <= 0 {
			t.Errorf("expected %s to know what the model may write, got %v", providerName, matches[0])
		}
	}
}

func TestEveryProviderListsModelsWithoutAConversationModel(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	endpoint := sim.New(&sim.Scenario{Model: "fake"})
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)

	addresses := endpoint.Addresses(server.URL)
	tests := []struct {
		providerName string
		format       string
	}{
		{codexProvider, sim.Responses},
		{opencodeGoProvider, sim.Completions},
		{anthropicProvider, sim.Messages},
		{ollamaProvider, sim.Completions},
	}

	for _, test := range tests {
		t.Run(test.providerName, func(t *testing.T) {
			models, err := ListModels(t.Context(), test.providerName, EndpointSettings{OverrideURL: addresses[test.format]})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(models) != 1 || models[0].ID != "fake" {
				t.Errorf("got %v", models)
			}
		})
	}
}

func TestSubscriptionCodexIsAskedForItsOwnCatalogue(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	models, err := ListModels(t.Context(), codexProvider, EndpointSettings{})
	if errors.Is(err, agent.ErrNoListing) {
		t.Fatalf("expected Codex to be asked rather than left to the registry, got %v", err)
	}
	if err == nil {
		t.Fatalf("expected the missing credentials to be reported, got %v", models)
	}
	if len(models) != 0 {
		t.Errorf("got %v", models)
	}
}

func testModelSelections() []model.Selection {
	return []model.Selection{
		{Provider: opencodeGoProvider, Model: "deepseek-v4-pro", Effort: "high"},
		{Provider: anthropicProvider, Model: "claude-opus-5", Effort: "max"},
	}
}

func TestResolveFallsBackToTheConfig(t *testing.T) {
	configured := []model.Selection{{
		Provider: opencodeGoProvider,
		Model:    "configured-model",
		Effort:   "medium",
	}}
	selection, err := Resolve(
		model.Selection{}, model.Selection{}, configured, filepath.Join(t.TempDir(), "round-robin.json"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if selection != configured[0] {
		t.Errorf("got %s", selection)
	}
}

func TestResolveRefusesToSwapTheModelOfAResumedConversation(t *testing.T) {
	resumed := model.Selection{
		Provider: opencodeGoProvider,
		Model:    "saved-model",
		Effort:   "low",
	}
	requested := model.Selection{
		Provider: opencodeGoProvider,
		Model:    "requested-model",
		Effort:   "high",
	}
	selection, err := Resolve(
		requested,
		resumed,
		[]model.Selection{{Provider: codexProvider, Model: "configured-model", Effort: "medium"}},
		"",
	)
	if err == nil || !strings.Contains(err.Error(), "a model is chosen when a session is created") {
		t.Fatalf("got selection %s and error %v", selection, err)
	}
}

func TestResolveRefusesToSwapTheEffortOfAResumedConversation(t *testing.T) {
	resumed := model.Selection{Provider: opencodeGoProvider, Model: "saved-model", Effort: "low"}
	requested := model.Selection{Provider: opencodeGoProvider, Model: "saved-model", Effort: "high"}

	selection, err := Resolve(requested, resumed, nil, "")
	if err == nil || !strings.Contains(err.Error(), "a model is chosen when a session is created") {
		t.Fatalf("got selection %s and error %v", selection, err)
	}
}

func TestResolveRefusesToResumeUnderAnotherProvider(t *testing.T) {
	resumed := model.Selection{Provider: opencodeGoProvider, Model: "saved-model"}

	selection, err := Resolve(
		model.Selection{Provider: codexProvider, Model: "requested-model", Effort: "high"},
		resumed,
		nil,
		"",
	)
	if err == nil || !strings.Contains(err.Error(), "cannot resume a conversation held with opencode-go/saved-model@") {
		t.Fatalf("got selection %s and error %v", selection, err)
	}
}

func TestResolveAcceptsTheModelAResumedConversationWasLeftOn(t *testing.T) {
	resumed := model.Selection{Provider: opencodeGoProvider, Model: "saved-model", Effort: "low"}

	selection, err := Resolve(resumed, resumed, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if selection != resumed {
		t.Errorf("got %s", selection)
	}
}

func TestResolveResumesUnderTheRecordedProvider(t *testing.T) {
	resumed := model.Selection{Provider: opencodeGoProvider, Model: "saved-model", Effort: "low"}

	selection, err := Resolve(
		model.Selection{},
		resumed,
		[]model.Selection{{Provider: codexProvider, Model: "configured-model", Effort: "medium"}},
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if selection != resumed {
		t.Errorf("got %s", selection)
	}
}

func TestResolveAvailableRoutesAnAutomaticSelectionAroundAProvider(t *testing.T) {
	configured := testModelSelections()
	selection, err := ResolveAvailable(
		model.Selection{},
		model.Selection{},
		configured,
		filepath.Join(t.TempDir(), "round-robin.json"),
		func(candidate model.Selection) bool { return candidate.Provider != opencodeGoProvider },
	)
	if err != nil {
		t.Fatal(err)
	}
	if selection != configured[1] {
		t.Errorf("got %s, want %s", selection, configured[1])
	}
}

func TestResolveAvailableStillOpensARecordedProviderForReplay(t *testing.T) {
	resumed := model.Selection{Provider: opencodeGoProvider, Model: "saved-model", Effort: "low"}
	selection, err := ResolveAvailable(
		model.Selection{},
		resumed,
		testModelSelections(),
		"",
		func(model.Selection) bool { return false },
	)
	if err != nil {
		t.Fatal(err)
	}
	if selection != resumed {
		t.Errorf("got %s", selection)
	}
}

func TestResolveRequiresAModel(t *testing.T) {
	selection, err := Resolve(model.Selection{}, model.Selection{}, nil, "")
	if err == nil || !strings.Contains(err.Error(), "-m provider/model@effort") {
		t.Fatalf("got selection %s and error %v", selection, err)
	}
}

func TestAnExplicitModelDoesNotAdvanceTheConfiguredRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "round-robin.json")
	requested := model.Selection{Provider: codexProvider, Model: "requested", Effort: "high"}
	selection, err := Resolve(requested, model.Selection{}, testModelSelections(), path)
	if err != nil {
		t.Fatal(err)
	}
	if selection != requested {
		t.Errorf("got %s", selection)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no cursor state, got %v", err)
	}
}

func TestAResumedModelDoesNotAdvanceTheConfiguredRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "round-robin.json")
	resumed := model.Selection{Provider: codexProvider, Model: "saved", Effort: "high"}
	selection, err := Resolve(model.Selection{}, resumed, testModelSelections(), path)
	if err != nil {
		t.Fatal(err)
	}
	if selection != resumed {
		t.Errorf("got %s", selection)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no cursor state, got %v", err)
	}
}

func TestASessionIsRefusedBeforeAnyoneHasLoggedIn(t *testing.T) {
	tests := map[string]struct {
		choice model.Choice
		want   string
	}{
		"codex": {
			choice: model.Choice{Provider: codexProvider, ID: "gpt-5.6-sol"},
			want:   "login command with codex",
		},
		"anthropic": {
			choice: model.Choice{Provider: anthropicProvider, ID: "claude-opus-5", MaxOutputTokens: 128_000},
			want:   "login command with anthropic",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())

			client, err := Connect(test.choice, testSelection(), EndpointSettings{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
			if client != nil {
				t.Errorf("expected no connection to be handed back, got %+v", client)
			}
		})
	}
}

func TestASessionConnectsOnceCredentialsAreStored(t *testing.T) {
	writeStoredCredentials(t)

	client, err := Connect(
		model.Choice{Provider: anthropicProvider, ID: "claude-opus-5", MaxOutputTokens: 128_000},
		testSelection(),
		EndpointSettings{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected a connection")
	}
}

func TestCodexConnectionsCarryFastMode(t *testing.T) {
	selection := model.Selection{Provider: codexProvider, Model: "gpt-5.6-sol", Effort: "high", IsFast: true}
	connection, err := Connect(
		model.Choice{Provider: codexProvider, ID: selection.Model},
		selection,
		EndpointSettings{OverrideURL: "http://somewhere"},
	)
	if err != nil {
		t.Fatal(err)
	}

	client, isCodex := connection.Client.(*codex.Client)
	if !isCodex || !client.IsFast {
		t.Errorf("got client %+v", connection.Client)
	}
}

func TestFastModeIsRejectedBeforeConnectingAnotherProvider(t *testing.T) {
	selection := model.Selection{Provider: anthropicProvider, Model: "claude-opus-5", Effort: "high", IsFast: true}
	connection, err := Connect(
		model.Choice{Provider: anthropicProvider, ID: selection.Model, MaxOutputTokens: 128_000},
		selection,
		EndpointSettings{OverrideURL: "http://somewhere"},
	)
	if err == nil || !strings.Contains(err.Error(), "does not support fast mode") {
		t.Fatalf("got connection %+v and error %v", connection, err)
	}
}

func TestOnlyASignedIntoProviderIsReadyForASession(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	for _, providerName := range []string{codexProvider, anthropicProvider, opencodeGoProvider} {
		if IsLoggedIn(providerName) {
			t.Errorf("%s was ready with no credentials stored", providerName)
		}
	}

	if !IsLoggedIn(ollamaProvider) {
		t.Error("a provider wanting no credentials was not ready")
	}

	writeStoredCredentials(t)

	for _, providerName := range []string{codexProvider, anthropicProvider} {
		if !IsLoggedIn(providerName) {
			t.Errorf("%s was not ready with credentials stored", providerName)
		}
	}
}

func TestListingModelsAsksForNoCredentials(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	if _, err := ListModels(t.Context(), anthropicProvider, EndpointSettings{OverrideURL: "http://somewhere"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeStoredCredentials(t *testing.T) {
	t.Helper()

	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	path := filepath.Join(state, "org.crdx", "io")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}

	credentials := fmt.Sprintf(
		`{"version":1,"codex":{"access":"stored","refresh":"refresh-me","account_id":"account","expires_at":%d},`+
			`"anthropic":{"access":"stored","refresh":"refresh-me","expires_at":%d}}`,
		time.Now().Add(time.Hour).UnixMilli(),
		time.Now().Add(time.Hour).UnixMilli(),
	)

	if err := os.WriteFile(filepath.Join(path, "auth.json"), []byte(credentials), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOllamaEndpointPrecedence(t *testing.T) {
	for _, test := range []struct {
		name      string
		endpoints EndpointSettings
		host      string
		want      string
	}{
		{name: "default", want: ollama.EndpointURL},
		{
			name:      "config",
			endpoints: EndpointSettings{OllamaHost: "configured:11434"},
			want:      "configured:11434",
		},
		{
			name:      "environment over config",
			endpoints: EndpointSettings{OllamaHost: "configured:11434"},
			host:      "environment:11434",
			want:      "environment:11434",
		},
		{
			name: "endpoint override over environment",
			endpoints: EndpointSettings{
				OverrideURL: "http://override/v1/chat/completions",
				OllamaHost:  "configured:11434",
			},
			host: "environment:11434",
			want: "http://override/v1/chat/completions",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(ollama.HostVariable, test.host)
			if got := ollamaEndpointURL(test.endpoints); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestOllamaConnectsToItsConfiguredHost(t *testing.T) {
	connection, err := connectProvider(
		model.Choice{Provider: ollamaProvider, ID: "qwen3.8", MaxOutputTokens: 32_768},
		testSelection(),
		EndpointSettings{OllamaHost: "speeder:11434"},
	)
	if err != nil {
		t.Fatal(err)
	}

	client, isOllama := connection.Client.(*ollama.Client)
	if !isOllama {
		t.Fatalf("got client %T", connection.Client)
	}
	if client.URL != "http://speeder:11434/v1/chat/completions" {
		t.Errorf("got Ollama conversation URL %q", client.URL)
	}
}

func TestEveryConnectionCarriesAWebSearchClient(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	endpoint := sim.New(&sim.Scenario{Model: "fake", Turns: []sim.Turn{{Say: "Hello."}}})
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)

	address := endpoint.Addresses(server.URL)[sim.Messages]

	for _, providerName := range []string{codexProvider, opencodeGoProvider, anthropicProvider, ollamaProvider} {
		client, err := Connect(
			model.Choice{Provider: providerName, ID: "fake", MaxOutputTokens: 128_000},
			testSelection(),
			EndpointSettings{OverrideURL: address},
		)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", providerName, err)
		}
		if client.Search == nil {
			t.Fatalf("%s: connection carries no search client", providerName)
		}
		if client.Search.URL != address {
			t.Errorf("%s: search asks %q, want %q", providerName, client.Search.URL, address)
		}
		if client.Search.Model != webSearchModel {
			t.Errorf("%s: search asks %q, want %q", providerName, client.Search.Model, webSearchModel)
		}
	}
}

func TestConnectReportsWhatTheProviderRefused(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	tests := []struct {
		name     string
		choice   model.Choice
		endpoint string
		want     string
	}{
		{
			"codex",
			model.Choice{Provider: codexProvider},
			"http://somewhere",
			"codex: Model is empty",
		},
		{
			"opencode-go",
			model.Choice{Provider: opencodeGoProvider, ID: "deepseek-v4-pro"},
			"http://somewhere",
			"chat: MaxOutputTokens is 0",
		},
		{
			"anthropic",
			model.Choice{Provider: anthropicProvider, MaxOutputTokens: 128_000},
			"http://somewhere",
			"anthropic: Model is empty",
		},
		{
			"ollama",
			model.Choice{Provider: ollamaProvider, ID: "qwen3.8"},
			"",
			"chat: MaxOutputTokens is 0",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := Connect(test.choice, testSelection(), EndpointSettings{OverrideURL: test.endpoint})

			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}

			if client != nil {
				t.Errorf("expected no connection to be handed back, got %+v", client)
			}
		})
	}
}

func TestConnectRefusesAnUnknownProvider(t *testing.T) {
	client, err := Connect(model.Choice{Provider: "nowhere"}, testSelection(), EndpointSettings{})
	if err == nil || !strings.Contains(err.Error(), `unknown provider "nowhere"`) {
		t.Fatalf("got connection %+v and error %v", client, err)
	}
}

func TestOpenCodeRequiresLogin(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	_, err := Connect(
		model.Choice{Provider: opencodeGoProvider, ID: "deepseek-v4-pro", MaxOutputTokens: 128_000},
		testSelection(),
		EndpointSettings{},
	)
	if err == nil || !strings.Contains(err.Error(), "login command with opencode-go") {
		t.Fatalf("got error %v", err)
	}
}
