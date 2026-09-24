package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/agent"
)

const (
	goldenCachePath       = "/state/models.json"
	goldenRegistryAddress = `"<registry>"`
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestGoldenAnUpdateWithNothingReachableMatchesTheGolden(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	err := Update(&output, deadAddress, modelCachePath(), seenModelsPath(), unreachableProviders, true)
	if err == nil {
		t.Fatal("expected an update with nothing reachable to fail")
	}

	assertGolden(t, "update-nothing-reachable.ansi", strings.Join([]string{
		report(t, output.String()),
		"=== error ===\n", err.Error(), "\n",
	}, ""))
}

func TestGoldenAnUpdateFromTheRegistryAloneMatchesTheGolden(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	if err := Update(&output, serveRegistry(t, oneCodexModel), modelCachePath(), seenModelsPath(), unreachableProviders, true); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "update-from-registry.ansi", report(t, output.String()))
}

func TestGoldenAnUpdateAProviderListsItselfMatchesTheGolden(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	listedByProvider := map[string][]agent.Model{
		OllamaProvider: {
			{ID: "gpt-5.6-sol", Name: "GPT-5.6 Sol", EffortLevels: []string{"low", "high"}, MaxOutputTokens: 32_000},
			{ID: "an-unselectable-model"},
		},
		CodexProvider: {
			{
				ID:                  "gpt-5.6-sol",
				Name:                "GPT-5.6 Sol",
				EffortLevels:        []string{"low", "medium", "high"},
				ContextWindowTokens: 272_000,
			},
			{
				ID:                  "gpt-5.6-sol-preview",
				Name:                "GPT-5.6 Sol Preview",
				EffortLevels:        []string{"low", "medium", "high"},
				ContextWindowTokens: 272_000,
			},
		},
		AnthropicProvider: nil,
	}

	var output bytes.Buffer
	lister := func(ctx context.Context, providerName string) ([]agent.Model, error) {
		if listed, isFound := listedByProvider[providerName]; isFound {
			return listed, nil
		}

		return unreachableProviders(ctx, providerName)
	}
	if err := Update(&output, serveRegistry(t, oneCodexModel), modelCachePath(), seenModelsPath(), lister, true); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "update-listed-by-provider.ansi", report(t, output.String()))
	assertGolden(t, "update-listed-by-provider.models.json", cachedProviderModels(t, CodexProvider))
}

func cachedProviderModels(t *testing.T, providerName string) string {
	t.Helper()

	models := loadModelCache(modelCachePath()).Providers[providerName].Models
	data, err := json.MarshalIndent(models, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	return string(data) + "\n"
}

func ignoredModelListings() map[string][]agent.Model {
	return map[string][]agent.Model{
		OpencodeGoProvider: {
			{ID: "grok-4.6", EffortLevels: []string{"high"}, MaxOutputTokens: 32_000},
			{ID: "qwen3-max", EffortLevels: []string{"high"}, MaxOutputTokens: 32_000},
		},
		AnthropicProvider: {
			{ID: "claude-sonnet-4-5", EffortLevels: []string{"high"}, MaxOutputTokens: 64_000},
			{ID: "claude-sonnet-4-6", EffortLevels: []string{"high"}, MaxOutputTokens: 64_000},
			{ID: "claude-opus-4-5", EffortLevels: []string{"high"}, MaxOutputTokens: 32_000},
			{ID: "claude-fable-5-20260609", EffortLevels: []string{"high"}, MaxOutputTokens: 128_000},
			{ID: "claude-fable-5-1", EffortLevels: []string{"high"}, MaxOutputTokens: 128_000},
		},
		OllamaProvider: {
			{ID: "llama-3", EffortLevels: []string{"medium"}, MaxOutputTokens: 8_000},
			{ID: "llama-4", EffortLevels: []string{"medium"}},
			{ID: "a-locally-built-model-with-a-very-long-name"},
			{Name: "Nothing Identifies This One"},
			{},
		},
	}
}

func listingModels(listedByProvider map[string][]agent.Model) ProviderLister {
	return func(ctx context.Context, providerName string) ([]agent.Model, error) {
		if listed, isFound := listedByProvider[providerName]; isFound {
			return listed, nil
		}

		return unreachableProviders(ctx, providerName)
	}
}

func listingIgnoredModels(t *testing.T) ProviderLister {
	t.Helper()

	return listingModels(ignoredModelListings())
}

func TestGoldenAnUpdateNamesTheModelsThatCameAndWent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	endpoint := serveRegistry(t, oneCodexModel)

	var firstOutput bytes.Buffer
	if err := Update(&firstOutput, endpoint, modelCachePath(), seenModelsPath(), listingModels(firstListings()), false); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(style.Plain(firstOutput.String()), addedChange) {
		t.Errorf("expected a first update to have nothing to compare with, got %q", firstOutput.String())
	}

	var output bytes.Buffer
	if err := Update(&output, endpoint, modelCachePath(), seenModelsPath(), listingModels(secondListings()), false); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "update-naming-changes.ansi", report(t, output.String()))
}

