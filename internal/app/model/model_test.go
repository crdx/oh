package model

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/pkg/agent"
)

func writeModelCache(t *testing.T, cache modelCache) {
	t.Helper()

	t.Setenv("XDG_STATE_HOME", t.TempDir())

	if err := saveModelCache(modelCachePath(), cache); err != nil {
		t.Fatal(err)
	}
}

func TestOnlyTheCachedListingDescribesAProvider(t *testing.T) {
	cache := modelCache{
		Version: cacheVersion,
		Providers: map[string]cachedModels{
			anthropicProvider: {Models: []agent.Model{
				{ID: "claude-sonnet-5", EffortLevels: []string{"low", "high"}, MaxOutputTokens: 128_000},
				{ID: "claude-sonnet-4-6", EffortLevels: []string{"low"}, MaxOutputTokens: 64_000},
			}},
		},
	}

	available := availableModelChoices(cache)

	var models []string
	for _, choice := range available {
		if choice.Provider != anthropicProvider {
			t.Errorf("expected nothing from %s, got %s", choice.Provider, choice.ID)
		}
		models = append(models, choice.ID)
	}

	want := []string{"claude-sonnet-5", "claude-sonnet-4-6"}
	if !slices.Equal(models, want) {
		t.Errorf("expected the cached listing alone, got %v", models)
	}
}

func TestAModelTheClientCannotTalkToIsNeverOffered(t *testing.T) {
	cache := modelCache{
		Version: cacheVersion,
		Providers: map[string]cachedModels{
			anthropicProvider: {Models: []agent.Model{
				{ID: "claude-opus-5", EffortLevels: []string{"high"}, MaxOutputTokens: 128_000},
				{ID: "claude-opus-4-5", EffortLevels: []string{"high"}, MaxOutputTokens: 64_000},
			}},
		},
	}

	var models []string
	for _, choice := range availableModelChoices(cache) {
		models = append(models, choice.ID)
	}

	if want := []string{"claude-opus-5"}; !slices.Equal(models, want) {
		t.Errorf("expected the model without adaptive thinking to be left out, got %v", models)
	}
}

func TestAnEmptyCacheOffersNothingToSelect(t *testing.T) {
	available := availableModelChoices(modelCache{Version: cacheVersion, Providers: map[string]cachedModels{}})

	if len(available) != 0 {
		t.Fatalf("expected nothing before an update, got %v", available)
	}

	if _, err := matchModel("anything", available); err == nil {
		t.Error("expected selecting against an empty cache to say so")
	}
}

