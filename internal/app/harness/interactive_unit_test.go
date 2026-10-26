package harness

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/commands"
	"crdx.org/oh/internal/app/contextsource"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/record"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/trigger"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/pkg/agent"
)

func TestInProcessCommandsTypedIntoAnInteractiveSessionAreAnsweredInPlace(t *testing.T) {
	rig := newEditingRig(t, nil, "")
	rig.app.runMode.isYolo = true
	rig.app.mode = caps.NewMode(caps.All())
	workspace := openTestWorkspace(t, t.TempDir())
	grants := preparePathGrantCommands(t, rig.app, workspace)
	set, err := commands.New(commands.Options{
		Workspace: workspace,
		PathGrants: commands.PathGrants{
			Permanent: []shell.ScopedPathGrant{{
				Path:   "/reference",
				Access: shell.ReadAccess | shell.WriteAccess | shell.ExecAccess,
				Kind:   shell.UnconfinedShellGrant,
			}},
			Grant:      grants.Grant,
			Revoke:     grants.Revoke,
			GetCurrent: grants.GetCurrent,
		},
		GetContextSources: func() commands.ContextSources {
			return commands.ContextSources{SystemSources: []contextsource.Source{contextsource.CountedFromBytes(contextsource.ToolDefinitions, 14, 1400)}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rig.app.commands = fixtureRegistry(t, set)

	rig.typeText("/ctx")
	rig.press(key.Key{Code: key.Enter})
	rig.requireScreenContains(" tool definitions")
	rig.press(key.Key{Code: key.Escape})

	rig.typeText("/grants")
	rig.press(key.Key{Code: key.Escape})
	rig.press(key.Key{Code: key.Enter})
	rig.requireScreenContains("Paths:")
	rig.requireScreenOmits("Caps:")
	rig.requireScreenContains("rxw  yolo shell")
	rig.press(key.Key{Code: key.Escape})
}

func TestInProcessCommandArgumentsAreCompletedFromTheSessionItself(t *testing.T) {
	rig := newEditingRig(t, nil, "")
	workspace := t.TempDir()
	sessionsDirectory := t.TempDir()
	for _, name := range []string{"able-dolphin", "agile-turtle", "tame-impala"} {
		stored, err := store.CreateNamed(sessionsDirectory, name, store.Meta{WorkspaceDir: workspace})
		if err != nil {
			t.Fatal(err)
		}
		if err := stored.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "a saved session"}); err != nil {
			t.Fatal(err)
		}
		if err := stored.Close(); err != nil {
			t.Fatal(err)
		}
	}
	models := []model.Choice{{Provider: model.OpencodeGoProvider, ID: "fake", EffortLevels: []string{"high"}}}
	set, err := commands.New(commands.Options{
		Workspace:       work.At(workspace),
		GetModelChoices: func() []model.Choice { return models },
		GetSessionNames: func() []string {
			stored, err := store.List(sessionsDirectory)
			if err != nil {
				t.Fatal(err)
			}
			names := make([]string, 0, len(stored))
			for _, session := range stored {
				names = append(names, session.Name)
			}
			return names
		},
		Session: commands.Session{Name: "tame-impala", IsPersisted: func() bool { return true }},
	})
	if err != nil {
		t.Fatal(err)
	}
	rig.app.commands = fixtureRegistry(t, set)
	rig.app.completer = trigger.New(slash.NewSource(func() slash.Registry { return rig.app.commands }))

	if got := rig.app.commands.Completions("/fork --from "); len(got) == 0 {
		stored, _ := store.List(sessionsDirectory)
		t.Fatalf("session arguments have no completions (stored %d)", len(stored))
	}
	rig.requireScreenOmits("opencode-go/fake")
	rig.typeText("/new -m opencode-go/")
	rig.requireScreenContains("› opencode-go/fake@")
	rig.press(tabKey)
	rig.requireInputPrefix("/new -m opencode-go/fake@")
	rig.requireScreenOmits("› ")
	rig.press(key.Key{Code: key.Rune, Value: 'u', Mod: key.Ctrl})

	rig.typeText("/fork -m opencode-go/fa")
	rig.requireScreenContains("› opencode-go/fake@")
	rig.press(key.Key{Code: key.Escape})
	rig.press(key.Key{Code: key.Rune, Value: 'u', Mod: key.Ctrl})

	rig.typeText("/fork --from ")
	rig.requireScreenContains("› able-dolphin")
	rig.press(key.Key{Code: key.Rune, Value: 'u', Mod: key.Ctrl})

	rig.typeText("/fork --from ag")
	rig.requireScreenContains("› agile-turtle")
	rig.press(key.Key{Code: key.Escape})
	rig.press(key.Key{Code: key.Rune, Value: 'u', Mod: key.Ctrl})

	rig.typeText("/!")
	rig.requireScreenOmits("no matching commands")
	rig.requireScreenOmits("› ")
}

func TestInProcessTheConfigOpensInATerminalEditorOverSSH(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rig := newEditingRig(t, terminalEditor, "")
		configDirectory := t.TempDir()
		configPath := filepath.Join(configDirectory, "config.toml")
		rig.withConfigCommands(configDirectory, configPath)

		rig.typeText("/conf")
		rig.press(key.Key{Code: key.Enter})
		start := rig.held.awaitStart(t)
		if !start.launch.IsTerminal || rig.handover.releases != 1 {
			t.Fatalf("got %+v with %d releases, want a terminal editor handed the terminal", start.launch, rig.handover.releases)
		}
		if !slices.Contains(start.paths, configPath) {
			t.Fatalf("editor got paths %q, want the active config %q", start.paths, configPath)
		}
		if err := os.WriteFile(configPath, []byte("edited config"), 0o600); err != nil {
			t.Fatal(err)
		}
		rig.finish(nil)
		if rig.handover.claims != 1 || rig.app.isEditorAwaited() {
			t.Fatalf("editor handover finished with %d claims and awaited=%t", rig.handover.claims, rig.app.isEditorAwaited())
		}
		configRoot, err := os.OpenRoot(configDirectory)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = configRoot.Close() }()
		contents, err := configRoot.ReadFile("config.toml")
		if err != nil || string(contents) != "edited config" {
			t.Fatalf("config contents are %q, err %v", contents, err)
		}
		rig.typeText("typing again")
		rig.requireInput("typing again")
		rig.requireScreenContains("typing again")
	})
}