func firstListings() map[string][]agent.Model {
	return map[string][]agent.Model{
		AnthropicProvider: {
			{ID: "claude-sonnet-4-6", EffortLevels: []string{"high"}, MaxOutputTokens: 64_000},
			{ID: "claude-opus-4-6", EffortLevels: []string{"high"}, MaxOutputTokens: 64_000},
		},
		OllamaProvider: {
			{ID: "llama-4", EffortLevels: []string{"medium"}, MaxOutputTokens: 8_000},
		},
	}
}

func secondListings() map[string][]agent.Model {
	return map[string][]agent.Model{
		AnthropicProvider: {
			{ID: "claude-sonnet-4-6", EffortLevels: []string{"high"}, MaxOutputTokens: 64_000},
			{ID: "claude-fable-5-1", EffortLevels: []string{"high"}, MaxOutputTokens: 128_000},
		},
		OllamaProvider: {
			{ID: "llama-4", EffortLevels: []string{"medium"}, MaxOutputTokens: 8_000},
		},
	}
}

func changedModelListings() map[string][]agent.Model {
	return map[string][]agent.Model{
		OpencodeGoProvider: {
			{ID: "grok-4.6", EffortLevels: []string{"high"}, MaxOutputTokens: 32_000},
			{ID: "qwen3-max", EffortLevels: []string{"high"}, MaxOutputTokens: 32_000},
		},
		AnthropicProvider: {
			{ID: "claude-sonnet-4-6", EffortLevels: []string{"high"}, MaxOutputTokens: 64_000},
			{ID: "claude-sonnet-4-7", EffortLevels: []string{"high"}, MaxOutputTokens: 64_000},
			{ID: "claude-opus-4-5", EffortLevels: []string{"high"}, MaxOutputTokens: 32_000},
		},
		OllamaProvider: {
			{ID: "llama-4", EffortLevels: []string{"medium"}},
			{ID: "mistral-3", EffortLevels: []string{"medium"}, MaxOutputTokens: 8_000},
		},
	}
}

func updateOverStoredModels(t *testing.T, output io.Writer, isShowingIgnored bool) {
	t.Helper()

	endpoint := serveRegistry(t, oneCodexModel)

	var storingOutput bytes.Buffer
	if err := Update(&storingOutput, endpoint, modelCachePath(), seenModelsPath(), listingIgnoredModels(t), false); err != nil {
		t.Fatal(err)
	}

	lister := listingModels(changedModelListings())
	if err := Update(output, endpoint, modelCachePath(), seenModelsPath(), lister, isShowingIgnored); err != nil {
		t.Fatal(err)
	}
}

func TestGoldenAnUpdateDrawsWhatChangedBesideWhatItIgnored(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	updateOverStoredModels(t, &output, true)

	assertGolden(t, "update-changed-and-ignored.ansi", report(t, output.String()))
}

func TestGoldenChangesWithoutColourKeepTheirColumns(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	restoreStyle := style.Init(&output)
	defer restoreStyle()

	updateOverStoredModels(t, &output, false)

	assertGolden(t, "update-changed-plain.txt", report(t, output.String()))
}

func TestGoldenAnUpdateNamesEveryModelItIgnoresAndWhy(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	if err := Update(&output, serveRegistry(t, oneCodexModel), modelCachePath(), seenModelsPath(), listingIgnoredModels(t), true); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "update-ignoring-models.ansi", report(t, output.String()))
}

func TestEveryCountedRowAddsUpToTheModelsTheProviderOffered(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	if err := Update(&output, serveRegistry(t, oneCodexModel), modelCachePath(), seenModelsPath(), listingIgnoredModels(t), true); err != nil {
		t.Fatal(err)
	}

	countedRow := regexp.MustCompile(`^(\S+(?: \S+)*) +(\d+) +(\d+) +(\d+|) `)

	var counted int

	for line := range strings.Lines(style.Plain(output.String())) {
		fields := countedRow.FindStringSubmatch(line)
		if fields == nil {
			continue
		}

		listed, selectable, ignored := number(t, fields[2]), number(t, fields[3]), number(t, fields[4])
		if listed != selectable+ignored {
			t.Errorf(
				"%s lists %d models, of which %d are selectable and %d ignored",
				fields[1], listed, selectable, ignored,
			)
		}

		counted++
	}

	if counted == 0 {
		t.Fatal("expected at least one counted row")
	}
}

func number(t *testing.T, field string) int {
	t.Helper()

	if field == "" {
		return 0
	}

	value, err := strconv.Atoi(field)
	if err != nil {
		t.Fatal(err)
	}

	return value
}

func TestGoldenAnUpdateWithoutTheFlagCountsWhatItIgnoredAndSaysHowToSeeIt(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	if err := Update(&output, serveRegistry(t, oneCodexModel), modelCachePath(), seenModelsPath(), listingIgnoredModels(t), false); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "update-counting-ignored.ansi", report(t, output.String()))
}

