package model

import (
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/pkg/agent"
)

func useCachedModels(t *testing.T) {
	t.Helper()

	writeModelCache(t, modelCache{
		Version: cacheVersion,
		Providers: map[string]cachedModels{
			codexProvider: {Models: []agent.Model{{
				ID:              "gpt-5.6-sol",
				EffortLevels:    []string{"none", "low", "medium", "high", "xhigh", "max"},
				MaxOutputTokens: 128_000,
			}}},
			opencodeGoProvider: {Models: []agent.Model{{
				ID:              "deepseek-v4-pro",
				EffortLevels:    []string{"high", "max"},
				MaxOutputTokens: 384_000,
			}}},
			anthropicProvider: {Models: []agent.Model{{
				ID:              "claude-opus-5",
				EffortLevels:    []string{"low", "medium", "high", "xhigh", "max"},
				MaxOutputTokens: 128_000,
			}}},
		},
	})
}

func TestModelSelectionAcceptsQualifiedAndFuzzyNames(t *testing.T) {
	useCachedModels(t)

	for _, selection := range []string{
		"opencode-go/deepseek-v4-pro@high",
		"deepseek@hi",
		"deepseek-v4@high",
	} {
		provider, model, effort, err := parseModelSelection(selection)
		if err != nil {
			t.Errorf("%s: %v", selection, err)
			continue
		}
		if provider != opencodeGoProvider || model != "deepseek-v4-pro" || effort != "high" {
			t.Errorf("%s: got %s/%s@%s", selection, provider, model, effort)
		}
	}
}

func TestCodexSelectionsMayEnableFastMode(t *testing.T) {
	useCachedModels(t)

	for writtenSelection, wantEffort := range map[string]string{
		"codex/gpt-5.6-sol@high+fast": "high",
		"sol+fast":                    "max",
	} {
		selection, err := ParseSelection(modelCachePath(), writtenSelection, Defaults{})
		if err != nil {
			t.Errorf("%s: %v", writtenSelection, err)
			continue
		}
		if !selection.IsFast || selection.Effort != wantEffort {
			t.Errorf("%s: got %s", writtenSelection, selection)
		}
		if !strings.HasSuffix(selection.String(), "+fast") {
			t.Errorf("%s lost fast mode in its canonical spelling", selection)
		}
	}
}

func TestTheConfiguredDefaultsApplyOnlyWhereNoEffortIsWritten(t *testing.T) {
	useCachedModels(t)

	defaults := Defaults{Effort: "xhigh", IsFast: true}

	for writtenSelection, want := range map[string]string{
		"sol":                         "codex/gpt-5.6-sol@xhigh+fast",
		"sol@high":                    "codex/gpt-5.6-sol@high",
		"anthropic/claude-opus-5":     "anthropic/claude-opus-5@xhigh",
		"opencode-go/deepseek-v4-pro": "opencode-go/deepseek-v4-pro@max",
	} {
		selection, err := ParseSelection(modelCachePath(), writtenSelection, defaults)
		if err != nil {
			t.Errorf("%s: %v", writtenSelection, err)
			continue
		}
		if selection.String() != want {
			t.Errorf("%s: got %s, want %s", writtenSelection, selection, want)
		}
	}
}

func TestAnAutomaticEffortIsHighOrAbove(t *testing.T) {
	for _, test := range []struct {
		wantedEffort Effort
		available    []string
		want         string
	}{
		{wantedEffort: "medium", available: []string{"medium", "xhigh"}, want: "xhigh"},
		{wantedEffort: "medium", available: []string{"medium", "max"}, want: "max"},
		{wantedEffort: "medium", available: []string{"medium", "high", "xhigh"}, want: "high"},
		{wantedEffort: "low", available: []string{"low", "medium", "high", "xhigh", "max"}, want: "high"},
		{wantedEffort: "none", available: []string{"none", "low", "medium", "high"}, want: "high"},
		{wantedEffort: "", available: []string{"low", "high"}, want: "high"},
		{wantedEffort: "high", available: []string{"medium", "high", "xhigh"}, want: "high"},
		{wantedEffort: "xhigh", available: []string{"high", "xhigh", "max"}, want: "xhigh"},
		{wantedEffort: "xhigh", available: []string{"high", "max"}, want: "max"},
	} {
		got := Defaults{Effort: test.wantedEffort}.EffortFor(test.available)
		if got != test.want {
			t.Errorf("%q of %v: got %q, want %q", test.wantedEffort, test.available, got, test.want)
		}
	}
}

