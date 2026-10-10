package model

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/state"
	"crdx.org/oh/pkg/agent"
)

func readSeenModels(t *testing.T) map[string][]agent.Model {
	t.Helper()

	var record seenModels
	if err := state.Read(seenModelsPath(), SeenModelsFormat, &record); err != nil {
		t.Fatal(err)
	}
	if record.Version != SeenModelsFormat {
		t.Errorf("expected the record to carry format %d, got %d", SeenModelsFormat, record.Version)
	}

	return record.Providers
}

func seenIDs(models []agent.Model) []string {
	ids := make([]string, len(models))
	for i, model := range models {
		ids[i] = model.ID
	}

	return ids
}

func updateListing(t *testing.T, listings ...map[string][]agent.Model) {
	t.Helper()

	endpoint := serveRegistry(t, oneCodexModel)

	for _, listed := range listings {
		var output bytes.Buffer
		if err := Update(&output, endpoint, modelCachePath(), seenModelsPath(), listingModels(listed), false); err != nil {
			t.Fatalf("unexpected error: %v, output %q", err, output.String())
		}
	}
}

func TestAnUpdateRemembersEveryModelItSawIncludingTheOnesItIgnored(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	updateListing(t, ignoredModelListings())

	seen := readSeenModels(t)
	for providerName, listed := range ignoredModelListings() {
		want := slices.DeleteFunc(seenIDs(listed), func(id string) bool { return id == "" })
		if got := seenIDs(seen[providerName]); !slices.Equal(got, want) {
			t.Errorf("%s: expected every identified model to be seen, got %v, want %v", providerName, got, want)
		}
	}
}