func TestInProcessTheConfigOpensInATerminalEditorFromTheOpeningPrompt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rig := newEditingRig(t, terminalEditor, "")
		configDirectory := t.TempDir()
		configPath := filepath.Join(configDirectory, "config.toml")
		rig.withConfigCommands(configDirectory, configPath)

		rig.app.acceptInitialInput(rig.input, rig.history, "/conf")
		start := rig.held.awaitStart(t)
		if !start.launch.IsTerminal || rig.handover.releases != 1 {
			t.Fatalf("got %+v with %d releases, want a terminal editor handed the terminal", start.launch, rig.handover.releases)
		}
		if !slices.Contains(start.paths, configPath) {
			t.Fatalf("editor got paths %q, want the active config %q", start.paths, configPath)
		}
		if err := os.WriteFile(configPath, []byte("edited config"), 0o600); err != nil {
			t.Fatal(err)
		}
		rig.finish(nil)
		if rig.handover.claims != 1 || rig.app.isEditorAwaited() {
			t.Fatalf("editor handover finished with %d claims and awaited=%t", rig.handover.claims, rig.app.isEditorAwaited())
		}
		configRoot, err := os.OpenRoot(configDirectory)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = configRoot.Close() }()
		contents, err := configRoot.ReadFile("config.toml")
		if err != nil || string(contents) != "edited config" {
			t.Fatalf("config contents are %q, err %v", contents, err)
		}
		rig.typeText("typing again")
		rig.requireInput("typing again")
		rig.requireScreenContains("typing again")
	})
}

func TestInProcessReplyCommandAndShortcutNeedNoExperimentalSetting(t *testing.T) {
	for name, invoke := range map[string]func(*editingRig){
		"command": func(rig *editingRig) {
			rig.typeText("/reply")
			rig.press(key.Key{Code: key.Enter})
		},
		"shortcut": func(rig *editingRig) {
			decoder := key.NewDecoder(bufio.NewReader(strings.NewReader("\x1b[46;5u")))
			keypress, err := decoder.Next()
			if err != nil {
				rig.t.Fatal(err)
			}
			if keypress != ctrlDot {
				rig.t.Fatalf("decoded shortcut as %+v, want %+v", keypress, ctrlDot)
			}
			rig.press(keypress)
		},
	} {
		t.Run(name, func(t *testing.T) {
			rig := newEditingRig(t, terminalEditor, "")
			rig.answer()
			rig.withReplyCommands()
			invoke(rig)
			start := rig.held.awaitStart(t)
			if !start.launch.IsTerminal || rig.handover.releases != 1 {
				t.Fatalf("got %+v with %d releases, want a terminal reply editor", start.launch, rig.handover.releases)
			}
			rig.writeDraft(start, "My inline reply.")
			rig.finish(nil)
			if got := rig.input.Text(); got != "My inline reply." {
				t.Errorf("got input %q, want the edited reply", got)
			}
			rig.requireScreenContains("My inline reply.")
			rig.requireDraftGone(start)
		})
	}
}

