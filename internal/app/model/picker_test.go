package model

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"crdx.org/io/internal/app/model/picker"
	"crdx.org/io/internal/money"
	"crdx.org/io/pkg/agent"
)

func TestTheEffortsOfferedRunFromLeastToMost(t *testing.T) {
	got := orderedEfforts([]string{"high", "none", "medium", "low"})
	want := []string{"none", "low", "medium", "high"}

	if !slices.Equal(got, want) {
		t.Errorf("expected the efforts in order, got %v", got)
	}
}

func TestAModelIsOfferedAtTheEffortNearestTheOneWanted(t *testing.T) {
	offers := offered([]Choice{
		{Provider: "codex", ID: "gpt-5.3-codex", EffortLevels: []string{"xhigh", "low", "medium"}},
		{Provider: "ollama", ID: "qwen3-coder:30b", EffortLevels: []string{"none"}},
		{Provider: "ollama", ID: "unlevelled", EffortLevels: []string{"whatever"}},
	}, Defaults{Effort: "high"})

	if offers[0].Effort != (picker.Effort{Level: "xhigh"}) {
		t.Errorf("expected the nearest effort above the one wanted, got %s", offers[0].Effort)
	}
	if offers[1].Effort != (picker.Effort{Level: "none"}) {
		t.Errorf("expected the only effort there is, got %s", offers[1].Effort)
	}
	if offers[2].Effort != (picker.Effort{Level: "whatever"}) {
		t.Errorf("expected an unrecognised effort to be offered as it stands, got %s", offers[2].Effort)
	}
	if offers[0].Name != "Codex 5.3" || offers[0].ID != "gpt-5.3-codex" {
		t.Errorf("expected the model to be named for a person, got %q of %q", offers[0].Name, offers[0].ID)
	}
	if offers[0].Provider != "Codex" || offers[0].ProviderID != "codex" {
		t.Errorf("expected the provider named for a person, got %q of %q", offers[0].Provider, offers[0].ProviderID)
	}
}

func TestOnlyAProviderWithFastModeOffersItBesideEachEffort(t *testing.T) {
	offers := offered([]Choice{
		{Provider: CodexProvider, ID: "gpt-5.6-sol", EffortLevels: []string{"low", "high"}},
		{Provider: AnthropicProvider, ID: "claude-opus-5", EffortLevels: []string{"low", "high"}},
	}, Defaults{Effort: "high"})

	fastLadder := []picker.Effort{
		{Level: "low"},
		{Level: "low", IsFast: true},
		{Level: "high"},
		{Level: "high", IsFast: true},
	}
	if !slices.Equal(offers[0].EffortLevels, fastLadder) {
		t.Errorf("expected a fast step beside each effort, got %v", offers[0].EffortLevels)
	}

	if !slices.Equal(offers[1].EffortLevels, []picker.Effort{{Level: "low"}, {Level: "high"}}) {
		t.Errorf("expected the efforts alone, got %v", offers[1].EffortLevels)
	}
}

func TestThePickerOnlyOpensWhereItCanBeDrawn(t *testing.T) {
	_, err := ChooseWhenNoneSelected(ErrNoSelection, t.TempDir()+"/models.json", money.Dollar(), everyoneSignedIn, nil, nil, Defaults{})

	if !errors.Is(err, ErrNoSelection) {
		t.Errorf("expected the reason nothing was selected to stand, got %v", err)
	}
}

func TestOnlyAnUnselectedModelOpensThePicker(t *testing.T) {
	wanted := errors.New("something else went wrong")

	_, err := ChooseWhenNoneSelected(wanted, t.TempDir()+"/models.json", money.Dollar(), everyoneSignedIn, os.Stdin, os.Stdout, Defaults{})

	if !errors.Is(err, wanted) {
		t.Errorf("expected the original reason back, got %v", err)
	}
}

func TestNothingCanBeChosenWhenNoModelsAreKnown(t *testing.T) {
	if _, err := Choose(t.TempDir()+"/models.json", money.Dollar(), everyoneSignedIn, nil, nil, Defaults{}); err == nil {
		t.Error("expected the empty model list to be refused")
	}
}

func TestThePickerRefusesToOpenWhereNoProviderIsSignedIntoAtAll(t *testing.T) {
	path := writeChoosableModels(t)

	_, err := Choose(path, money.Dollar(), func(string) bool { return false }, nil, nil, Defaults{})
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("expected the picker to advise signing in, got %v", err)
	}
}

func TestOnlyTheModelsOfASignedIntoProviderAreOffered(t *testing.T) {
	choices := signedInto([]Choice{
		{Provider: CodexProvider, ID: "gpt-5.6-sol"},
		{Provider: AnthropicProvider, ID: "claude-opus-5"},
		{Provider: OllamaProvider, ID: "qwen3.8:27b"},
	}, func(providerName string) bool { return providerName != AnthropicProvider })

	offeredNames := make([]string, len(choices))
	for i, choice := range choices {
		offeredNames[i] = choice.Provider + "/" + choice.ID
	}

	want := []string{"codex/gpt-5.6-sol", "ollama/qwen3.8:27b"}
	if !slices.Equal(offeredNames, want) {
		t.Errorf("got %v, want %v", offeredNames, want)
	}
}

func everyoneSignedIn(string) bool {
	return true
}

func writeChoosableModels(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "models.json")
	cache := modelCache{
		Version:   cacheVersion,
		CheckedAt: time.Now(),
		Providers: map[string]cachedModels{
			CodexProvider: {Models: []agent.Model{
				{ID: "gpt-5.6-sol", EffortLevels: []string{"high"}, MaxOutputTokens: 128_000},
			}},
		},
	}
	if err := saveModelCache(path, cache); err != nil {
		t.Fatal(err)
	}

	return path
}