func TestAnAutomaticEffortFallsBackToWhatTheModelOffers(t *testing.T) {
	for _, test := range []struct {
		wantedEffort Effort
		available    []string
		want         string
	}{
		{wantedEffort: "none", available: []string{"none", "minimal"}, want: "none"},
		{wantedEffort: "medium", available: []string{"none", "minimal"}, want: "minimal"},
		{wantedEffort: "high", available: []string{"low", "medium"}, want: "medium"},
		{wantedEffort: "max", available: []string{"none", "low", "medium"}, want: "medium"},
	} {
		got := Defaults{Effort: test.wantedEffort}.EffortFor(test.available)
		if got != test.want {
			t.Errorf("%q of %v: got %q, want %q", test.wantedEffort, test.available, got, test.want)
		}
	}
}

func TestFastModeIsRefusedWhereItCannotBeProvided(t *testing.T) {
	useCachedModels(t)

	for _, writtenSelection := range []string{
		"anthropic/claude-opus-5@high+fast",
		"opencode-go/deepseek-v4-pro@high+fast",
	} {
		if _, err := ParseSelection(modelCachePath(), writtenSelection, Defaults{}); err == nil ||
			!strings.Contains(err.Error(), "does not support fast mode") {
			t.Errorf("%s: got %v", writtenSelection, err)
		}
	}
}

func TestUnknownModelModesAreRefused(t *testing.T) {
	useCachedModels(t)

	for _, writtenSelection := range []string{"sol+", "sol+slow", "sol+fast+fast"} {
		if _, err := ParseSelection(modelCachePath(), writtenSelection, Defaults{}); err == nil {
			t.Errorf("expected %s to be refused", writtenSelection)
		}
	}
}