func TestGoldenAnUpdateWithoutColourKeepsItsColumns(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	restoreStyle := style.Init(&output)
	defer restoreStyle()

	if err := Update(&output, serveRegistry(t, oneCodexModel), modelCachePath(), seenModelsPath(), listingIgnoredModels(t), true); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "update-ignoring-models-plain.txt", report(t, output.String()))
}

func TestGoldenAProviderWhoseModelsAreAllIgnoredSaysSoAndOneIgnoredModelReadsAsOne(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	lister := func(ctx context.Context, providerName string) ([]agent.Model, error) {
		if providerName == AnthropicProvider {
			return []agent.Model{{ID: "claude-opus-4-5", EffortLevels: []string{"high"}}}, nil
		}

		return unreachableProviders(ctx, providerName)
	}
	if err := Update(&output, serveRegistry(t, oneCodexModel), modelCachePath(), seenModelsPath(), lister, false); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "update-ignoring-every-model.ansi", report(t, output.String()))
}

func TestGoldenAnUpdateThatRecordsNothingStillNamesWhatItIgnored(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var output bytes.Buffer
	lister := func(ctx context.Context, providerName string) ([]agent.Model, error) {
		if providerName == OpencodeGoProvider {
			return []agent.Model{
				{ID: "grok-4.6", EffortLevels: []string{"high"}, MaxOutputTokens: 32_000},
				{ID: "minimax-m2", EffortLevels: []string{"high"}, MaxOutputTokens: 32_000},
			}, nil
		}

		return unreachableProviders(ctx, providerName)
	}

	err := Update(&output, deadAddress, modelCachePath(), seenModelsPath(), lister, true)
	if err == nil {
		t.Fatal("expected an update that recorded nothing to fail")
	}

	assertGolden(t, "update-ignoring-everything.ansi", strings.Join([]string{
		report(t, output.String()),
		"=== error ===\n", err.Error(), "\n",
	}, ""))
}

func TestGoldenAStartupRefreshThatRecordsNothingShowsWhatItIgnored(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeCheckedModelCache(t, time.Now().Add(-8*24*time.Hour))

	var output bytes.Buffer
	lister := func(ctx context.Context, providerName string) ([]agent.Model, error) {
		if providerName == OpencodeGoProvider {
			return []agent.Model{
				{ID: "grok-4.6", EffortLevels: []string{"high"}, MaxOutputTokens: 32_000},
				{ID: "minimax-m2", EffortLevels: []string{"high"}, MaxOutputTokens: 32_000},
			}, nil
		}

		return unreachableProviders(ctx, providerName)
	}
	if err := Ensure(&output, deadAddress, modelCachePath(), seenModelsPath(), lister); err != nil {
		t.Fatalf("expected a failed refresh to be forgiven, got %v", err)
	}

	assertGolden(t, "ensure-refresh-ignoring-every-model.ansi", report(t, output.String()))
}

func TestGoldenAStartupRefreshNamesWhatChangedAndNothingElse(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	endpoint := serveRegistry(t, oneCodexModel)

	var storingOutput bytes.Buffer
	if err := Update(&storingOutput, endpoint, modelCachePath(), seenModelsPath(), listingModels(firstListings()), false); err != nil {
		t.Fatal(err)
	}

	ageModelCache(t, time.Now().Add(-8*24*time.Hour))

	var output bytes.Buffer
	if err := Ensure(&output, endpoint, modelCachePath(), seenModelsPath(), listingModels(secondListings())); err != nil {
		t.Fatalf("expected the refresh to succeed, got %v", err)
	}

	assertGolden(t, "ensure-refresh-naming-changes.ansi", report(t, output.String()))
}

func ageModelCache(t *testing.T, checked time.Time) {
	t.Helper()

	cache := loadModelCache(modelCachePath())
	cache.CheckedAt = checked

	if err := saveModelCache(modelCachePath(), cache); err != nil {
		t.Fatal(err)
	}
}

func unreachableProviders(_ context.Context, providerName string) ([]agent.Model, error) {
	switch providerName {
	case CodexProvider:
		return nil, agent.ErrNoListing
	case OpencodeGoProvider:
		return nil, errors.New("not logged in to OpenCode Go: run the login command with opencode-go")
	case AnthropicProvider:
		return nil, errors.New("not logged in to Anthropic: run the login command with anthropic")
	default:
		return nil, errors.New(`Get "http://localhost:11434/api/tags": connect: connection refused`)
	}
}

var registryAddressPattern = regexp.MustCompile(`"[^"]*` + regexp.QuoteMeta(simulatedRegistryPath) + `[^"]*"`)

func report(t *testing.T, drawn string) string {
	t.Helper()

	drawn = strings.ReplaceAll(drawn, modelCachePath(), goldenCachePath)
	drawn = registryAddressPattern.ReplaceAllString(drawn, goldenRegistryAddress)

	return strutil.VisibleEscapes(drawn)
}

func assertGolden(t *testing.T, name string, drawn string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(drawn), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if drawn != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}
