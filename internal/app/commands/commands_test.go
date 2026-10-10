package commands

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/dispatch"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/app/snippets"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/agent"
)

type commandTestContext struct {
	events             []agent.Event
	notice             string
	continuationIndent int
	hasOwnStyle        bool
	isListing          bool
	success            string
	paintedOver        []string
}

func newCommandTestContext(t *testing.T) *commandTestContext {
	t.Helper()

	context := &commandTestContext{}
	t.Cleanup(func() {
		for _, text := range context.paintedOver {
			t.Errorf("a status notice would paint its colour over styled text %q", strutil.VisibleEscapes(text))
		}
	})
	return context
}

func (self *commandTestContext) Emit(event agent.Event) { self.events = append(self.events, event) }
func (self *commandTestContext) Send(string)            {}

func (self *commandTestContext) Notice(text string) {
	self.showStatus(text)
}

func (self *commandTestContext) NoticeIndented(text string, continuationIndent int) {
	self.showStatus(text)
	self.continuationIndent = continuationIndent
}

func (self *commandTestContext) NoticeListing(text string) {
	self.showStatus(text)
	self.isListing = true
}

func (self *commandTestContext) PlainNotice(text string) {
	self.notice = text
	self.hasOwnStyle = true
}

func (self *commandTestContext) PlainNoticeIndented(text string, continuationIndent int) {
	self.notice = text
	self.continuationIndent = continuationIndent
	self.hasOwnStyle = true
}

func (self *commandTestContext) PlainNoticeListing(text string) {
	self.notice = text
	self.hasOwnStyle = true
	self.isListing = true
}

func (self *commandTestContext) Success(text string) {
	self.recordPaintedOver(text)
	self.success = text
}

func (self *commandTestContext) showStatus(text string) {
	self.recordPaintedOver(text)
	self.notice = text
}