func TestNewSessionQueriesCarryFastMode(t *testing.T) {
	choices := []Choice{{Provider: codexProvider, ID: "gpt-5.6-sol", EffortLevels: []string{"medium", "high"}}}
	selection, err := ResolveQuery("sol+fast", choices, Defaults{Effort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if !selection.IsFast || selection.String() != "codex/gpt-5.6-sol@high+fast" {
		t.Errorf("got %s", selection)
	}
}

func TestModelSelectionReachesAnthropic(t *testing.T) {
	useCachedModels(t)

	for _, selection := range []string{
		"anthropic/claude-opus-5@high",
		"opus@hi",
		"claude-opus-5@high",
	} {
		provider, model, effort, err := parseModelSelection(selection)
		if err != nil {
			t.Errorf("%s: %v", selection, err)
			continue
		}
		if provider != anthropicProvider || model != "claude-opus-5" || effort != "high" {
			t.Errorf("%s: got %s/%s@%s", selection, provider, model, effort)
		}
	}
}

func TestAnthropicOffersEveryEffortLevelTheModelTakes(t *testing.T) {
	useCachedModels(t)

	for _, effort := range []string{"low", "medium", "high", "xhigh", "max"} {
		_, _, resolved, err := parseModelSelection("anthropic/claude-opus-5@" + effort)
		if err != nil {
			t.Errorf("%s: %v", effort, err)
			continue
		}
		if resolved != effort {
			t.Errorf("expected %s, got %s", effort, resolved)
		}
	}
}

func TestASelectionWithoutAnEffortSettlesOnTheHighestOffered(t *testing.T) {
	useCachedModels(t)

	for selection, want := range map[string]string{
		"anthropic/claude-opus-5": "max",
		"deepseek":                "max",
		"sol":                     "max",
	} {
		_, _, effort, err := parseModelSelection(selection)
		if err != nil {
			t.Errorf("%s: %v", selection, err)
			continue
		}
		if effort != want {
			t.Errorf("expected %s to select %s, got %s", selection, want, effort)
		}
	}
}

func TestASelectionWithAnEmptyEffortIsRefused(t *testing.T) {
	useCachedModels(t)

	if _, _, _, err := parseModelSelection("sol@"); err == nil {
		t.Error("expected a selection naming no effort after @ to be refused")
	}
}

func listedModels() []Choice {
	return []Choice{
		{Provider: anthropicProvider, ID: "claude-opus-4-5"},
		{Provider: anthropicProvider, ID: "claude-opus-4-5-20251101"},
		{Provider: anthropicProvider, ID: "claude-opus-5"},
		{Provider: anthropicProvider, ID: "claude-sonnet-5"},
		{Provider: codexProvider, ID: "gpt-5.6-sol"},
	}
}

func TestACloserReadingOfAQueryWinsOutright(t *testing.T) {
	for query, want := range map[string]string{
		"anthropic/claude-opus-5":  "claude-opus-5",
		"claude-opus-5":            "claude-opus-5",
		"claude-opus-4-5":          "claude-opus-4-5",
		"opus-5":                   "claude-opus-5",
		"claude-opus-4-5-20251101": "claude-opus-4-5-20251101",
		"sonnet":                   "claude-sonnet-5",
		"sol":                      "gpt-5.6-sol",
		"gpt":                      "gpt-5.6-sol",
		"gp56":                     "gpt-5.6-sol",
	} {
		choice, err := matchModel(query, listedModels())
		if err != nil {
			t.Errorf("%s: %v", query, err)

			continue
		}

		if choice.ID != want {
			t.Errorf("expected %s to find %s, got %s", query, want, choice.ID)
		}
	}
}

func TestABareQueryBorrowsNoLettersFromTheProviderName(t *testing.T) {
	choices := []Choice{
		{Provider: anthropicProvider, ID: "claude-opus-5"},
		{Provider: anthropicProvider, ID: "claude-sonnet-5"},
	}

	choice, err := matchModel("opus5", choices)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if choice.ID != "claude-opus-5" {
		t.Errorf("got %s", choice.ID)
	}
}

func TestAQualifiedQueryIsStillReadLoosely(t *testing.T) {
	choices := []Choice{
		{Provider: opencodeGoProvider, ID: "deepseek-v4-pro"},
		{Provider: anthropicProvider, ID: "claude-opus-5"},
	}

	choice, err := matchModel("opencode/deepseek", choices)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if choice.ID != "deepseek-v4-pro" {
		t.Errorf("got %s", choice.ID)
	}
}

func TestAQueryReadTheSameWayBySeveralModelsIsAmbiguous(t *testing.T) {
	_, err := matchModel("claude-opus", listedModels())
	if err == nil {
		t.Fatal("expected a query opening three model names to be refused")
	}

	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("expected the ambiguity to be named, got %v", err)
	}
}

func TestAModelOffersOnlyTheEffortLevelsItTakes(t *testing.T) {
	useCachedModels(t)

	wanted := map[string][]string{
		"gpt-5.6-sol":     {"none", "low", "medium", "high", "xhigh", "max"},
		"deepseek-v4-pro": {"high", "max"},
		"claude-opus-5":   {"low", "medium", "high", "xhigh", "max"},
	}

	for _, choice := range Choices(modelCachePath()) {
		want, isKnown := wanted[choice.ID]
		if !isKnown {
			t.Errorf("no effort levels pinned for %s", choice.ID)

			continue
		}

		if !slices.Equal(choice.EffortLevels, want) {
			t.Errorf("expected %s to take %v, got %v", choice.ID, want, choice.EffortLevels)
		}
	}
}

func TestAnEffortTheModelDoesNotTakeIsRefused(t *testing.T) {
	useCachedModels(t)

	if _, _, _, err := parseModelSelection("sol@minimal"); err == nil {
		t.Error("expected an effort gpt-5.6-sol does not take to be refused before the turn")
	}
}

func TestOffAsksForNone(t *testing.T) {
	useCachedModels(t)

	for _, selection := range []string{"sol@off", "sol@of", "sol@o", "sol@none", "sol@n"} {
		_, _, effort, err := parseModelSelection(selection)
		if err != nil {
			t.Errorf("%s: %v", selection, err)

			continue
		}

		if effort != "none" {
			t.Errorf("expected %s to ask for none, got %q", selection, effort)
		}
	}
}

func TestOffIsNotOfferedToAModelThatCannotStopReasoning(t *testing.T) {
	useCachedModels(t)

	for _, selection := range []string{"opus@off", "deepseek@off"} {
		if _, _, _, err := parseModelSelection(selection); err == nil {
			t.Errorf("expected %s to be refused, since that model takes no none level", selection)
		}
	}
}

func TestAmbiguousModelSelectionShowsEveryMatch(t *testing.T) {
	choices := []Choice{
		{Provider: opencodeGoProvider, ID: "deepseek-v4", EffortLevels: []string{"high"}},
		{Provider: opencodeGoProvider, ID: "deepseek-v4-pro", EffortLevels: []string{"high"}},
	}

	_, err := matchModel("deepseek", choices)
	if err == nil {
		t.Fatal("expected an ambiguous model to be rejected")
	}
	for _, match := range []string{"opencode-go/deepseek-v4", "opencode-go/deepseek-v4-pro"} {
		if !strings.Contains(err.Error(), match) {
			t.Errorf("error does not show %q: %v", match, err)
		}
	}
}

func TestAQueryScatteredThroughANameIsReadOnlyWhenGuessingIsAllowed(t *testing.T) {
	if matches := RankedChoices("gp56", listedModels()); len(matches) != 1 {
		t.Errorf("expected a loose reading to find the model, got %v", matches)
	}

	if matches := RankedChoicesWithoutGuessing("gp56", listedModels()); matches != nil {
		t.Errorf("expected the scattered query to name nothing, got %v", matches)
	}

	if matches := RankedChoicesWithoutGuessing("sonnet", listedModels()); len(matches) != 1 {
		t.Errorf("expected a query held within a name to still be read, got %v", matches)
	}
}

func TestExactModelSelectionWinsOverFuzzyMatches(t *testing.T) {
	choices := []Choice{
		{Provider: opencodeGoProvider, ID: "deepseek-v4", EffortLevels: []string{"high"}},
		{Provider: opencodeGoProvider, ID: "deepseek-v4-pro", EffortLevels: []string{"high"}},
	}

	choice, err := matchModel("deepseek-v4", choices)
	if err != nil {
		t.Fatal(err)
	}
	if choice.ID != "deepseek-v4" {
		t.Errorf("got model %q", choice.ID)
	}
}

func TestEveryListedPriceIsReadFromTheCacheEvenWhenTheModelCannotBeChosen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	prices := agent.TokenPrices{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75}
	err := StoreSimulated(path, AnthropicProvider, []agent.Model{
		{ID: "claude-sonnet-4-6", Prices: &prices},
		{ID: "claude-haiku-4-6"},
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []PricedModel{{Provider: AnthropicProvider, ID: "claude-sonnet-4-6", Prices: prices}}
	if got := ListedPrices(path); !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
	if ListedPrices(filepath.Join(t.TempDir(), "absent.json")) != nil {
		t.Error("an absent model cache should quote no prices")
	}
}
