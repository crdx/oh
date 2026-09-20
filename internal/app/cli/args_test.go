package cli

import (
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"crdx.org/io/internal/app/caps"
	"crdx.org/io/internal/app/cycle"
	"crdx.org/io/internal/app/model"
)

const helpProcessVariable = "OH_TEST_HELP_PROCESS"

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestGoldenUsageMatchesTheGolden(t *testing.T) {
	if os.Getenv(helpProcessVariable) != "" {
		os.Args = []string{"oh", "--help"}
		Bind()
		return
	}

	//nolint:gosec // rerun this test binary as its help subprocess
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestGoldenUsageMatchesTheGolden$")
	command.Env = append(os.Environ(), helpProcessVariable+"=1")
	output, err := command.CombinedOutput()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 2 {
		t.Fatalf("help process returned %v", err)
	}

	goldenPath := filepath.Join("testdata", "usage.txt")
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, output, 0o600); err != nil { //nolint:gosec // fixed testdata path
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != string(want) {
		t.Errorf("usage differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, output, want)
	}
}

func modelCachePath() string {
	return filepath.Join(os.Getenv("XDG_STATE_HOME"), "models.json")
}

func bind(t *testing.T, arguments ...string) Input {
	t.Helper()

	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })

	os.Args = append([]string{"oh"}, arguments...)

	return *Bind()
}

func TestLoginProviderIsOptional(t *testing.T) {
	withoutProvider := bind(t, "-L")
	if !withoutProvider.Login || withoutProvider.Provider != "" {
		t.Errorf("got %+v", withoutProvider)
	}

	withProvider := bind(t, "-L", "anthropic")
	if !withProvider.Login || withProvider.Provider != "anthropic" {
		t.Errorf("got %+v", withProvider)
	}
}