func (self *commandTestContext) recordPaintedOver(text string) {
	if strings.Contains(text, "\x1b") {
		self.paintedOver = append(self.paintedOver, text)
	}
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
			if start.CapFlags != "" {
				action += ":caps=" + start.CapFlags
			}
			if len(start.Tools) > 0 {
				action += ":tools=" + strings.Join(start.Tools, ",")
			}
			if start.IsYolo {
				action += ":yolo"
			}
			actions = append(actions, action)
			return nil
		},
	})

	tests := map[string]string{
		"/conf":                     "edit:" + strings.Join([]string{configDirectory, configPath}, ","),
		"/edit config-file":         "edit:" + configPath,
		"/edit system-prompt-file":  "edit:" + systemPromptPath,
		"/edit workspace-dir":       "edit:" + workspaceDirectory,
		"/edit skills-dir":          "edit:" + strings.Join(skillDirectories[:2], ","),
		"/edit snippets-dir":        "edit:" + snippetsDirectory,
		"/open skills-dir":          "open:" + strings.Join(skillDirectories[:2], ","),
		"/open snippets-dir":        "open:" + snippetsDirectory,
		"/new":                      "new:",
		"/fork":                     "fork:tame-impala:",
		"/new sonnet":               "new:sonnet",
		"/fork sonnet":              "fork:tame-impala:sonnet",
		"/fork sonnet --yolo":       "fork:tame-impala:sonnet:yolo",
		"/new -m sonnet":            "new:sonnet",
		"/new --model sonnet":       "new:sonnet",
		"/new -c rxw --yolo":        "new::caps=rxw:yolo",
		"/new -t read --tool grep":  "new::tools=read,grep",
		"/fork -m sonnet --yolo":    "fork:tame-impala:sonnet:yolo",
		"/fork --caps l -m sonnet":  "fork:tame-impala:sonnet:caps=l",
		"/new --from able-dolphin":  "fork:able-dolphin:",
		"/fork --from able-dolphin": "fork:able-dolphin:",
		"/new -f able-dolphin":      "fork:able-dolphin:",
		"/fork -f able-dolphin":     "fork:able-dolphin:",
		"/fork --from able-dolphin -m sonnet --yolo": "fork:able-dolphin:sonnet:yolo",
		"/open config-dir":                           "open:" + configDirectory,
		"/open workspace-dir":                        "open:" + workspaceDirectory,
		"/open scratch-dir":                          "open:" + scratchDirectory,
		"/open home-dir":                             "open:" + homeDirectory,
		"/open session-dir":                          "open:" + sessionDirectory,
		"/copy last-message":                         "copy:The latest answer.",
		"/copy session-chat":                         "copy:" + chatContents,
		"/copy session-name":                         "copy:tame-impala",
		"/copy session-id":                           "copy:session-id",
		"/copy session-dir":                          "copy:" + sessionDirectory,
		"/copy config-file":                          "copy:" + configPath,
		"/copy skills-dir":                           "copy:" + strings.Join(skillDirectories[:2], ","),
		"/copy snippets-dir":                         "copy:" + snippetsDirectory,
		"/open session-log-file":                     "open:" + filepath.Join(sessionDirectory, sessionJournalName),
		"/edit session-log-file":                     "edit:" + filepath.Join(sessionDirectory, sessionJournalName),
		"/open session-chat-file":                    "open:" + filepath.Join(sessionDirectory, sessionTranscriptName),
		"/edit session-chat-file":                    "edit:" + filepath.Join(sessionDirectory, sessionTranscriptName),
		"/edit agents-file":                          "edit:" + projectContextPath,
		"/open agents-file":                          "open:" + projectContextPath,
		"/copy agents-file":                          "copy:" + projectContextPath,
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
			context := newCommandTestContext(t)
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

func TestConfCreatesTheConfigDirWithoutCreatingASystemPrompt(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "org.crdx", "oh")
	configPath := filepath.Join(configDirectory, "config.toml")
	systemPromptPath := filepath.Join(configDirectory, "SYSTEM.md")
	var opened []string
	commands := newCommandRegistry(t, commandEnvironment{
		configDir:        configDirectory,
		configPath:       configPath,
		systemPromptPath: systemPromptPath,
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
	if _, err := os.Stat(systemPromptPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("SYSTEM.md was created: %v", err)
	}
	want := []string{configDirectory, configPath}
	if !slices.Equal(opened, want) {
		t.Errorf("got %v, want %v", opened, want)
	}
}

func TestConfIncludesAnExistingSystemPrompt(t *testing.T) {
	configDirectory := t.TempDir()
	configPath := filepath.Join(configDirectory, "config.toml")
	systemPromptPath := filepath.Join(configDirectory, "SYSTEM.md")
	if err := os.WriteFile(systemPromptPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	var opened []string
	commands := newCommandRegistry(t, commandEnvironment{
		configDir:        configDirectory,
		configPath:       configPath,
		systemPromptPath: systemPromptPath,
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

	want := []string{configDirectory, systemPromptPath, configPath}
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
		"/new sonnet -m opus",
		"/fork one two",
		"/new -m",
		"/new -m -c rx",
		"/new -m sonnet --model opus",
		"/new -c rx -c r",
		"/new --yolo --yolo",
		"/new -x",
		"/fork -t",
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

	result, failure := dispatch.Handle(commands, dispatch.Actions{}, "/new -m opus")
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
		completions := commands.Completions(prefix)
		if len(completions) == 0 || completions[0].Text != want {
			t.Errorf("Completions(%q) got %+v, want %q first", prefix, completions, want)
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

func TestEditReportsThatNoEditorCanBeOpened(t *testing.T) {
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
	if !errors.Is(err, errEditorUnavailable) {
		t.Errorf("got error %v", err)
	}
}

func TestAnInheritedWaiverIsNeitherOfferedAgainNorWidenedByCapabilities(t *testing.T) {
	environment := fixtureEnvironment(t)
	environment.isYoloInherited = true
	commands := newCommandRegistry(t, environment)

	var options []string
	for _, completion := range commands.Completions("/new -") {
		options = append(options, completion.Label)
	}
	if want := []string{"-m", "-c", "-t", "-f"}; !slices.Equal(options, want) {
		t.Errorf("got options %v, want %v", options, want)
	}

	var flags []string
	for _, completion := range commands.Completions("/new -c ") {
		flags = append(flags, completion.Label)
	}
	if want := []string{"s", "sl", "sla"}; !slices.Equal(flags, want) {
		t.Errorf("got capabilities %v, want %v", flags, want)
	}
}

func TestTheSourceOptionListsTheStoredSessions(t *testing.T) {
	environment := fixtureEnvironment(t)
	commands := newCommandRegistry(t, environment)

	var names []string
	for _, completion := range commands.Completions("/new --from a") {
		names = append(names, completion.Label)
	}
	if want := []string{"able-dolphin", "agile-turtle"}; !slices.Equal(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}

	for _, prefix := range []string{"/new --from ", "/fork --from ", "/new -f ", "/fork -f "} {
		var labels []string
		for _, completion := range commands.Completions(prefix) {
			labels = append(labels, completion.Label)
		}
		if want := []string{"able-dolphin", "agile-turtle", "tame-impala", "wise-otter"}; !slices.Equal(labels, want) {
			t.Errorf("%q got %v, want %v", prefix, labels, want)
		}
	}

	environment.getSessionNames = nil
	commands = newCommandRegistry(t, environment)
	if completions := commands.Completions("/new --from "); len(completions) > 0 {
		t.Errorf("expected no session source to leave the name free-form, got %v", completions)
	}
}

func TestCompletingAToolLeavesTheListedToolsAlone(t *testing.T) {
	toolNames := []string{"read", "grep", "bash"}
	environment := fixtureEnvironment(t)
	environment.getToolNames = func() []string { return toolNames }
	commands := newCommandRegistry(t, environment)

	for range 2 {
		var labels []string
		for _, completion := range commands.Completions("/new -t read -t ") {
			labels = append(labels, completion.Label)
		}
		if want := []string{"bash", "grep"}; !slices.Equal(labels, want) {
			t.Errorf("got %v, want %v", labels, want)
		}
	}
	if want := []string{"read", "grep", "bash"}; !slices.Equal(toolNames, want) {
		t.Errorf("the listed tools became %v", toolNames)
	}
}