func TestInProcessReasoningTitlesAreHiddenOnlyOnScreenAndResumeTheSameWay(t *testing.T) {
	thoughts := []string{
		"**Title-only reasoning**",
		"**Title-only reasoning**",
		"**Title-only reasoning**",
		"**Title-only reasoning**",
		"**A hidden title**\n\nI am keeping the conversational reasoning.",
		"An ordinary thought with **inline bold** stays.",
	}
	entries := []replayEntry{{Event: &agent.Event{Kind: agent.UserMessageEvent, Text: "show title-only and conversational reasoning"}}}
	for _, thought := range thoughts {
		entries = append(entries, replayEntry{Event: &agent.Event{Kind: agent.ModelReasoningEvent, Text: thought}})
	}
	entries = append(entries, replayEntry{Event: &agent.Event{Kind: agent.ModelMessageEvent, Text: "Finished thinking."}})

	sessionsDirectory := t.TempDir()
	log, err := store.Create(sessionsDirectory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	liveRig := newThinkingRig(t, output.ReasoningMarkdown, false, replayColumns)
	liveRig.chat.recorder = record.New(log)
	live := streamThrough(t, liveRig, entries)
	liveScreen := shown(t, live, replayColumns)
	if !strings.Contains(liveScreen, strings.Repeat("Title-only reasoning\n", 4)) {
		t.Fatalf("title-only thoughts did not stay on four separate lines:\n%s", liveScreen)
	}
	if strings.Contains(liveScreen, "A hidden title") || !strings.Contains(liveScreen, "I am keeping the conversational reasoning.") {
		t.Fatalf("the conversational reasoning kept its title or lost its body:\n%s", liveScreen)
	}

	for _, entry := range entries {
		if err := liveRig.chat.recorder.Event(*entry.Event); err != nil {
			t.Fatal(err)
		}
	}
	name := log.Name()
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	stored, err := store.Read(sessionsDirectory, name)
	if err != nil {
		t.Fatal(err)
	}
	var recorded []string
	for _, event := range stored.Events {
		if event.Kind == agent.ModelReasoningEvent {
			recorded = append(recorded, event.Text)
		}
	}
	if !slices.Equal(recorded, thoughts) {
		t.Fatalf("recorded reasoning %q, want original %q", recorded, thoughts)
	}

	resumedEntries := make([]replayEntry, 0, len(stored.Events))
	for _, event := range stored.Events {
		eventCopy := event
		resumedEntries = append(resumedEntries, replayEntry{Event: &eventCopy})
	}
	resumed := replayInto(newThinkingRig(t, output.ReasoningMarkdown, false, replayColumns), resumedEntries)
	resumedScreen := shown(t, resumed, replayColumns)
	if !slices.Equal(visibleRows(strings.Split(liveScreen, "\n")), visibleRows(strings.Split(resumedScreen, "\n"))) {
		t.Fatalf("live reasoning differs from resume:\nlive:\n%s\nresumed:\n%s", liveScreen, resumedScreen)
	}
}

func (self *editingRig) screenText() string {
	self.t.Helper()
	return strings.Join(playScreenOfSize(self.t, self.drawn.String(), replayColumns, replayLines).text(), "\n")
}

func (self *editingRig) requireScreenContains(want string) {
	self.t.Helper()
	if got := self.screenText(); !strings.Contains(got, want) {
		self.t.Errorf("screen does not contain %q:\n%s", want, got)
	}
}

func (self *editingRig) requireScreenOmits(want string) {
	self.t.Helper()
	if got := self.screenText(); strings.Contains(got, want) {
		self.t.Errorf("screen contains %q:\n%s", want, got)
	}
}

func (self *editingRig) requireInput(want string) {
	self.t.Helper()
	if got := self.input.Text(); got != want {
		self.t.Errorf("input is %q, want %q", got, want)
	}
}

func (self *editingRig) requireInputPrefix(want string) {
	self.t.Helper()
	if got := self.input.Text(); !strings.HasPrefix(got, want) {
		self.t.Errorf("input is %q, want prefix %q", got, want)
	}
}