func parseOptions(t *testing.T, arguments ...string) Options {
	t.Helper()

	settledOptions, err := bind(t, arguments...).Parse(modelCachePath(), model.Defaults{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return settledOptions
}

func useCachedModels(t *testing.T) {
	t.Helper()

	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path := modelCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	data := []byte(`{"version":5,"providers":{"opencode-go":{"models":[{"id":"deepseek-v4-pro","efforts":["high","max"],"output":384000}]}}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestEveryOptionIsRead(t *testing.T) {
	useCachedModels(t)

	parsedOptions := parseOptions(
		t,
		"-c", "r",
		"-m", "deepseek@hi",
		"-t", "read",
		"--tool", "grep",
		"Use", "the", "brief.",
	)

	if parsedOptions.Caps != caps.Read {
		t.Errorf("expected reading alone, got %s", parsedOptions.Caps.Flags())
	}

	wantSelection := model.Selection{Provider: model.OpencodeGoProvider, Model: "deepseek-v4-pro", Effort: "high"}
	if parsedOptions.Selection != wantSelection {
		t.Errorf("expected %s, got %s", wantSelection, parsedOptions.Selection)
	}

	if !slices.Equal(parsedOptions.Tools, []string{"read", "grep"}) {
		t.Errorf("expected read and grep, got %v", parsedOptions.Tools)
	}
	if parsedOptions.Message != "Use the brief." {
		t.Errorf("expected the caller's opening message, got %q", parsedOptions.Message)
	}

	id := "0347juX1xcrL9W0QKJe0cs"

	if parsedOptions := parseOptions(t, "-r", id); parsedOptions.Session != id || !parsedOptions.Resuming() {
		t.Errorf("expected the session, got %q", parsedOptions.Session)
	}
}

func TestModelSelectionRequiresModelAndEffort(t *testing.T) {
	for _, selection := range []string{"model", "model@", "@high", "model@high@extra"} {
		if _, err := (Input{inputFlags: inputFlags{Model: selection}}).Parse(modelCachePath(), model.Defaults{}); err == nil {
			t.Errorf("expected %q to be rejected", selection)
		}
	}
}

func TestANewConversationMayStartFromASession(t *testing.T) {
	parsedOptions := parseOptions(t, "--from", "chosen-lobster", "carry", "on")

	if !parsedOptions.StartingFromSession() || parsedOptions.SourceSession != "chosen-lobster" {
		t.Errorf("expected the source session, got %q", parsedOptions.SourceSession)
	}
	if parsedOptions.Resuming() {
		t.Error("expected starting from a session not to count as resuming")
	}
	if parsedOptions.Message != "carry on" {
		t.Errorf("expected the prompt beside it, got %q", parsedOptions.Message)
	}
}

func TestASessionMayBeResumedWithAPromptBesideIt(t *testing.T) {
	for _, argument := range []string{"-r", "--resume"} {
		parsedOptions := parseOptions(t, argument, "0347juX1xcrL9W0QKJe0cs", "carry", "on")

		if !parsedOptions.Resuming() {
			t.Errorf("expected %s to resume the session", argument)
		}

		if parsedOptions.Message != "carry on" {
			t.Errorf("expected the prompt beside %s, got %q", argument, parsedOptions.Message)
		}
	}
}

func TestTheVersionIsAskedForOnItsOwn(t *testing.T) {
	for _, argument := range []string{"--version", "-v"} {
		if !bind(t, argument).Version {
			t.Errorf("expected the version to be asked for by %s", argument)
		}
	}
}

func TestTheModelListIsAskedForOnItsOwn(t *testing.T) {
	for _, argument := range []string{"--list", "-l"} {
		if !bind(t, argument).List {
			t.Errorf("expected the model list to be asked for by %s", argument)
		}
	}
}

func TestTheSessionPickerIsAskedForByBareResumeOption(t *testing.T) {
	for _, argument := range []string{"-r", "--resume"} {
		if !bind(t, argument).IsSessionPicker {
			t.Errorf("expected a bare %s to ask for the session picker", argument)
		}
	}
}

func TestTheModelPickerIsAskedForByBareModelOption(t *testing.T) {
	for _, argument := range []string{"-m", "--model"} {
		input := bind(t, argument)
		if !input.IsModelPicker {
			t.Errorf("expected a bare %s to ask for the model picker", argument)
		}
		if input.Model != "" {
			t.Errorf("expected no model to be named by %s, got %q", argument, input.Model)
		}
	}
}

func TestAModelGivenToTheModelOptionIsNotThePicker(t *testing.T) {
	input := bind(t, "-m", "codex/gpt@high")

	if input.IsModelPicker {
		t.Error("expected a named model not to ask for the picker")
	}
	if input.Model != "codex/gpt@high" {
		t.Errorf("expected the named model, got %q", input.Model)
	}
}

func TestALeadingDashNamesThePipeRatherThanThePrompt(t *testing.T) {
	if parsedOptions := parseOptions(t, "-"); parsedOptions.Message != "" {
		t.Errorf("expected a bare dash to say nothing, got %q", parsedOptions.Message)
	}

	if parsedOptions := parseOptions(t, "-", "explain", "this"); parsedOptions.Message != "explain this" {
		t.Errorf("expected the words beside the dash, got %q", parsedOptions.Message)
	}

	if parsedOptions := parseOptions(t, "what", "does", "-", "mean"); parsedOptions.Message != "what does - mean" {
		t.Errorf("expected a dash within the prompt to be a word of it, got %q", parsedOptions.Message)
	}
}

func TestAPrintedSessionNeedsMoreThanTheStdinMarker(t *testing.T) {
	if err := bind(t, "-p", "-").Check(false); err == nil {
		t.Error("expected a printed session with nothing piped to be refused")
	}

	if err := bind(t, "-p", "-").Check(true); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTheSimulationIsRefusedBesideASessionOrAModel(t *testing.T) {
	if err := bind(t, "--demo").Check(false); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	for _, arguments := range [][]string{
		{"--demo", "-r", "earlier-session"},
		{"--demo", "-r"},
		{"--demo", "--from", "earlier-session"},
		{"--demo", "-m", "codex/model"},
		{"--demo", "-m"},
	} {
		if err := bind(t, arguments...).Check(false); err == nil {
			t.Errorf("expected %v to be refused", arguments)
		}
	}
}

func TestTheIgnoredModelsAreAskedForAlongsideAnUpdate(t *testing.T) {
	input := bind(t, "-u", "-I")

	if !input.Update || !input.IsShowingIgnored {
		t.Errorf("expected both flags to be read, got Update=%v IsShowingIgnored=%v",
			input.Update, input.IsShowingIgnored)
	}
}

func TestWhateverIsLeftOverIsTheFirstThingSaid(t *testing.T) {
	parsedOptions := parseOptions(t, "why", "does", "the", "spinner", "stutter")

	if parsedOptions.Message != "why does the spinner stutter" {
		t.Errorf("expected the words back as one, got %q", parsedOptions.Message)
	}
}

func TestTheDefaultCapabilitiesAreReadingAndTheShell(t *testing.T) {
	parsedOptions := parseOptions(t)

	if got := parsedOptions.Caps.Flags(); got != "rx" {
		t.Errorf("expected rx, got %q", got)
	}
	if parsedOptions.WereCapsChosen {
		t.Error("expected the default capabilities to count as unchosen")
	}

	if parsedOptions.Message != "" {
		t.Errorf("expected nothing said, got %q", parsedOptions.Message)
	}

	if len(parsedOptions.Tools) != 0 {
		t.Errorf("expected every tool by default, got %v", parsedOptions.Tools)
	}
}

func TestCapabilitiesAreReadAsTheLettersTheyAreSpelledWith(t *testing.T) {
	for _, capString := range []string{"rxwngl", "lgnwxr", "wxngl"} {
		currentCaps, err := caps.Parse(capString)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", capString, err)
		}

		if got := currentCaps.Flags(); got != caps.AllFlags {
			t.Errorf("%s: expected it written back as %s, got %q", capString, caps.AllFlags, got)
		}
	}

	if _, err := caps.Parse("rwz"); err == nil {
		t.Error("expected a letter naming no capability to be refused")
	}
}

func TestReadingIsAlwaysGranted(t *testing.T) {
	grantedCaps, err := caps.Parse("")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if got := grantedCaps.Flags(); got != "r" {
		t.Errorf("expected r, got %q", got)
	}
}

func TestASessionCannotBeResumedAndUsedAsTheSourceTogether(t *testing.T) {
	input := Input{inputFlags: inputFlags{Session: "one"}, SourceSession: "another"}
	if _, err := input.Parse(modelCachePath(), model.Defaults{}); err == nil {
		t.Error("expected an error")
	}
}

func TestAModeNamedOnTheCommandLineCountsAsChosen(t *testing.T) {
	opts := Input{inputFlags: inputFlags{Session: "one", Caps: "rx"}}
	settledOptions, err := opts.Parse(modelCachePath(), model.Defaults{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if settledOptions.Caps.Has(caps.Write) {
		t.Error("expected writing to be held back")
	}
	if !settledOptions.WereCapsChosen {
		t.Error("expected the named capabilities to count as chosen")
	}
}

func TestTheYoloFlagWaivesTheSandbox(t *testing.T) {
	settledOptions, err := Input{inputFlags: inputFlags{Yolo: true}}.Parse(modelCachePath(), model.Defaults{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !settledOptions.Yolo {
		t.Error("expected --yolo to waive the sandbox")
	}

	settledOptions, err = Input{}.Parse(modelCachePath(), model.Defaults{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if settledOptions.Yolo {
		t.Error("expected a sandbox without --yolo")
	}
}

func TestOnlyANewSessionInheritsAWaivedSandbox(t *testing.T) {
	if got := InheritedOptions([]string{"--yolo", "hello"}, cycle.NewSession); !slices.Equal(got, []string{"--yolo"}) {
		t.Errorf("got %v, want --yolo handed on to a new session", got)
	}

	if got := InheritedOptions([]string{"--yolo"}, cycle.ResumeSession); got != nil {
		t.Errorf("got %v, want a resumed session to take its confinement from its own journal", got)
	}

	if got := InheritedOptions([]string{"hello"}, cycle.NewSession); got != nil {
		t.Errorf("got %v, want nothing handed on by a sandboxed session", got)
	}
}
