package modelsdev

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"crdx.org/oh/pkg/agent"
)

func TestFetchOmitsModelsWithDatedVersionSuffixes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"anthropic": {"models": {
				"claude-opus-4-5": {},
				"claude-opus-4-5-20251101": {},
				"dated-id-under-an-alias": {"id": "claude-sonnet-4-5-20250929"},
				"not-a-date": {"id": "claude-release-99999999"}
			}},
			"openai": {"models": {
				"gpt-snapshot-20250101": {}
			}}
		}`))
	}))
	defer server.Close()

	registry, err := Fetch(t.Context(), server.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	anthropic := registry.Provider("anthropic")
	if len(anthropic) != 2 {
		t.Fatalf("expected only undated Anthropic models, got %v", anthropic)
	}
	if _, isFound := anthropic["claude-opus-4-5"]; !isFound {
		t.Error("expected the undated model to remain")
	}
	if _, isFound := anthropic["not-a-date"]; !isFound {
		t.Error("expected a non-date numeric suffix to remain")
	}
	if len(registry.Provider("openai")) != 0 {
		t.Errorf("expected dated models from every provider to be omitted, got %v", registry.Provider("openai"))
	}
}

func TestFetchTakesTheContextWindowFromTheInputShareOfTheBudget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"openai": {"models": {
				"split-budget": {"limit": {"context": 1050000, "input": 922000, "output": 128000}},
				"whole-budget": {"limit": {"context": 400000, "output": 128000}}
			}}
		}`))
	}))
	defer server.Close()

	registry, err := Fetch(t.Context(), server.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	openai := registry.Provider("openai")
	if got := openai["split-budget"].ContextWindowTokens; got != 922_000 {
		t.Errorf("got %d, want the input share 922000", got)
	}
	if got := openai["whole-budget"].ContextWindowTokens; got != 400_000 {
		t.Errorf("got %d, want the whole budget 400000", got)
	}
	if got := openai["split-budget"].MaxOutputTokens; got != 128_000 {
		t.Errorf("got %d, want the output limit 128000", got)
	}
}

func TestFetchTakesThePriceOfEachTokenKind(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"anthropic": {"models": {
				"priced": {"cost": {"input": 3, "output": 15, "cache_read": 0.3, "cache_write": 3.75}},
				"free": {"cost": {"input": 0, "output": 0}},
				"unpriced": {}
			}}
		}`))
	}))
	defer server.Close()

	registry, err := Fetch(t.Context(), server.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	anthropic := registry.Provider("anthropic")

	prices := anthropic["priced"].Prices
	if prices == nil {
		t.Fatal("expected the priced model to carry its prices")
	}
	if prices.Input != 3 || prices.Output != 15 || prices.CacheRead != 0.3 || prices.CacheWrite != 3.75 {
		t.Errorf("got %+v", *prices)
	}

	if anthropic["free"].Prices != nil {
		t.Error("expected a model costing nothing to carry no prices")
	}
	if anthropic["unpriced"].Prices != nil {
		t.Error("expected a model without costs to carry no prices")
	}
}

func TestFetchTellsAModelThatThinksWithoutAnEffortFromOneNothingIsKnownAbout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"opencode-go": {"models": {
				"graded": {"reasoning": true, "reasoning_options": [{"type": "effort", "values": ["low", "high"]}]},
				"toggled": {"reasoning": true, "reasoning_options": [{"type": "toggle"}]},
				"always": {"reasoning": true, "reasoning_options": []},
				"plain": {"reasoning": false},
				"undescribed": {}
			}}
		}`))
	}))
	defer server.Close()

	registry, err := Fetch(t.Context(), server.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	models := registry.Provider("opencode-go")
	for name, want := range map[string]bool{
		"graded":      false,
		"toggled":     true,
		"always":      true,
		"plain":       false,
		"undescribed": false,
	} {
		if got := models[name].IsEffortless; got != want {
			t.Errorf("%s: effortless is %t, want %t", name, got, want)
		}
	}

	if got := models["graded"].EffortLevels; len(got) != 2 {
		t.Errorf("expected the graded model to keep its levels, got %v", got)
	}
}

func TestFetchTakesEachModelsWireFromThePackageItsProviderOrItselfNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"opencode-go": {"npm": "@ai-sdk/openai-compatible", "models": {
				"inherits": {},
				"messages": {"provider": {"npm": "@ai-sdk/anthropic"}},
				"responses": {"provider": {"npm": "@ai-sdk/openai"}},
				"foreign": {"provider": {"npm": "@ai-sdk/google"}},
				"blank": {"provider": {}}
			}},
			"elsewhere": {"npm": "@ai-sdk/mistral", "models": {
				"inherits-the-unknown": {},
				"names-its-own": {"provider": {"npm": "@ai-sdk/openai-compatible"}}
			}},
			"unpackaged": {"models": {
				"nothing-said": {}
			}}
		}`))
	}))
	defer server.Close()

	registry, err := Fetch(t.Context(), server.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, test := range []struct {
		provider string
		model    string
		want     agent.Wire
	}{
		{"opencode-go", "inherits", agent.CompletionsWire},
		{"opencode-go", "messages", agent.MessagesWire},
		{"opencode-go", "responses", agent.ResponsesWire},
		{"opencode-go", "foreign", ""},
		{"opencode-go", "blank", agent.CompletionsWire},
		{"elsewhere", "inherits-the-unknown", ""},
		{"elsewhere", "names-its-own", agent.CompletionsWire},
		{"unpackaged", "nothing-said", ""},
	} {
		if got := registry.Provider(test.provider)[test.model].Wire; got != test.want {
			t.Errorf("%s/%s: got wire %q, want %q", test.provider, test.model, got, test.want)
		}
	}
}