func TestListingModelsPrintsEverySelectableQualifiedName(t *testing.T) {
	useCachedModels(t)

	var output bytes.Buffer
	if err := List(&output, modelCachePath()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := strings.Join([]string{
		"codex/gpt-5.6-sol",
		"opencode-go/deepseek-v4-pro",
		"anthropic/claude-opus-5",
	}, "\n") + "\n"
	if output.String() != want {
		t.Errorf("got %q, want %q", output.String(), want)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("writer failed")
}

func TestListingModelsReturnsWriterFailures(t *testing.T) {
	useCachedModels(t)

	err := List(failingWriter{}, modelCachePath())
	if err == nil || !strings.Contains(err.Error(), "writer failed") {
		t.Errorf("got %v", err)
	}
}

func TestListingModelsWithoutACacheSaysHowToFetchThem(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	if err := List(&bytes.Buffer{}, modelCachePath()); err == nil || !strings.Contains(err.Error(), "-u") {
		t.Errorf("expected the empty listing to say how to fetch models, got %v", err)
	}
}

func TestAModelTakingNoEffortLevelCannotBeSelected(t *testing.T) {
	choices := choicesFor(codexProvider, []agent.Model{
		{ID: "gpt-5.6-sol", EffortLevels: []string{"high"}, MaxOutputTokens: 128_000},
		{ID: "gpt-realtime-2.1", EffortLevels: []string{"high"}, MaxOutputTokens: 16_000},
		{ID: "chatgpt-image-latest"},
		{ID: "", EffortLevels: []string{"high"}, MaxOutputTokens: 128_000},
	})

	if len(choices) != 1 || choices[0].ID != "gpt-5.6-sol" {
		t.Errorf("expected only what can be asked to think, got %v", choices)
	}
}

func TestOpenCodeGoOnlyOffersModelsForItsWireProtocol(t *testing.T) {
	models := []agent.Model{
		{ID: "minimax-m3", EffortLevels: []string{"high"}, MaxOutputTokens: 128_000},
		{ID: "qwen3.8-max", EffortLevels: []string{"high"}, MaxOutputTokens: 128_000},
		{ID: "muse-spark-1.2-contributor", EffortLevels: []string{"high"}, MaxOutputTokens: 128_000},
		{ID: "ox-alpha-free", EffortLevels: []string{"high"}, MaxOutputTokens: 128_000},
		{ID: "mimo-v2-omni", EffortLevels: []string{"high"}, MaxOutputTokens: 128_000},
	}

	choices := choicesFor(opencodeGoProvider, models)
	if len(choices) != 2 || choices[0].ID != "ox-alpha-free" || choices[1].ID != "mimo-v2-omni" {
		t.Errorf("got %v", choices)
	}
}

func TestOllamaModelsAreSelectableWithoutARegistryEntry(t *testing.T) {
	choices := choicesFor(ollamaProvider, []agent.Model{{
		ID:                  "qwen3.8:27b",
		EffortLevels:        []string{"none", "high"},
		ContextWindowTokens: 262_144,
		MaxOutputTokens:     32_768,
	}})

	if len(choices) != 1 {
		t.Fatalf("got choices %v", choices)
	}
	if choices[0].Provider != ollamaProvider || choices[0].ID != "qwen3.8:27b" ||
		choices[0].ContextWindowTokens != 262_144 || choices[0].MaxOutputTokens != 32_768 {
		t.Errorf("got choice %+v", choices[0])
	}
}

func TestTheRegistryFillsInWhatAListingLeftOut(t *testing.T) {
	listed := []agent.Model{
		{ID: "claude-opus-5", EffortLevels: []string{"high"}},
		{ID: "claude-sonnet-5"},
		{ID: "unknown-model"},
	}

	registered := map[string]agent.Model{
		"claude-opus-5": {
			ID:                  "claude-opus-5",
			Name:                "Claude Opus 5",
			EffortLevels:        []string{"low", "medium", "high", "xhigh", "max"},
			ContextWindowTokens: 274_000,
			MaxOutputTokens:     128_000,
		},
		"claude-sonnet-5": {
			ID:           "claude-sonnet-5",
			Name:         "Claude Sonnet 5",
			EffortLevels: []string{"low", "high"},
			Prices:       &agent.TokenPrices{Input: 3, Output: 15},
		},
	}

	supplemented := supplement(AnthropicProvider, listed, registered)

	if supplemented[0].Name != "Claude Opus 5" || supplemented[0].ContextWindowTokens != 274_000 ||
		supplemented[0].MaxOutputTokens != 128_000 {
		t.Errorf("expected the registry to fill in the gaps, got %+v", supplemented[0])
	}

	if !slices.Equal(supplemented[0].EffortLevels, []string{"high"}) {
		t.Errorf("expected what the endpoint said to stand, got %v", supplemented[0].EffortLevels)
	}

	if !slices.Equal(supplemented[1].EffortLevels, []string{"low", "high"}) {
		t.Errorf("expected the registry to supply the missing efforts, got %v", supplemented[1].EffortLevels)
	}

	if supplemented[2].ID != "unknown-model" || supplemented[2].Name != "" {
		t.Errorf("expected a model the registry does not know to be left alone, got %+v", supplemented[2])
	}

	if supplemented[1].Prices == nil || supplemented[1].Prices.Output != 15 {
		t.Errorf("expected the registry to supply the missing prices, got %+v", supplemented[1].Prices)
	}

	if supplemented[0].Prices != nil {
		t.Errorf("expected an unpriced model to stay unpriced, got %+v", supplemented[0].Prices)
	}
}

func TestTheRegistrySuppliesTheMaximumCodexContextWindow(t *testing.T) {
	listed := []agent.Model{{
		ID:                  "gpt-5.6-sol",
		ContextWindowTokens: 272_000,
	}}
	registered := map[string]agent.Model{
		"gpt-5.6-sol": {
			ID:                  "gpt-5.6-sol",
			ContextWindowTokens: 1_000_000,
		},
	}

	codexModels := supplement(CodexProvider, listed, registered)
	if codexModels[0].ContextWindowTokens != 1_000_000 {
		t.Errorf("Codex got a context window of %d", codexModels[0].ContextWindowTokens)
	}

	anthropicModels := supplement(AnthropicProvider, listed, registered)
	if anthropicModels[0].ContextWindowTokens != 272_000 {
		t.Errorf("Anthropic got a context window of %d", anthropicModels[0].ContextWindowTokens)
	}
}

func TestOnlyTheLatestIterationOfEachCurrentModelIsRetained(t *testing.T) {
	tests := []struct {
		name    string
		current []string
		want    []string
	}{
		{
			"codex",
			[]string{
				"gpt-5", "gpt-5-mini", "gpt-5-nano", "gpt-5-pro", "gpt-5.1", "gpt-5.2",
				"gpt-5.2-chat-latest", "gpt-5.2-pro", "gpt-5.3-codex", "gpt-5.3-codex-spark",
				"gpt-5.4", "gpt-5.4-mini", "gpt-5.4-nano", "gpt-5.4-pro", "gpt-5.5", "gpt-5.5-pro",
				"gpt-5.6", "gpt-5.6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-realtime-2.1",
				"o1", "o1-pro", "o3", "o3-mini", "o3-pro", "o4-mini",
			},
			[]string{
				"gpt-5.2-chat-latest", "gpt-5.3-codex", "gpt-5.3-codex-spark", "gpt-5.4-mini",
				"gpt-5.4-nano", "gpt-5.5-pro", "gpt-5.6", "gpt-5.6-luna", "gpt-5.6-sol",
				"gpt-5.6-terra", "gpt-realtime-2.1", "o3", "o3-pro", "o4-mini",
			},
		},
		{
			"opencode-go",
			[]string{
				"deepseek-v4-flash", "deepseek-v4-flash-vision-exp", "deepseek-v4-pro", "glm-5.2",
				"glm-5.3", "gpt-5.6-luna", "grok-4.5", "grok-4.6", "hy3", "kimi-k3",
				"muse-spark-1.2-contributor", "ox-alpha-free",
			},
			[]string{
				"deepseek-v4-flash", "deepseek-v4-flash-vision-exp", "deepseek-v4-pro", "glm-5.3",
				"gpt-5.6-luna", "grok-4.6", "hy3", "kimi-k3", "muse-spark-1.2-contributor",
				"ox-alpha-free",
			},
		},
		{
			"anthropic",
			[]string{
				"claude-fable-5", "claude-opus-4-6", "claude-opus-4-7", "claude-opus-4-8",
				"claude-opus-5", "claude-sonnet-4-6", "claude-sonnet-5",
			},
			[]string{"claude-fable-5", "claude-opus-5", "claude-sonnet-5"},
		},
		{
			"anthropic with dated snapshots",
			[]string{
				"claude-fable-5", "claude-fable-5-1", "claude-fable-5-20260609",
				"claude-opus-4-5-20251101", "claude-opus-5", "claude-sonnet-4-5-20250929",
				"claude-sonnet-5",
			},
			[]string{"claude-fable-5-1", "claude-opus-5", "claude-sonnet-5"},
		},
		{
			"anthropic snapshots of one iteration",
			[]string{"claude-opus-4-5", "claude-opus-4-5-20251101"},
			[]string{"claude-opus-4-5-20251101"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			models := make([]agent.Model, len(test.current))
			for i, modelID := range test.current {
				models[i].ID = modelID
			}

			latest := latestModelIterations(models)

			var got []string

			for _, model := range models {
				if _, isSuperseded := supersededBy(latest, model.ID); !isSuperseded {
					got = append(got, model.ID)
				}
			}

			if !slices.Equal(got, test.want) {
				t.Errorf("got %v, want %v", got, test.want)
			}
		})
	}
}

func TestModelSelectionResolvesAgainstTheCachedListing(t *testing.T) {
	writeModelCache(t, modelCache{
		Version: cacheVersion,
		Providers: map[string]cachedModels{
			anthropicProvider: {Models: []agent.Model{
				{ID: "claude-sonnet-5", EffortLevels: []string{"low", "medium", "high"}, MaxOutputTokens: 128_000},
			}},
		},
	})

	provider, model, effort, err := parseModelSelection("sonnet@hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if provider != anthropicProvider || model != "claude-sonnet-5" || effort != "high" {
		t.Errorf("got %s/%s@%s", provider, model, effort)
	}
}

func TestTheOutputCeilingComesFromTheChosenModel(t *testing.T) {
	writeModelCache(t, modelCache{
		Version: cacheVersion,
		Providers: map[string]cachedModels{
			anthropicProvider: {Models: []agent.Model{
				{ID: "claude-opus-5", EffortLevels: []string{"high"}, MaxOutputTokens: 128_000},
				{ID: "claude-sonnet-4-6", EffortLevels: []string{"high"}, MaxOutputTokens: 64_000},
			}},
		},
	})

	for model, want := range map[string]int{"claude-opus-5": 128_000, "claude-sonnet-4-6": 64_000} {
		choice, err := chosenAnthropicModel(model)
		if err != nil {
			t.Fatalf("%s: %v", model, err)
		}

		if choice.MaxOutputTokens != want {
			t.Errorf("expected %s to allow %d, got %d", model, want, choice.MaxOutputTokens)
		}
	}
}

func TestAModelNothingIsKnownAboutIsRefusedWithSomethingToDoAboutIt(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	_, err := chosenAnthropicModel("claude-from-the-future")
	if err == nil || !strings.Contains(err.Error(), "-u") {
		t.Errorf("expected the refusal to say what to do, got %v", err)
	}
}

func TestAModelTheClientCannotTalkToIsRefusedWithTheReasonRatherThanAnUpdate(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	_, err := chosenAnthropicModel("claude-opus-4-5")
	if err == nil || !strings.Contains(err.Error(), undrivableReason) {
		t.Errorf("expected the refusal to say the model cannot be spoken to, got %v", err)
	}
}

func TestAnEffortACachedModelDoesNotTakeIsRefused(t *testing.T) {
	writeModelCache(t, modelCache{
		Version: cacheVersion,
		Providers: map[string]cachedModels{
			anthropicProvider: {Models: []agent.Model{
				{ID: "claude-sonnet-5", EffortLevels: []string{"low", "medium"}, MaxOutputTokens: 64_000},
			}},
		},
	})

	if _, _, _, err := parseModelSelection("sonnet@max"); err == nil {
		t.Error("expected an effort the model does not take to be refused")
	}
}

func TestACachedModelListIsReadWithoutWhatATerminalWouldObey(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path := modelCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	data := fmt.Appendf(nil,
		`{"version":%d,"providers":{"anthropic":{"models":[`+
			`{"id":"claude-\u001b[2Jsonnet-5","name":"Sonnet \u001b]52;c;cHduZWQ=\u0007"}]}}}`,
		cacheVersion,
	)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	models := loadModelCache(path).Providers["anthropic"].Models
	if len(models) != 1 {
		t.Fatalf("got %d models, want the one that was cached", len(models))
	}

	for name, text := range map[string]string{"id": models[0].ID, "name": models[0].Name} {
		for _, character := range text {
			if unicode.IsControl(character) {
				t.Errorf("the cached %s holds %q: %q", name, character, text)
			}
		}
	}

	if got, want := models[0].ID, "claude-[2Jsonnet-5"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestACacheInAnotherFormatIsIgnored(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path := modelCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	for _, version := range []int{2, 99} {
		data := fmt.Appendf(nil, `{"version":%d,"providers":{"anthropic":{}}}`, version)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}

		if got := loadModelCache(path); len(got.Providers) != 0 {
			t.Errorf("expected version %d to be ignored, got %v", version, got)
		}

		if _, _, _, err := parseModelSelection("opus@hi"); err == nil {
			t.Errorf("expected version %d to leave nothing selectable", version)
		}
	}
}

func TestSavingModelsReturnsStateDirectoryFailures(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parent, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := saveModelCache(filepath.Join(parent, "models.json"), modelCache{})
	if err == nil || !strings.Contains(err.Error(), "create state directory") {
		t.Errorf("got %v", err)
	}
}

func TestSavingModelsReturnsCacheWriteFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := saveModelCache(path, modelCache{}); err == nil {
		t.Error("expected writing over a directory to fail")
	}
}

const deadAddress = "http://127.0.0.1:1"

func serveRegistry(t *testing.T, body string) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path != simulatedRegistryPath {
				http.Error(writer, "nothing here", http.StatusNotFound)

				return
			}

			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(body))
		},
	))

	t.Cleanup(server.Close)

	return server.URL + "/v1/messages"
}

