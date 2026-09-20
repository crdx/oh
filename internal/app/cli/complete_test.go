package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/pkg/session"
)

func writeStoredSession(t *testing.T, directory string, workspaceDir string, name string, started string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(directory, name), 0o700); err != nil {
		t.Fatal(err)
	}

	head := fmt.Sprintf(`{"kind":"head","time":%q,"id":%q,"name":%q}`+"\n", started, name, name)
	if err := os.WriteFile(filepath.Join(directory, name, "session.jsonl"), []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}

	meta := fmt.Sprintf(
		`{"version":%d,"name":%q,"data":{"workspaceDir":%q},"started":%q,"touched":%q}`+"\n",
		session.MetaFormat, name, workspaceDir, started, started,
	)
	if err := os.WriteFile(filepath.Join(directory, name, "meta.json"), []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCompletionRequestReadsTheKindAndWord(t *testing.T) {
	kind, word, wanted := completionRequest([]string{"--complete", "model", "gpt"})
	if !wanted || kind != "model" || word != "gpt" {
		t.Errorf("got %q %q %v", kind, word, wanted)
	}

	kind, word, wanted = completionRequest([]string{"--complete", "option"})
	if !wanted || kind != "option" || word != "" {
		t.Errorf("got %q %q %v", kind, word, wanted)
	}

	if _, _, wanted := completionRequest([]string{"--complete"}); wanted {
		t.Error("expected a bare --complete to ask for nothing")
	}

	if _, _, wanted := completionRequest([]string{"-r", "perky-jaguar"}); wanted {
		t.Error("expected ordinary arguments to ask for nothing")
	}
}

func TestOptionCompletionsComeFromTheUsage(t *testing.T) {
	options := usageOptions(usage)

	for _, wanted := range []string{"-L", "--login", "-r", "--resume", "-m", "--model", "-t", "--tool", "-l", "--list", "-h", "--help", "-U", "--usage", "-J", "--json"} {
		if !slices.Contains(options, wanted) {
			t.Errorf("expected %q among %v", wanted, options)
		}
	}

	for _, unwanted := range []string{"-", "--add", "--complete", "--from", "--sessions", "Options:"} {
		if slices.Contains(options, unwanted) {
			t.Errorf("did not expect %q among %v", unwanted, options)
		}
	}
}

func TestProviderCompletionsUseTheLoginNames(t *testing.T) {
	got := completions(completeProvider, "op", Sources{})
	if !slices.Equal(got, []string{model.OpencodeGoProvider}) {
		t.Errorf("got %v", got)
	}
}

func TestNothingTypedOffersTheLongOptions(t *testing.T) {
	options := []string{"--caps", "--help", "-c", "-h"}

	if got := optionCompletions("", options); !slices.Equal(got, []string{"--caps", "--help"}) {
		t.Errorf("got %v", got)
	}

	if got := optionCompletions("-", options); !slices.Equal(got, options) {
		t.Errorf("got %v", got)
	}

	if got := optionCompletions("-c", options); !slices.Equal(got, []string{"-c"}) {
		t.Errorf("got %v", got)
	}
}

func TestModelCompletionsAreWholeSelections(t *testing.T) {
	choices := []model.Choice{
		{Provider: "openai", ID: "gpt-5", EffortLevels: []string{"low", "high"}},
		{Provider: "anthropic", ID: "claude-sonnet-5", EffortLevels: []string{"none", "high"}},
	}

	selections := modelCompletions("sonnet", choices)
	if len(selections) != 2 || selections[0] != "anthropic/claude-sonnet-5@none" {
		t.Errorf("got %v", selections)
	}

	selections = modelCompletions("gpt-5@h", choices)
	if len(selections) != 1 || selections[0] != "openai/gpt-5@high" {
		t.Errorf("got %v", selections)
	}

	if selections := modelCompletions("", choices); len(selections) != 4 {
		t.Errorf("expected every selection, got %v", selections)
	}
}

func TestFastModeCompletionsReachOnlyCodex(t *testing.T) {
	choices := []model.Choice{
		{Provider: model.CodexProvider, ID: "gpt-5.6-sol", EffortLevels: []string{"high"}},
		{Provider: model.AnthropicProvider, ID: "claude-opus-5", EffortLevels: []string{"high"}},
	}

	if selections := modelCompletions("sol@high+f", choices); !slices.Equal(
		selections,
		[]string{"codex/gpt-5.6-sol@high+fast"},
	) {
		t.Errorf("got %v", selections)
	}
	if selections := modelCompletions("opus@high+f", choices); len(selections) != 0 {
		t.Errorf("got %v", selections)
	}
}

func TestEffortCompletionsAreBareLevels(t *testing.T) {
	choices := []model.Choice{
		{Provider: "openai", ID: "gpt-5", EffortLevels: []string{"low", "high"}},
		{Provider: "anthropic", ID: "claude-sonnet-5", EffortLevels: []string{"none", "high"}},
	}

	if efforts := effortCompletions("sonnet@", choices); !slices.Equal(efforts, []string{"none", "high"}) {
		t.Errorf("got %v", efforts)
	}

	if efforts := effortCompletions("sonnet@h", choices); !slices.Equal(efforts, []string{"high"}) {
		t.Errorf("got %v", efforts)
	}

	if efforts := effortCompletions("sonnet@of", choices); !slices.Equal(efforts, []string{"none"}) {
		t.Errorf("got %v", efforts)
	}

	if efforts := effortCompletions("zzz@", choices); len(efforts) != 0 {
		t.Errorf("expected no efforts for an unmatched model, got %v", efforts)
	}
}

func TestCapabilityCompletionsGrowOneAtATime(t *testing.T) {
	sets := capsCompletions()
	if sets[0] != "r" || sets[len(sets)-1] != "rxwngl" {
		t.Errorf("got %v", sets)
	}
}

func TestToolCompletionsComeFromTheRuntime(t *testing.T) {
	got, isWanted := Complete([]string{"--complete", completeTool, "g"}, Sources{ToolNames: []string{"read", "grep"}})
	if !isWanted || !slices.Equal(got, []string{"grep"}) {
		t.Errorf("got %v, wanted %v", got, isWanted)
	}
}

func TestWritingCompletionsLinesThemUp(t *testing.T) {
	var out bytes.Buffer
	WriteCompletions(&out, []string{"--complete", completeCaps, "rxw"}, Sources{})

	if out.String() != "rxw\nrxwn\nrxwng\nrxwngl\n" {
		t.Errorf("got %q", out.String())
	}

	out.Reset()
	WriteCompletions(&out, []string{"--complete", "nonsense", ""}, Sources{})
	if out.Len() != 0 {
		t.Errorf("got %q", out.String())
	}
}

func TestSessionCompletionsNameTheWorkspaceNewestFirst(t *testing.T) {
	directory := t.TempDir()
	workspaceDir := t.TempDir()
	t.Chdir(workspaceDir)
	writeStoredSession(t, directory, workspaceDir, "older-badger", "2024-01-01T00:00:00Z")
	writeStoredSession(t, directory, workspaceDir, "newer-jaguar", "2025-01-01T00:00:00Z")
	writeStoredSession(t, directory, t.TempDir(), "elsewhere-otter", "2026-01-01T00:00:00Z")

	names := sessionNames(directory)
	if !slices.Equal(names, []string{"newer-jaguar", "older-badger"}) {
		t.Errorf("got %v", names)
	}

	if names := withPrefix("old", names); !slices.Equal(names, []string{"older-badger"}) {
		t.Errorf("got %v", names)
	}

	if names := sessionNames(filepath.Join(directory, "missing")); len(names) > 0 {
		t.Errorf("expected no names for a missing directory, got %v", names)
	}
}
