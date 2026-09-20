package commands

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/internal/app/dispatch"
	"crdx.org/io/internal/app/slash"
	"crdx.org/io/internal/app/snippets"
	"crdx.org/io/internal/app/work"
	"crdx.org/io/pkg/agent"
)

type commandTestContext struct {
	events  []agent.Event
	notice  string
	success string
}

func (self *commandTestContext) Emit(event agent.Event)  { self.events = append(self.events, event) }
func (self *commandTestContext) Send(string)             {}
func (self *commandTestContext) Notice(text string)      { self.notice = text }
func (self *commandTestContext) PlainNotice(text string) { self.notice = text }
func (self *commandTestContext) Success(text string) {
	self.success = text
}

func newCommandRegistry(t *testing.T, environment commandEnvironment) slash.Registry {
	t.Helper()

	set, err := buildCommands(environment)
	if err != nil {
		t.Fatal(err)
	}
	return commandRegistry(t, set)
}

func newCommandRegistryWithSnippets(
	t *testing.T,
	environment commandEnvironment,
	configuredSnippets map[string]snippets.Definition,
) slash.Registry {
	t.Helper()

	snippetSet, err := snippets.New(configuredSnippets)
	if err != nil {
		t.Fatal(err)
	}
	systemSet, err := buildCommands(environment)
	if err != nil {
		t.Fatal(err)
	}
	return commandRegistry(t, systemSet, snippetSet)
}