func TestUpdatingWithNothingReachableSaysSo(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	if err := updateModelsWithoutProviderListings(&output, deadAddress, modelCachePath()); err == nil {
		t.Fatalf("expected the update to fail, got output %q", output.String())
	}
}

func TestTheRegistryIsReadFromAStandInEndpointWhenOneIsNamed(t *testing.T) {
	if got := registryAddress(""); got != "" {
		t.Errorf("expected the real registry when no endpoint is named, got %q", got)
	}

	want := "http://localhost:8080" + simulatedRegistryPath +
		"?providers=anthropic%2Copenai%2Copencode-go"

	if got := registryAddress("http://localhost:8080/v1/messages"); got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestTheRealRegistryIsNeverAskedAboutOurProviderNames(t *testing.T) {
	for _, endpoint := range []string{"", "not a url at all", "://broken"} {
		if got := registryAddress(endpoint); got != "" {
			t.Errorf("expected nothing for %q, got %q", endpoint, got)
		}
	}
}

func TestAStandInEndpointKeepsAModelCacheOfItsOwn(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	realCache := modelCachePath()

	t.Setenv(endpointVariable, "http://localhost:8080/v1/messages")

	if standIn := modelCachePath(); standIn == realCache {
		t.Errorf("expected a cache of its own, got %q for both", standIn)
	}
}

func TestProviderDescriptionCoversEveryEndpointAndRegistryCombination(t *testing.T) {
	listed := []agent.Model{{ID: "known", EffortLevels: []string{"high"}, MaxOutputTokens: 128_000}}
	registered := map[string]agent.Model{"known": {ID: "known", Name: "Known"}}
	listingFailure := errors.New("listing failed")

	tests := map[string]struct {
		listed     []agent.Model
		listingErr error
		registered map[string]agent.Model
		wantSource string
		wantWhy    string
		wantModels int
	}{
		"endpoint only":         {listed: listed, wantSource: sourceEndpoint, wantModels: 1},
		"endpoint and registry": {listed: listed, registered: registered, wantSource: sourceBoth, wantModels: 1},
		"registry only":         {registered: registered, wantSource: sourceRegistry, wantWhy: "the endpoint lists no models", wantModels: 1},
		"neither":               {listingErr: listingFailure, wantWhy: listingFailure.Error()},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			models, source, why := describeProviderModels(
				t.Context(),
				codexProvider,
				test.registered,
				func(context.Context, string) ([]agent.Model, error) {
					return test.listed, test.listingErr
				},
			)
			if len(models) != test.wantModels || source != test.wantSource || why != test.wantWhy {
				t.Errorf("got models=%v source=%q why=%q", models, source, why)
			}
		})
	}
}

