package model

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestRoundRobinSelectionsResolveEveryEntry(t *testing.T) {
	useCachedModels(t)

	selections, err := ParseRoundRobin(modelCachePath(), []string{
		"opencode/deepseek@hi",
		"anth/opus-5@max",
	}, Defaults{})
	if err != nil {
		t.Fatal(err)
	}

	wanted := []string{
		"opencode-go/deepseek-v4-pro@high",
		"anthropic/claude-opus-5@max",
	}
	for i, selection := range selections {
		if selection.String() != wanted[i] {
			t.Errorf("selection %d is %s, want %s", i, selection, wanted[i])
		}
	}
}

func TestRoundRobinSelectionsKeepCanonicalDuplicates(t *testing.T) {
	useCachedModels(t)

	selections, err := ParseRoundRobin(modelCachePath(), []string{
		"opencode-go/deepseek-v4-pro@high",
		"opencode/deepseek@hi",
	}, Defaults{})
	if err != nil {
		t.Fatal(err)
	}
	if len(selections) != 2 || selections[0] != selections[1] {
		t.Errorf("got %v, want the same selection twice", selections)
	}
}

func TestRepeatedRoundRobinSelectionsWeightTheRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "round-robin.json")
	frequent := Selection{Provider: CodexProvider, Model: "frequent", Effort: "high"}
	occasional := Selection{Provider: AnthropicProvider, Model: "occasional", Effort: "high"}
	selections := []Selection{frequent, frequent, occasional}

	var selected []Selection
	for range len(selections) * 2 {
		selection, err := ReserveRoundRobin(path, selections)
		if err != nil {
			t.Fatal(err)
		}
		selected = append(selected, selection)
	}

	want := []Selection{frequent, frequent, occasional, frequent, frequent, occasional}
	if !slices.Equal(selected, want) {
		t.Errorf("got %v, want %v", selected, want)
	}
}

func TestUnavailableSelectionsAreSkippedWithoutLosingTheirWeight(t *testing.T) {
	path := filepath.Join(t.TempDir(), "round-robin.json")
	frequent := Selection{Provider: CodexProvider, Model: "frequent", Effort: "high"}
	available := Selection{Provider: AnthropicProvider, Model: "available", Effort: "high"}
	selections := []Selection{frequent, frequent, available}
	isAvailable := func(selection Selection) bool { return selection == available }

	for range 2 {
		selected, err := ReserveAvailableRoundRobin(path, selections, isAvailable)
		if err != nil {
			t.Fatal(err)
		}
		if selected != available {
			t.Errorf("got %s, want %s", selected, available)
		}
	}

	selected, err := ReserveAvailableRoundRobin(path, selections, func(Selection) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if selected != frequent {
		t.Errorf("got %s, want %s after recovery", selected, frequent)
	}
}

func TestARotationWithNothingAvailableKeepsItsOrdinaryPosition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "round-robin.json")
	selections := []Selection{
		{Provider: CodexProvider, Model: "first", Effort: "high"},
		{Provider: AnthropicProvider, Model: "second", Effort: "high"},
	}

	for i := range selections {
		selected, err := ReserveAvailableRoundRobin(path, selections, func(Selection) bool { return false })
		if err != nil {
			t.Fatal(err)
		}
		if selected != selections[i] {
			t.Errorf("got %s, want %s", selected, selections[i])
		}
	}
}

func TestNormalAndFastRoundRobinSelectionsAreDistinct(t *testing.T) {
	useCachedModels(t)

	selections, err := ParseRoundRobin(modelCachePath(), []string{"sol@high", "sol@high+fast"}, Defaults{})
	if err != nil {
		t.Fatal(err)
	}
	if len(selections) != 2 || selections[0].IsFast || !selections[1].IsFast {
		t.Errorf("got %v", selections)
	}
}

func TestOneRoundRobinSelectionNeedsNoState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "round-robin.json")
	selection := Selection{Provider: CodexProvider, Model: "only", Effort: "high"}

	selected, err := ReserveRoundRobin(path, []Selection{selection})
	if err != nil {
		t.Fatal(err)
	}
	if selected != selection {
		t.Errorf("got %s, want %s", selected, selection)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no state, got %v", err)
	}
}

func TestConcurrentRoundRobinReservationsShareTheStateLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "round-robin.json")
	selections := []Selection{
		{Provider: CodexProvider, Model: "first", Effort: "high"},
		{Provider: AnthropicProvider, Model: "second", Effort: "high"},
	}
	const calls = 20

	selected := make(chan Selection, calls)
	var wait sync.WaitGroup
	for range calls {
		wait.Go(func() {
			selection, err := ReserveRoundRobin(path, selections)
			if err != nil {
				t.Error(err)
				return
			}
			selected <- selection
		})
	}
	wait.Wait()
	close(selected)

	counts := map[Selection]int{}
	for selection := range selected {
		counts[selection]++
	}
	for _, selection := range selections {
		if counts[selection] != calls/len(selections) {
			t.Errorf("selected %s %d times", selection, counts[selection])
		}
	}
}

func TestRoundRobinSelectionsValidateEntriesThatAreNotFirst(t *testing.T) {
	useCachedModels(t)

	_, err := ParseRoundRobin(modelCachePath(), []string{
		"sol@high",
		"nothing-like-this@high",
	}, Defaults{})
	if err == nil || !strings.Contains(err.Error(), "nothing-like-this") {
		t.Fatalf("expected the invalid selection to be named, got %v", err)
	}
}

func TestRoundRobinStateFromANewerOhIsRefusedBeforeItsShapeIsRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rotation.json")
	stored := `{"version":99,"last":"codex/gpt@high"}`
	if err := os.WriteFile(path, []byte(stored), 0o600); err != nil {
		t.Fatal(err)
	}

	selections := []Selection{
		{Provider: "codex", Model: "gpt", Effort: "high"},
		{Provider: "anthropic", Model: "opus", Effort: "high"},
	}

	_, err := ReserveRoundRobin(path, selections)
	if err == nil || !strings.Contains(err.Error(), "newer build") {
		t.Fatalf("expected the newer state to be named as one, got %v", err)
	}
}
