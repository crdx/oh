package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/util/strutil"
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
	options := strutil.OptionNames(usage)

	for _, wanted := range []string{"-L", "--login", "-r", "--resume", "-f", "--force", "--from", "-m", "--model", "-t", "--tool", "-l", "--list", "-h", "--help", "-U", "--usage", "-J", "--json"} {
		if !slices.Contains(options, wanted) {
			t.Errorf("expected %q among %v", wanted, options)
		}
	}

	for _, unwanted := range []string{"-", "--add", "--complete", "--sessions", "Options:"} {
		if slices.Contains(options, unwanted) {
			t.Errorf("did not expect %q among %v", unwanted, options)
		}
	}
}

func TestEveryOptionDescriptionIsTwentyThreeCharacters(t *testing.T) {
	for _, option := range strutil.OptionNames(usage) {
		description := OptionDescription(option)
		if len(description) != 23 {
			t.Errorf("expected %s to be described in 23 characters, got %d %q", option, len(description), description)
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

	selections := model.SelectionsMatching("sonnet", choices)
	if len(selections) != 2 || selections[0] != "anthropic/claude-sonnet-5@none" {
		t.Errorf("got %v", selections)
	}

	selections = model.SelectionsMatching("gpt-5@h", choices)
	if len(selections) != 1 || selections[0] != "openai/gpt-5@high" {
		t.Errorf("got %v", selections)
	}

	if selections := model.SelectionsMatching("", choices); len(selections) != 4 {
		t.Errorf("expected every selection, got %v", selections)
	}
}

func TestAProviderNameBeingTypedCompletesItsModels(t *testing.T) {
	choices := []model.Choice{
		{Provider: model.CodexProvider, ID: "gpt-5.6-sol", EffortLevels: []string{"high"}},
		{Provider: model.OpencodeGoProvider, ID: "minimax-m3"},
		{Provider: model.OpencodeGoProvider, ID: "kimi-k3", EffortLevels: []string{"high"}},
	}

	for _, word := range []string{"opencode", "OpenCode-", "opencode-go/"} {
		if selections := model.SelectionsMatching(word, choices); !slices.Equal(
			selections,
			[]string{"opencode-go/minimax-m3", "opencode-go/kimi-k3@high"},
		) {
			t.Errorf("%s: got %v", word, selections)
		}
	}
	if selections := model.SelectionsMatching("min", choices); !slices.Equal(selections, []string{"opencode-go/minimax-m3"}) {
		t.Errorf("a model name should outrank a provider name, got %v", selections)
	}
	if selections := model.SelectionsMatching("opencode@h", choices); !slices.Equal(selections, []string{"opencode-go/kimi-k3@high"}) {
		t.Errorf("an effort should narrow a provider's models, got %v", selections)
	}
	if selections := model.SelectionsMatching("zz", choices); len(selections) != 0 {
		t.Errorf("got %v", selections)
	}
}

func TestAModelTakingNoEffortCompletesWithoutOne(t *testing.T) {
	choices := []model.Choice{
		{Provider: model.OpencodeGoProvider, ID: "minimax-m3"},
		{Provider: model.OpencodeGoProvider, ID: "minimax-m2", EffortLevels: []string{"high"}},
	}

	if selections := model.SelectionsMatching("minimax", choices); !slices.Equal(
		selections,
		[]string{"opencode-go/minimax-m3", "opencode-go/minimax-m2@high"},
	) {
		t.Errorf("got %v", selections)
	}
	for _, word := range []string{"minimax-m3@", "minimax-m3@h", "minimax-m3+f"} {
		if selections := model.SelectionsMatching(word, choices[:1]); len(selections) != 0 {
			t.Errorf("%s: expected nothing to complete, got %v", word, selections)
		}
	}
	if efforts := effortCompletions("minimax-m3@", choices[:1]); len(efforts) != 0 {
		t.Errorf("expected no effort to complete, got %v", efforts)
	}
}

func TestFastModeCompletionsReachOnlyCodex(t *testing.T) {
	choices := []model.Choice{
		{Provider: model.CodexProvider, ID: "gpt-5.6-sol", EffortLevels: []string{"high"}},
		{Provider: model.AnthropicProvider, ID: "claude-opus-5", EffortLevels: []string{"high"}},
	}

	if selections := model.SelectionsMatching("sol@high+f", choices); !slices.Equal(
		selections,
		[]string{"codex/gpt-5.6-sol@high+fast"},
	) {
		t.Errorf("got %v", selections)
	}
	if selections := model.SelectionsMatching("opus@high+f", choices); len(selections) != 0 {
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
	sets := CapsCompletions("", "ab", false)
	if sets[0] != "r" || sets[len(sets)-1] != "rxwsnglab" {
		t.Errorf("got %v", sets)
	}
}

func TestAWaivedSandboxCompletesOnlyTheCapabilitiesItCanTake(t *testing.T) {
	if sets := CapsCompletions("", "ab", true); !slices.Equal(sets, []string{"s", "sl", "sla", "slab"}) {
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
	WriteCompletions(&out, []string{"--complete", completeCaps, "rxw"}, func() Sources { return Sources{} })

	if out.String() != "rxw\nrxws\nrxwsn\nrxwsng\nrxwsngl\n" {
		t.Errorf("got %q", out.String())
	}

	out.Reset()
	WriteCompletions(&out, []string{"--complete", "nonsense", ""}, func() Sources { return Sources{} })
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
	writeStoredSession(t, directory, workspaceDir, "archived-ibis", "2026-01-01T00:00:00Z")
	writeStoredSession(t, directory, t.TempDir(), "elsewhere-otter", "2027-01-01T00:00:00Z")
	if err := session.Archive(directory, "archived-ibis"); err != nil {
		t.Fatal(err)
	}

	names := storedSessionNames(directory)
	if !slices.Equal(names, []string{"newer-jaguar", "older-badger"}) {
		t.Errorf("got stored names %v", names)
	}

	if names := withPrefix("old", names); !slices.Equal(names, []string{"older-badger"}) {
		t.Errorf("got prefixed names %v", names)
	}

	anyNames := anySessionNames(directory)
	if !slices.Equal(anyNames, []string{"archived-ibis", "newer-jaguar", "older-badger"}) {
		t.Errorf("got any session names %v", anyNames)
	}
	anyCompletions, isWanted := Complete([]string{completeFlag, completeAnySession, "arch"}, Sources{SessionsDir: directory})
	if !isWanted || !slices.Equal(anyCompletions, []string{"archived-ibis"}) {
		t.Errorf("got any session completions %v and wanted %t", anyCompletions, isWanted)
	}

	missingDirectory := filepath.Join(directory, "missing")
	if names := storedSessionNames(missingDirectory); len(names) > 0 {
		t.Errorf("expected no names for a missing directory, got %v", names)
	}
	if names := anySessionNames(missingDirectory); len(names) > 0 {
		t.Errorf("expected no session names of any kind for a missing directory, got %v", names)
	}
}

func TestGoldenBashCompletionRecognisesEveryForcedResumeFormAndCursorPosition(t *testing.T) {
	completionPath, err := filepath.Abs(filepath.Join("..", "..", "..", "completion", "oh.bash"))
	if err != nil {
		t.Fatal(err)
	}
	callsPath := filepath.Join(t.TempDir(), "calls")
	repliesPath := filepath.Join(t.TempDir(), "replies")
	binaryPath := filepath.Join(t.TempDir(), "oh")
	binary := `#!/bin/bash
set -euo pipefail
printf '%s\n' "$*" > "$CALLS"
case "$2" in
    stored-session) printf '%s\n' stored-ibis ;;
    any-session) printf '%s\n' archived-otter stored-ibis ;;
    option) printf '%s\n' --force ;;
esac
`
	if err := os.WriteFile(binaryPath, []byte(binary), 0o700); err != nil { //nolint:gosec // the test executes this stand-in
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		line   string
		cursor string
		kind   string
		word   string
	}{
		{name: "short resume", line: "-r ", cursor: "-r ", kind: completeStoredSession},
		{name: "long resume", line: "--resume ", cursor: "--resume ", kind: completeStoredSession},
		{name: "short force after short resume", line: "-r -f ", cursor: "-r -f ", kind: completeAnySession},
		{name: "long force after long resume", line: "--resume --force ", cursor: "--resume --force ", kind: completeAnySession},
		{name: "long force after short resume", line: "-r --force ", cursor: "-r --force ", kind: completeAnySession},
		{name: "short force after long resume", line: "--resume -f ", cursor: "--resume -f ", kind: completeAnySession},
		{name: "short force before short resume", line: "-f -r ", cursor: "-f -r ", kind: completeAnySession},
		{name: "long force before long resume", line: "--force --resume ", cursor: "--force --resume ", kind: completeAnySession},
		{name: "long force before short resume", line: "--force -r ", cursor: "--force -r ", kind: completeAnySession},
		{name: "short force before long resume", line: "-f --resume ", cursor: "-f --resume ", kind: completeAnySession},
		{name: "resume then force group", line: "-rf ", cursor: "-rf ", kind: completeAnySession},
		{name: "force then resume group", line: "-fr ", cursor: "-fr ", kind: completeAnySession},
		{name: "force group after resume", line: "-r -pf ", cursor: "-r -pf ", kind: completeAnySession},
		{name: "later resume ignored", line: "-f --resume ", cursor: "-f ", kind: completeOption},
		{name: "later force ignored", line: "-r -f ", cursor: "-r ", kind: completeStoredSession},
		{name: "stored session prefix", line: "-r archived", cursor: "-r archived", kind: completeStoredSession, word: "archived"},
		{name: "forced session prefix", line: "-r -f arch", cursor: "-r -f arch", kind: completeAnySession, word: "arch"},
		{name: "fork source", line: "--from ", cursor: "--from ", kind: completeStoredSession},
	}
	var golden strings.Builder
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := exec.CommandContext(t.Context(), "bash", "-c", `
source "$COMPLETION"
COMP_WORDS=("$OH")
COMP_LINE="$OH $LINE_INPUT"
POINT_LINE="$OH $CURSOR_INPUT"
COMP_POINT=${#POINT_LINE}
_oh
printf '%s\n' "${COMPREPLY[@]}" > "$REPLIES"
`)
			command.Env = append(
				os.Environ(),
				"CALLS="+callsPath,
				"COMPLETION="+completionPath,
				"CURSOR_INPUT="+test.cursor,
				"LINE_INPUT="+test.line,
				"OH="+binaryPath,
				"REPLIES="+repliesPath,
			)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("bash completion failed: %v\n%s", err, output)
			}
			called, err := os.ReadFile(callsPath) //nolint:gosec // the test's own call log
			if err != nil {
				t.Fatal(err)
			}
			want := "--complete " + test.kind
			if test.word != "" {
				want += " " + test.word
			}
			request := strings.TrimSpace(string(called))
			if request != want {
				t.Errorf("completion called %q, want %q", request, want)
			}
			replies, err := os.ReadFile(repliesPath) //nolint:gosec // the test's own completion log
			if err != nil {
				t.Fatal(err)
			}
			_, _ = fmt.Fprintf(
				&golden,
				"%s\n  line: %q\n  cursor: %q\n  request: %q\n  replies: %q\n\n",
				test.name,
				test.line,
				test.cursor,
				request,
				strings.Fields(string(replies)),
			)
		})
	}
	assertCLIGolden(t, "bash-completion.txt", strings.TrimRight(golden.String(), "\n")+"\n")
}

func assertCLIGolden(t *testing.T, name string, drawn string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)
	if *updateGoldens {
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