func TestEndpointListingExcludesRegistryModelsTheProviderCannotUse(t *testing.T) {
	listed := []agent.Model{{ID: "gpt-5.6-sol", EffortLevels: []string{"high"}}}
	registered := map[string]agent.Model{
		"gpt-5.6-sol":      {ID: "gpt-5.6-sol", MaxOutputTokens: 128_000},
		"gpt-realtime-2.1": {ID: "gpt-realtime-2.1", MaxOutputTokens: 16_000},
	}

	models, source, _ := describeProviderModels(
		t.Context(),
		codexProvider,
		registered,
		func(context.Context, string) ([]agent.Model, error) {
			return listed, nil
		},
	)

	if source != sourceBoth || len(models) != 1 || models[0].ID != "gpt-5.6-sol" {
		t.Errorf("got models=%v source=%q", models, source)
	}
}

func TestAProviderThatListsNothingIsDescribedByTheRegistryAlone(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	endpoint := serveRegistry(t, `{
		"openai": {"models": {
			"gpt-5.5-sol": {
				"id": "gpt-5.5-sol", "name": "GPT-5.5 Sol", "reasoning": true,
				"reasoning_options": [{"type": "effort", "values": ["low", "high"]}],
				"limit": {"context": 300000, "output": 64000}
			},
			"gpt-5.6-sol": {
				"id": "gpt-5.6-sol", "name": "GPT-5.6 Sol", "reasoning": true,
				"reasoning_options": [{"type": "effort", "values": ["low", "high", "max"]}],
				"limit": {"context": 400000, "output": 128000}
			},
			"chatgpt-image-latest": {"id": "chatgpt-image-latest", "name": "Image"}
		}}
	}`)

	var output bytes.Buffer
	if err := updateModelsWithoutProviderListings(&output, endpoint, modelCachePath()); err != nil {
		t.Fatalf("unexpected error: %v, output %q", err, output.String())
	}

	cached := loadModelCache(modelCachePath()).Providers[codexProvider]
	if cached.Source != sourceRegistry {
		t.Errorf("expected the registry to stand in, got source %q", cached.Source)
	}

	if len(cached.Models) != 1 {
		t.Fatalf("expected only the compatible latest model to be recorded, got %v", cached.Models)
	}

	wantRow := "Codex              3           1        2  models.dev (the endpoint lists no models)\n"
	if !strings.Contains(style.Plain(output.String()), wantRow) {
		t.Errorf("expected the row to name why the endpoint described nothing, got %q", output.String())
	}

	choices := choicesFor(codexProvider, cached.Models)
	if len(choices) != 1 || choices[0].ID != "gpt-5.6-sol" {
		t.Fatalf("expected only the one that reasons to be selectable, got %v", choices)
	}

	if !slices.Equal(choices[0].EffortLevels, []string{"low", "high", "max"}) {
		t.Errorf("expected the registry's effort levels, got %v", choices[0].EffortLevels)
	}
}