func TestASeenModelIsNeverForgotten(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	updateListing(t, firstListings(), secondListings())

	want := []string{"claude-sonnet-4-6", "claude-opus-4-6", "claude-fable-5-1"}
	if got := seenIDs(readSeenModels(t)[AnthropicProvider]); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestASeenModelTakesItsNewestDescriptionAndKeepsWhatThatLeavesOut(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	prices := agent.TokenPrices{Input: 5, Output: 25}
	updateListing(t,
		map[string][]agent.Model{AnthropicProvider: {
			{ID: "claude-opus-5", Name: "Opus 5", EffortLevels: []string{"high"}, MaxOutputTokens: 64_000, Prices: &prices},
		}},
		map[string][]agent.Model{AnthropicProvider: {
			{ID: "claude-opus-5", Name: "Claude Opus 5", EffortLevels: []string{"high", "max"}},
		}},
	)

	seen := readSeenModels(t)[AnthropicProvider]
	if len(seen) != 1 {
		t.Fatalf("expected one model, got %v", seen)
	}

	model := seen[0]
	if model.Name != "Claude Opus 5" || !slices.Equal(model.EffortLevels, []string{"high", "max"}) {
		t.Errorf("expected the newest description, got %+v", model)
	}
	if model.MaxOutputTokens != 64_000 || model.Prices == nil || *model.Prices != prices {
		t.Errorf("expected what the newest description left out to be kept, got %+v", model)
	}
}

func TestAnUpdateThatRecordsNoModelListStillRemembersWhatItSaw(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	listed := map[string][]agent.Model{AnthropicProvider: {{ID: "claude-opus-4-5", EffortLevels: []string{"high"}}}}

	var output bytes.Buffer
	if err := Update(&output, deadAddress, modelCachePath(), seenModelsPath(), listingModels(listed), false); err == nil {
		t.Fatalf("expected an update that recorded no model list to fail, got %q", output.String())
	}

	if got := seenIDs(readSeenModels(t)[AnthropicProvider]); !slices.Equal(got, []string{"claude-opus-4-5"}) {
		t.Errorf("got %v", got)
	}
}

func TestAModelOnlySeenIsChosenButNeverOffered(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	updateListing(t, map[string][]agent.Model{AnthropicProvider: {
		{ID: "claude-opus-5", EffortLevels: []string{"high"}, MaxOutputTokens: 128_000},
		{ID: "claude-opus-5-1", EffortLevels: []string{"high"}, MaxOutputTokens: 256_000},
	}})

	choice, err := chosenAnthropicModel("claude-opus-5")
	if err != nil {
		t.Fatalf("expected a superseded model to be chosen from what was seen, got %v", err)
	}
	if choice.MaxOutputTokens != 128_000 {
		t.Errorf("expected the seen description, got %+v", choice)
	}

	for _, offered := range Choices(modelCachePath()) {
		if offered.ID == "claude-opus-5" {
			t.Errorf("expected a superseded model to be offered to nobody, got %+v", offered)
		}
	}
}

func TestTheModelListOutranksWhatWasSeen(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	if err := recordSeenModels(seenModelsPath(), map[string][]agent.Model{AnthropicProvider: {
		{ID: "claude-opus-5", EffortLevels: []string{"high"}, MaxOutputTokens: 32_000},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := saveModelCache(modelCachePath(), modelCache{Providers: map[string]cachedModels{
		anthropicProvider: {Models: []agent.Model{
			{ID: "claude-opus-5", EffortLevels: []string{"high"}, MaxOutputTokens: 128_000},
		}},
	}}); err != nil {
		t.Fatal(err)
	}

	choice, err := chosenAnthropicModel("claude-opus-5")
	if err != nil || choice.MaxOutputTokens != 128_000 {
		t.Errorf("expected the model list's description, got %+v and %v", choice, err)
	}
}

const seenModelsBeforeWires = `{"version":1,"providers":{"opencode-go":[{"id":"qwen3.8-max","efforts":["high"],"output":64000}]}}`

func writeSeenModels(t *testing.T, body string) {
	t.Helper()

	if err := os.WriteFile(seenModelsPath(), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSeenModelsWrittenBeforeWiresAreRefusedUntilMigrated(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeSeenModels(t, seenModelsBeforeWires)

	var output bytes.Buffer
	err := Update(&output, serveRegistry(t, oneCodexModel), modelCachePath(), seenModelsPath(), listingModels(ignoredModelListings()), false)
	if err == nil || !strings.Contains(err.Error(), "run oh --ctl migrate") {
		t.Errorf("expected recording what was seen to ask for a migration, got %v", err)
	}

	data, readError := os.ReadFile(seenModelsPath())
	if readError != nil {
		t.Fatal(readError)
	}
	if string(data) != seenModelsBeforeWires {
		t.Errorf("expected the seen models to be left for the migration, got %s", data)
	}
}

func TestAModelKnownOnlyFromSeenModelsBeforeWiresAsksForAMigration(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeSeenModels(t, seenModelsBeforeWires)

	_, err := Chosen(nil, seenModelsPath(), opencodeGoProvider, "qwen3.8-max")
	if err == nil || !strings.Contains(err.Error(), "run oh --ctl migrate") {
		t.Errorf("expected a migration to be asked for, got %v", err)
	}

	listed := []Choice{{Provider: opencodeGoProvider, ID: "qwen3.8-max", Wire: agent.MessagesWire}}
	if choice, err := Chosen(listed, seenModelsPath(), opencodeGoProvider, "qwen3.8-max"); err != nil || choice.Wire != agent.MessagesWire {
		t.Errorf("expected a listed model to be chosen whatever the seen models hold, got %+v and %v", choice, err)
	}
}

func TestAModelKnownOnlyFromSeenModelsIsChosenOnItsSeenWire(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeSeenModels(t, `{"version":2,"providers":{"opencode-go":[{"id":"qwen3.8-max","efforts":["high"],"output":64000,"wire":"messages"}]}}`)

	choice, err := Chosen(nil, seenModelsPath(), opencodeGoProvider, "qwen3.8-max")
	if err != nil || choice.Wire != agent.MessagesWire {
		t.Errorf("expected the seen model on its seen wire, got %+v and %v", choice, err)
	}
}