func commandRegistry(t *testing.T, sets ...slash.CommandSet) slash.Registry {
	t.Helper()

	registry, err := slash.NewRegistry(sets...)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestCommandsRunWithoutStoppingTheHarness(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDirectory := filepath.Join(configHome, "org.crdx", "oh")
	configPath := filepath.Join(configDirectory, "config.toml")
	systemPromptPath := filepath.Join(configDirectory, "SYSTEM.md")
	workspaceDirectory := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspaceDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	projectContextPath := filepath.Join(workspaceDirectory, "AGENTS.md")
	if err := os.WriteFile(projectContextPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	scratchDirectory := filepath.Join(t.TempDir(), "scratch")
	homeDirectory := filepath.Join(t.TempDir(), "home")
	skillDirectories := []string{
		filepath.Join(configDirectory, "skills"),
		filepath.Join(t.TempDir(), "shared-skills"),
		filepath.Join(t.TempDir(), "skills-that-were-moved"),
	}
	for _, directory := range skillDirectories[:2] {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	snippetsDirectory := filepath.Join(configDirectory, "snippets")
	if err := os.Mkdir(snippetsDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	sessionDirectory := filepath.Join(t.TempDir(), "sessions", "tame-impala")
	if err := os.MkdirAll(sessionDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDirectory, sessionJournalName), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	chatContents := "# Chat\n\nA useful answer.\n"
	if err := os.WriteFile(filepath.Join(sessionDirectory, sessionTranscriptName), []byte(chatContents), 0o600); err != nil {
		t.Fatal(err)
	}
	var actions []string
	commands := newCommandRegistry(t, commandEnvironment{
		configDir:        configDirectory,
		configPath:       configPath,
		systemPromptPath: systemPromptPath,
		workspace:        work.At(workspaceDirectory),
		scratchDir:       scratchDirectory,
		homeDir:          homeDirectory,
		skillDirs:        skillDirectories,
		session: commandSession{
			name:           "tame-impala",
			id:             "session-id",
			directory:      sessionDirectory,
			isPersisted:    func() bool { return true },
			getLastMessage: func() (string, bool) { return "The latest answer.", true },
		},
		openEditor: func(paths []string) error {
			actions = append(actions, "edit:"+strings.Join(paths, ","))
			return nil
		},
		openTarget: func(paths []string) error {
			actions = append(actions, "open:"+strings.Join(paths, ","))
			return nil
		},
		copyText: func(values []string) error {
			actions = append(actions, "copy:"+strings.Join(values, ","))
			return nil
		},
		startSession: func(start SessionStart) error {
			action := "new:" + start.ModelGlob
			if start.SourceSessionName != "" {
				action = "fork:" + start.SourceSessionName + ":" + start.ModelGlob
			}
			actions = append(actions, action)
			return nil
		},
	})

	tests := map[string]string{
		"/conf":                    "edit:" + strings.Join([]string{configDirectory, systemPromptPath, configPath}, ","),
		"/edit config-file":        "edit:" + configPath,
		"/edit system-prompt-file": "edit:" + systemPromptPath,
		"/edit workspace-dir":      "edit:" + workspaceDirectory,
		"/edit skills-dir":         "edit:" + strings.Join(skillDirectories[:2], ","),
		"/edit snippets-dir":       "edit:" + snippetsDirectory,
		"/open skills-dir":         "open:" + strings.Join(skillDirectories[:2], ","),
		"/open snippets-dir":       "open:" + snippetsDirectory,
		"/new":                     "new:",
		"/new sonnet":              "new:sonnet",
		"/fork":                    "fork:tame-impala:",
		"/fork sonnet":             "fork:tame-impala:sonnet",
		"/open config-dir":         "open:" + configDirectory,
		"/open workspace-dir":      "open:" + workspaceDirectory,
		"/open scratch-dir":        "open:" + scratchDirectory,
		"/open home-dir":           "open:" + homeDirectory,
		"/open session-dir":        "open:" + sessionDirectory,
		"/copy last-message":       "copy:The latest answer.",
		"/copy session-chat":       "copy:" + chatContents,
		"/copy session-name":       "copy:tame-impala",
		"/copy session-id":         "copy:session-id",
		"/copy session-dir":        "copy:" + sessionDirectory,
		"/copy config-file":        "copy:" + configPath,
		"/copy skills-dir":         "copy:" + strings.Join(skillDirectories[:2], ","),
		"/copy snippets-dir":       "copy:" + snippetsDirectory,
		"/open session-log-file":   "open:" + filepath.Join(sessionDirectory, sessionJournalName),
		"/edit session-log-file":   "edit:" + filepath.Join(sessionDirectory, sessionJournalName),
		"/open session-chat-file":  "open:" + filepath.Join(sessionDirectory, sessionTranscriptName),
		"/edit session-chat-file":  "edit:" + filepath.Join(sessionDirectory, sessionTranscriptName),
		"/edit agents-file":        "edit:" + projectContextPath,
		"/open agents-file":        "open:" + projectContextPath,
		"/copy agents-file":        "copy:" + projectContextPath,
	}

	wantConfirmations := map[string]string{
		"/copy last-message": "Copied last message to clipboard",
		"/copy session-chat": "Copied session chat to clipboard",
		"/copy session-name": "Copied session name to clipboard: tame-impala",
		"/copy session-id":   "Copied session id to clipboard: session-id",
		"/copy session-dir":  "Copied session dir to clipboard: " + sessionDirectory,
		"/copy config-file":  "Copied config file to clipboard: " + configPath,
		"/copy skills-dir": "Copied skills dir to clipboard: " +
			strings.Join(skillDirectories[:2], ", "),
		"/copy snippets-dir": "Copied snippets dir to clipboard: " + snippetsDirectory,
		"/copy agents-file":  "Copied agents file to clipboard: " + projectContextPath,
	}

	for input, wantAction := range tests {
		t.Run(input, func(t *testing.T) {
			actions = nil
			invocation, found := commands.Find(input)
			if !found {
				t.Fatal("expected command to be found")
			}
			context := &commandTestContext{}
			if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(actions, []string{wantAction}) {
				t.Errorf("got actions %v, want %q", actions, wantAction)
			}
			wantSuccess := wantConfirmations[input]
			if context.success != wantSuccess {
				t.Errorf("got success %q, want %q", context.success, wantSuccess)
			}
		})
	}

	if _, err := os.Stat(configDirectory); err != nil {
		t.Errorf("config directory was not prepared: %v", err)
	}
}

func TestSnippetsDirectoryTargetRequiresAnExistingDirectory(t *testing.T) {
	configDirectory := t.TempDir()
	if _, exists := locationTargets(commandEnvironment{configDir: configDirectory})["snippets-dir"]; exists {
		t.Error("found snippets-dir before the directory existed")
	}
	if err := os.Mkdir(filepath.Join(configDirectory, "snippets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, exists := locationTargets(commandEnvironment{configDir: configDirectory})["snippets-dir"]; !exists {
		t.Error("snippets-dir was absent after the directory was created")
	}
}

func TestConfCreatesTheConfigDirBeforeOpeningIt(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "org.crdx", "oh")
	var opened []string
	commands := newCommandRegistry(t, commandEnvironment{
		configDir:        configDirectory,
		configPath:       filepath.Join(configDirectory, "config.toml"),
		systemPromptPath: filepath.Join(configDirectory, "SYSTEM.md"),
		openEditor: func(paths []string) error {
			opened = paths
			return nil
		},
	})

	invocation, found := commands.Find("/conf")
	if !found {
		t.Fatal("expected /conf to be registered")
	}
	if err := invocation.Command.Run(nil, invocation.Arguments); err != nil {
		t.Fatal(err)
	}

	if info, err := os.Stat(configDirectory); err != nil || !info.IsDir() {
		t.Errorf("config directory was not created: %v", err)
	}
	want := []string{
		configDirectory,
		filepath.Join(configDirectory, "SYSTEM.md"),
		filepath.Join(configDirectory, "config.toml"),
	}
	if !slices.Equal(opened, want) {
		t.Errorf("got %v, want %v", opened, want)
	}
}

func TestForkRequiresAPersistedSession(t *testing.T) {
	isPersisted := false
	didStartRun := false
	commands := newCommandRegistry(t, commandEnvironment{
		session: commandSession{
			name:        "tame-impala",
			isPersisted: func() bool { return isPersisted },
		},
		startSession: func(SessionStart) error {
			didStartRun = true
			return nil
		},
	})

	result, failure := dispatch.Handle(commands, dispatch.Actions{}, "/fork")
	if result != dispatch.Rejected {
		t.Fatalf("expected the command to be refused, got result %d", result)
	}
	want := "/fork: Session does not exist yet (alt+enter to send)"
	if failure != want {
		t.Errorf("got %q, want %q", failure, want)
	}
	if didStartRun {
		t.Error("fork started before the session was stored")
	}

	isPersisted = true
	result, failure = dispatch.Handle(commands, dispatch.Actions{}, "/fork")
	if result != dispatch.Handled || failure != "" {
		t.Errorf("stored session got result %d and failure %q", result, failure)
	}
	if !didStartRun {
		t.Error("fork did not start after the session was stored")
	}
}

func TestCommandsRejectUnknownOrExtraTargets(t *testing.T) {
	commands := newCommandRegistry(t, commandEnvironment{
		session: commandSession{isPersisted: func() bool { return true }},
	})

	for _, input := range []string{
		"/edit config-file extra",
		"/conf extra",
		"/edit unknown",
		"/help extra",
		"/info extra",
		"/new one two",
		"/fork one two",
		"/copy session-name extra",
		"/open session-chat",
		"/edit session-chat",
		"/open session-log",
		"/edit session-log",
		"/open unknown",
	} {
		t.Run(input, func(t *testing.T) {
			invocation, found := commands.Find(input)
			if !found {
				t.Fatal("expected command to be found")
			}

			err := invocation.Command.Run(nil, invocation.Arguments)
			if err == nil {
				t.Fatal("expected usage error")
			}
			if !slash.IsUsageError(err) {
				t.Errorf("got error %v", err)
			}
			if message := slash.FormatError(invocation, err); !strings.HasPrefix(message, "Usage: ") {
				t.Errorf("got formatted error %q", message)
			}
		})
	}
}

func TestAModelThatCannotBeResolvedLeavesTheCommandToBeCorrected(t *testing.T) {
	commands := newCommandRegistry(t, commandEnvironment{
		startSession: func(SessionStart) error {
			return errors.New(`model "opus" is ambiguous`)
		},
	})

	result, failure := dispatch.Handle(commands, dispatch.Actions{}, "/new opus")
	if result != dispatch.Rejected {
		t.Fatalf("expected the command to be refused, got result %d", result)
	}

	want := `/new: Model "opus" is ambiguous (alt+enter to send)`
	if failure != want {
		t.Errorf("got %q, want %q", failure, want)
	}
}

func TestTargetCommandsExposeTheirArgumentsForCompletion(t *testing.T) {
	commands := newCommandRegistry(t, commandEnvironment{})

	for prefix, want := range map[string]string{
		"/open c":         "/open config-dir",
		"/copy l":         "/copy last-message",
		"/copy session-n": "/copy session-name",
		"/open session-c": "/open session-chat-file",
		"/open session-l": "/open session-log-file",
		"/open w":         "/open workspace-dir",
		"/open scr":       "/open scratch-dir",
		"/edit sy":        "/edit system-prompt-file",
	} {
		var state slash.Completion
		completion, found := state.Next(commands, prefix)
		if !found || completion != want {
			t.Errorf("Complete(%q) got %q and %t, want %q", prefix, completion, found, want)
		}
	}
}

func TestCommandsReportTargetsThatDoNotExistYet(t *testing.T) {
	sessionDirectory := filepath.Join(t.TempDir(), "missing-session")
	didActionRun := false
	commands := newCommandRegistry(t, commandEnvironment{
		session:   commandSession{directory: sessionDirectory},
		skillDirs: []string{filepath.Join(t.TempDir(), "missing-skills")},
		openTarget: func([]string) error {
			didActionRun = true
			return nil
		},
	})

	for input, want := range map[string]string{
		"/open skills-dir":        "Skills directory does not exist yet",
		"/open session-dir":       "Session directory does not exist yet",
		"/open session-log-file":  "Session log does not exist yet",
		"/open session-chat-file": "Session chat does not exist yet",
		"/copy session-chat":      "Session chat does not exist yet",
		"/copy last-message":      "no model message has been received yet",
	} {
		t.Run(input, func(t *testing.T) {
			didActionRun = false
			invocation, found := commands.Find(input)
			if !found {
				t.Fatal("expected command to be found")
			}

			err := invocation.Command.Run(nil, invocation.Arguments)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("got error %v, want %q", err, want)
			}
			if didActionRun {
				t.Error("action ran for a missing target")
			}
		})
	}
}

func TestHelpLeavesTheSnippetsToTheirOwnHelpCommand(t *testing.T) {
	got := helpText([]string{"/edit", "/help"}, "/help", nil)
	if strings.Contains(got, "Snippets:") || strings.Contains(got, "/help") {
		t.Errorf("got help %q", got)
	}
}

func TestBrowseIsNotACommandAnymore(t *testing.T) {
	if _, found := newCommandRegistry(t, commandEnvironment{}).Find("/browse session-dir"); found {
		t.Error("expected /browse to have been folded into /open")
	}
}

func TestEditReportsAnUnconfiguredEditor(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	configDirectory := t.TempDir()
	set, err := New(Options{
		ConfigDir:  configDirectory,
		ConfigFile: filepath.Join(configDirectory, "config.toml"),
	})
	if err != nil {
		t.Fatal(err)
	}
	commands := commandRegistry(t, set)
	invocation, found := commands.Find("/edit config-file")
	if !found {
		t.Fatal("expected /edit to be registered")
	}

	err = invocation.Command.Run(nil, invocation.Arguments)
	if err == nil || !strings.Contains(err.Error(), "set editor in config.toml") {
		t.Errorf("got error %v", err)
	}
}