const oneCodexModel = `{
	"openai": {"models": {
		"gpt-5.6-sol": {
			"id": "gpt-5.6-sol", "name": "GPT-5.6 Sol", "reasoning": true,
			"reasoning_options": [{"type": "effort", "values": ["low", "high"]}],
			"limit": {"context": 400000, "output": 128000},
			"cost": {"input": 1.25, "output": 10, "cache_read": 0.125, "cache_write": 1.25}
		}
	}}
}`

func writeCheckedModelCache(t *testing.T, checked time.Time) {
	t.Helper()

	if err := saveModelCache(modelCachePath(), modelCache{
		CheckedAt: checked,
		Providers: map[string]cachedModels{
			codexProvider: {FetchedAt: checked, Source: sourceRegistry, Models: []agent.Model{
				{ID: "stale-model", EffortLevels: []string{"high"}, MaxOutputTokens: 128_000},
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestACacheCheckedWithinTheWeekIsLeftAlone(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeCheckedModelCache(t, time.Now().Add(-6*24*time.Hour))

	var output bytes.Buffer
	if err := ensureModelsWithoutProviderListings(&output, deadAddress, modelCachePath()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.Len() != 0 {
		t.Errorf("expected a current cache to be left alone, got %q", output.String())
	}

	cached := loadModelCache(modelCachePath()).Providers[codexProvider]
	if len(cached.Models) != 1 || cached.Models[0].ID != "stale-model" {
		t.Errorf("expected the cached models to stand, got %v", cached.Models)
	}
}

func TestACacheOlderThanAWeekIsRefreshed(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeCheckedModelCache(t, time.Now().Add(-8*24*time.Hour))

	var output bytes.Buffer
	endpoint := serveRegistry(t, oneCodexModel)
	if err := ensureModelsWithoutProviderListings(&output, endpoint, modelCachePath()); err != nil {
		t.Fatalf("unexpected error: %v, output %q", err, output.String())
	}

	drawn := style.Plain(output.String())
	if !strings.HasPrefix(drawn, refreshMessage+"\n") {
		t.Errorf("expected the refresh to say so, got %q", output.String())
	}

	for _, wanted := range []string{"gpt-5.6-sol  " + addedChange, "stale-model  " + removedChange} {
		if !strings.Contains(drawn, wanted) {
			t.Errorf("expected the refresh to name %q, got %q", wanted, output.String())
		}
	}

	cached := loadModelCache(modelCachePath()).Providers[codexProvider]
	if len(cached.Models) != 1 || cached.Models[0].ID != "gpt-5.6-sol" {
		t.Errorf("expected the refreshed models, got %v", cached.Models)
	}

	if !isCacheCurrent(loadModelCache(modelCachePath()), time.Now()) {
		t.Error("expected the refresh to be stamped")
	}
}

func TestARefreshThatFindsTheSameModelsSaysOnlyThatItRefreshed(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	endpoint := serveRegistry(t, oneCodexModel)

	checked := time.Now().Add(-8 * 24 * time.Hour)
	if err := saveModelCache(modelCachePath(), modelCache{
		CheckedAt: checked,
		Providers: map[string]cachedModels{
			codexProvider: {FetchedAt: checked, Source: sourceRegistry, Models: []agent.Model{
				{ID: "gpt-5.6-sol", EffortLevels: []string{"low", "high"}, MaxOutputTokens: 128_000},
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := ensureModelsWithoutProviderListings(&output, endpoint, modelCachePath()); err != nil {
		t.Fatalf("unexpected error: %v, output %q", err, output.String())
	}

	if style.Plain(output.String()) != refreshMessage+"\n" {
		t.Errorf("expected a refresh finding the same models to say so and nothing more, got %q", output.String())
	}
}

func TestNothingCachedIsFetchedRatherThanAskedFor(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	endpoint := serveRegistry(t, oneCodexModel)
	if err := ensureModelsWithoutProviderListings(&output, endpoint, modelCachePath()); err != nil {
		t.Fatalf("unexpected error: %v, output %q", err, output.String())
	}

	choices := availableModelChoices(loadModelCache(modelCachePath()))
	if len(choices) != 1 || choices[0].ID != "gpt-5.6-sol" {
		t.Errorf("expected an empty cache to be filled, got %v", choices)
	}
}

func TestAPriceSurvivesTheCacheAndReachesTheChoice(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	endpoint := serveRegistry(t, oneCodexModel)
	if err := ensureModelsWithoutProviderListings(&output, endpoint, modelCachePath()); err != nil {
		t.Fatalf("unexpected error: %v, output %q", err, output.String())
	}

	choices := availableModelChoices(loadModelCache(modelCachePath()))
	if len(choices) != 1 {
		t.Fatalf("expected one choice, got %v", choices)
	}

	prices := choices[0].Prices
	if prices == nil {
		t.Fatal("expected the cached model to carry its prices")
	}
	if prices.Input != 1.25 || prices.Output != 10 || prices.CacheRead != 0.125 || prices.CacheWrite != 1.25 {
		t.Errorf("got %+v", *prices)
	}
}

func TestARefreshThatFailsKeepsWhatIsCachedAndWaitsBeforeAskingAgain(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeCheckedModelCache(t, time.Now().Add(-8*24*time.Hour))

	var output bytes.Buffer
	if err := ensureModelsWithoutProviderListings(&output, deadAddress, modelCachePath()); err != nil {
		t.Fatalf("expected a failed refresh to be forgiven, got %v", err)
	}

	if !strings.Contains(output.String(), "model list not refreshed") {
		t.Errorf("expected the failure to be reported, got %q", output.String())
	}

	for _, providerName := range ProviderNames() {
		if !strings.Contains(output.String(), ProviderName(providerName)) {
			t.Errorf("expected a failure to show what %s said, got %q", providerName, output.String())
		}
	}

	cached := loadModelCache(modelCachePath()).Providers[codexProvider]
	if len(cached.Models) != 1 || cached.Models[0].ID != "stale-model" {
		t.Errorf("expected the cached models to survive, got %v", cached.Models)
	}

	if !isCacheCurrent(loadModelCache(modelCachePath()), time.Now()) {
		t.Error("expected the attempt to be stamped so the next start does not wait again")
	}
}

func TestAnUpdateThatFindsTheSameModelsNamesNoChange(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	endpoint := serveRegistry(t, oneCodexModel)

	var firstOutput bytes.Buffer
	if err := Update(&firstOutput, endpoint, modelCachePath(), seenModelsPath(), listingModels(firstListings()), false); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := Update(&output, endpoint, modelCachePath(), seenModelsPath(), listingModels(firstListings()), false); err != nil {
		t.Fatal(err)
	}

	if output.String() != firstOutput.String() {
		t.Errorf(
			"expected an update finding the same models to draw what it drew before\n--- got ---\n%s--- want ---\n%s",
			output.String(), firstOutput.String(),
		)
	}
}

func TestAProviderNobodyCouldListKeepsItsModelsAndNamesNoRemoval(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	endpoint := serveRegistry(t, oneCodexModel)

	var firstOutput bytes.Buffer
	if err := Update(&firstOutput, endpoint, modelCachePath(), seenModelsPath(), listingModels(firstListings()), false); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := Update(&output, endpoint, modelCachePath(), seenModelsPath(), unreachableProviders, false); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(style.Plain(output.String()), removedChange) {
		t.Errorf("expected the models nobody could list to stand, got %q", output.String())
	}

	cached := loadModelCache(modelCachePath()).Providers[anthropicProvider]
	if len(cached.Models) != 2 {
		t.Errorf("expected the cached models to survive, got %v", cached.Models)
	}
}

func TestNothingCachedAndNothingReachableIsAnError(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	if err := ensureModelsWithoutProviderListings(&output, deadAddress, modelCachePath()); err == nil {
		t.Fatalf("expected an empty cache with nothing reachable to fail, got %q", output.String())
	}
}
