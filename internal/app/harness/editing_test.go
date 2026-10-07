package harness

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/commands"
	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/editor"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/pkg/agent"
)

const (
	terminalEditorScreen = "\x1b[?1049h\x1b[H~ the editor's own screen ~\x1b[?1049l"
	editorTimeout        = 5 * time.Second
)

var (
	ctrlG              = key.Key{Code: key.Rune, Value: 'g', Mod: key.Ctrl}
	terminalEditor     = editor.Command{"vim"}
	graphicalEditor    = editor.Command{"subl", "--wait"}
	errEditorExitedBad = errors.New("exit status 1")
)

type editorStart struct {
	launch editor.Launch
	paths  []string
}

type heldEditor struct {
	started  chan editorStart
	finishes chan error
}

type fakeHandover struct {
	isHeld   bool
	releases int
	claims   int
}

func (self *fakeHandover) IsHeld() bool { return self.isHeld }
func (self *fakeHandover) Release()     { self.releases++ }
func (self *fakeHandover) Claim() error { self.claims++; return nil }

func holdEditors(self *App) (*heldEditor, *fakeHandover) {
	held := &heldEditor{started: make(chan editorStart, 1), finishes: make(chan error)}
	handover := &fakeHandover{isHeld: true}

	self.externalEdit.terminal = handover
	self.externalEdit.run = func(ctx context.Context, launch editor.Launch, paths []string, _ editor.Terminal) error {
		held.started <- editorStart{launch: launch, paths: paths}

		select {
		case err := <-held.finishes:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return held, handover
}

func (self *heldEditor) awaitStart(t *testing.T) editorStart {
	t.Helper()

	select {
	case start := <-self.started:
		return start
	case <-time.After(editorTimeout):
		t.Fatal("the editor never started")
		return editorStart{}
	}
}

func endHeldEditor(t *testing.T, self *App, inputLine *edit.Input) {
	t.Helper()

	select {
	case outcome := <-self.editorOutcomes():
		self.editorEnded(outcome)
		self.drawAfterEvent(inputLine)
	case <-time.After(editorTimeout):
		t.Fatal("the editor never ended")
	}
}

type editingRig struct {
	t        *testing.T
	app      *App
	drawn    *bytes.Buffer
	history  *edit.History
	input    *edit.Input
	held     *heldEditor
	handover *fakeHandover
}

func newEditingRig(t *testing.T, command editor.Command, draft string) *editingRig {
	t.Helper()

	return newEditingRigOfWidth(t, command, draft, replayColumns)
}

func newEditingRigOfWidth(t *testing.T, command editor.Command, draft string, columns int) *editingRig {
	t.Helper()

	drawn := &bytes.Buffer{}
	self := testConversation(t, drawn)
	self.screen = output.NewTerminalOfSize(drawn, columns, replayLines)
	self.editorConfig = editor.NewConfiguration(command)
	self.settleAccess()
	held, handover := holdEditors(self)

	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)
	inputLine.SetText(draft)
	self.inputLine = inputLine
	self.show(inputLine)

	return &editingRig{t: t, app: self, drawn: drawn, history: history, input: inputLine, held: held, handover: handover}
}

func (self *editingRig) press(keypress key.Key) {
	self.t.Helper()

	if !self.app.handleKeypressAndShowInput(self.input, self.history, keypress) {
		self.t.Fatalf("%+v ended the session", keypress)
	}
}

func (self *editingRig) typeText(text string) {
	self.t.Helper()

	for _, value := range text {
		self.press(key.Key{Code: key.Rune, Value: value})
	}
}

func (self *editingRig) finish(err error) {
	self.t.Helper()

	self.held.finishes <- err
	endHeldEditor(self.t, self.app, self.input)
}

func (self *editingRig) writeDraft(start editorStart, text string) {
	self.t.Helper()

	if len(start.paths) != 1 {
		self.t.Fatalf("the editor was handed %q, want the one draft", start.paths)
	}
	if err := os.WriteFile(start.paths[0], []byte(text), 0o600); err != nil {
		self.t.Fatal(err)
	}
}

func (self *editingRig) requireDraftGone(start editorStart) {
	self.t.Helper()

	if _, err := os.Stat(start.paths[0]); !errors.Is(err, os.ErrNotExist) {
		self.t.Errorf("the draft %s was left behind: %v", start.paths[0], err)
	}
}

func (self *editingRig) withConfigCommands(configDirectory string, configPath string) {
	self.t.Helper()

	systemCommands, err := commands.New(commands.Options{
		ConfigDir:  configDirectory,
		ConfigFile: configPath,
		Workspace:  work.At(self.t.TempDir()),
		OpenEditor: self.app.openEditor,
	})
	if err != nil {
		self.t.Fatal(err)
	}
	self.app.commands = fixtureRegistry(self.t, systemCommands)
}

func TestATerminalEditorTakesTheDraftAndGivesTheTerminalBack(t *testing.T) {
	rig := newEditingRig(t, terminalEditor, "a first thought")

	rig.press(ctrlG)
	start := rig.held.awaitStart(t)

	if !start.launch.IsTerminal || rig.handover.releases != 1 {
		t.Fatalf("got %+v with %d releases, want the terminal handed to vim", start.launch, rig.handover.releases)
	}
	content, err := os.ReadFile(start.paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "a first thought" {
		t.Errorf("the editor was handed %q, want the draft", content)
	}

	rig.writeDraft(start, "a better thought\nover two lines\n")
	rig.finish(nil)

	if got := rig.input.Text(); got != "a better thought\nover two lines" {
		t.Errorf("got the input %q, want what the editor saved", got)
	}
	if rig.handover.claims != 1 {
		t.Errorf("the terminal was claimed back %d times, want once", rig.handover.claims)
	}
	if rig.app.isEditorAwaited() {
		t.Error("the editor still stands after it ended")
	}
	rig.requireDraftGone(start)
}

func TestTheEditorOpensTheDraftWhereTheCursorStood(t *testing.T) {
	for name, command := range map[string]editor.Command{
		"a terminal editor":  terminalEditor,
		"a graphical editor": graphicalEditor,
	} {
		t.Run(name, func(t *testing.T) {
			rig := newEditingRig(t, command, "first line\nsecond liné here")
			rig.press(key.Key{Code: key.Left})
			rig.press(key.Key{Code: key.Left})
			rig.press(key.Key{Code: key.Left})
			rig.press(key.Key{Code: key.Left})
			rig.press(key.Key{Code: key.Left})

			rig.press(ctrlG)
			start := rig.held.awaitStart(t)

			if want := (editor.Position{Line: 2, Column: 12, ByteColumn: 13}); start.launch.Position != want {
				t.Errorf("got %+v, want the cursor's own line and column", start.launch.Position)
			}
			rig.finish(nil)
		})
	}
}

func TestNothingIsDrawnWhileATerminalEditorHoldsTheScreen(t *testing.T) {
	rig := newEditingRig(t, terminalEditor, "draft")

	rig.press(ctrlG)
	start := rig.held.awaitStart(t)
	before := rig.drawn.Len()

	rig.typeText("ignored")
	rig.app.screen.RefreshProgress()
	rig.app.show(rig.input)

	if drawn := rig.drawn.String()[before:]; drawn != "" {
		t.Errorf("drew %q over the editor", drawn)
	}

	rig.writeDraft(start, "draft")
	rig.finish(nil)
}

func TestATurnStreamingUnderATerminalEditorLandsAsIfNobodyHadEdited(t *testing.T) {
	edited := newEditingRig(t, terminalEditor, "draft")
	edited.app.agent = agent.New("", answeringProvider{answer: "An answer that arrived under the editor."}, nil)

	edited.press(ctrlG)
	start := edited.held.awaitStart(t)
	before := edited.drawn.Len()
	edited.app.start("are you there")
	edited.app.waitForCurrentTurn()
	if drawn := edited.drawn.String()[before:]; drawn != "" {
		t.Errorf("the turn drew %q over the editor", drawn)
	}
	edited.drawn.WriteString(terminalEditorScreen)
	edited.writeDraft(start, "an edited draft")
	edited.finish(nil)

	untouched := newEditingRig(t, terminalEditor, "draft")
	untouched.app.agent = agent.New("", answeringProvider{answer: "An answer that arrived under the editor."}, nil)
	untouched.app.start("are you there")
	untouched.app.waitForCurrentTurn()
	untouched.input.SetText("an edited draft")
	untouched.app.show(untouched.input)

	requireSameVisibleScreen(t, "a turn that arrived under the editor", untouched.drawn.String(), edited.drawn.String())
	requireNothingDrawnAboveTheScreen(t, "a turn that arrived under the editor", edited.drawn.String(), replayLines)
}

func TestAFailedTerminalEditorKeepsTheDraft(t *testing.T) {
	rig := newEditingRig(t, terminalEditor, "keep me")

	rig.press(ctrlG)
	start := rig.held.awaitStart(t)
	rig.writeDraft(start, "thrown away")
	rig.finish(errEditorExitedBad)

	if got := rig.input.Text(); got != "keep me" {
		t.Errorf("got the input %q, want the draft kept", got)
	}
	if rig.handover.claims != 1 {
		t.Errorf("the terminal was claimed back %d times, want once", rig.handover.claims)
	}
	rig.requireDraftGone(start)
}

func TestAGraphicalEditorIsAwaitedWithoutTakingTheTerminal(t *testing.T) {
	rig := newEditingRig(t, graphicalEditor, "draft")

	rig.press(ctrlG)
	start := rig.held.awaitStart(t)

	if start.launch.IsTerminal || rig.handover.releases != 0 {
		t.Fatalf("got %+v with %d releases, want subl run apart", start.launch, rig.handover.releases)
	}
	rig.typeText("x")
	rig.press(ctrlG)
	if got := rig.input.Text(); got != "draft" {
		t.Errorf("typing reached the input as %q while the draft was out being edited", got)
	}

	rig.writeDraft(start, "edited elsewhere")
	rig.finish(nil)

	if got := rig.input.Text(); got != "edited elsewhere" {
		t.Errorf("got the input %q, want what subl saved", got)
	}
	rig.requireDraftGone(start)
}

func TestAStopKeyAbandonsAGraphicalEdit(t *testing.T) {
	for name, keypress := range map[string]key.Key{
		"escape": {Code: key.Escape},
		"ctrl+d": {Code: key.Rune, Value: 'd', Mod: key.Ctrl},
	} {
		t.Run(name, func(t *testing.T) {
			rig := newEditingRig(t, graphicalEditor, "as it was")

			rig.press(ctrlG)
			start := rig.held.awaitStart(t)
			rig.writeDraft(start, "half done")
			rig.press(keypress)
			endHeldEditor(t, rig.app, rig.input)

			if got := rig.input.Text(); got != "as it was" {
				t.Errorf("got the input %q, want the draft as it was", got)
			}
			if !rig.app.feedback.IsEmpty() {
				t.Errorf("abandoning said %q, want nothing said", rig.app.feedback.Render(replayColumns, time.Now()))
			}
			rig.requireDraftGone(start)
		})
	}
}

func TestAConfigEditorFailureIsShownRatherThanWrittenOverTheScreen(t *testing.T) {
	rig := newEditingRig(t, graphicalEditor, "")
	configDirectory := t.TempDir()
	rig.withConfigCommands(configDirectory, filepath.Join(configDirectory, "config.toml"))

	rig.typeText("/conf")
	rig.press(key.Key{Code: key.Enter})
	start := rig.held.awaitStart(t)

	if rig.app.isEditorAwaited() {
		t.Error("the input waits on a graphical editor opened for the config")
	}
	if len(start.paths) != 2 {
		t.Errorf("got paths %q, want the config directory and file", start.paths)
	}

	rig.finish(errors.New("subl: Timeout waiting for detached instance to start"))

	if shown := style.Plain(strings.Join(rig.app.statusRows(replayColumns), "\n")); !strings.Contains(shown, "The editor failed: subl: Timeout waiting") {
		t.Errorf("got status %q, want the failure", shown)
	}
}

func TestAConfigOpenedInATerminalEditorHandsTheTerminalOver(t *testing.T) {
	rig := newEditingRig(t, terminalEditor, "")
	configDirectory := t.TempDir()
	rig.withConfigCommands(configDirectory, filepath.Join(configDirectory, "config.toml"))

	rig.typeText("/conf")
	rig.press(key.Key{Code: key.Enter})
	start := rig.held.awaitStart(t)

	if !rig.app.isTerminalHandedOver() || rig.handover.releases != 1 {
		t.Error("the terminal stayed with oh while vim edited the config")
	}
	if len(start.paths) != 2 {
		t.Errorf("got paths %q, want the config directory and file", start.paths)
	}

	rig.finish(nil)

	if rig.handover.claims != 1 || rig.app.isEditorAwaited() {
		t.Error("the terminal never came back from vim")
	}
}

func TestATerminalEditorIsRefusedWithoutATerminalToHandOver(t *testing.T) {
	rig := newEditingRig(t, terminalEditor, "")
	rig.handover.isHeld = false

	err := rig.app.openEditor([]string{"/nowhere"})
	if !errors.Is(err, errNoTerminal) || !strings.HasPrefix(err.Error(), "vim ") {
		t.Errorf("got %v, want vim refused for want of a terminal", err)
	}
}

func TestEndingTheSessionAbandonsAGraphicalEdit(t *testing.T) {
	rig := newEditingRig(t, graphicalEditor, "draft")

	rig.press(ctrlG)
	start := rig.held.awaitStart(t)
	rig.app.endEditing()

	if rig.app.isEditorAwaited() {
		t.Error("the edit outlived the session")
	}
	rig.requireDraftGone(start)
}

func TestGoldenEditingTheDraft(t *testing.T) {
	passes := map[string]func() string{
		"a terminal editor takes the draft": func() string {
			rig := newEditingRig(t, terminalEditor, "a first thought")
			rig.press(ctrlG)
			start := rig.held.awaitStart(t)
			rig.drawn.WriteString(terminalEditorScreen)
			rig.writeDraft(start, "a better thought\nover two lines\n")
			rig.finish(nil)
			return rig.drawn.String()
		},
		"a terminal editor that fails keeps the draft": func() string {
			rig := newEditingRig(t, terminalEditor, "keep me")
			rig.press(ctrlG)
			rig.held.awaitStart(t)
			rig.drawn.WriteString(terminalEditorScreen)
			rig.finish(errors.New("vim: exit status 1"))
			return rig.drawn.String()
		},
		"a turn arrives under a terminal editor": func() string {
			rig := newEditingRig(t, terminalEditor, "draft")
			rig.app.agent = agent.New("", answeringProvider{answer: "An answer that arrived under the editor."}, nil)
			rig.press(ctrlG)
			start := rig.held.awaitStart(t)
			rig.app.start("are you there")
			rig.app.waitForCurrentTurn()
			rig.drawn.WriteString(terminalEditorScreen)
			rig.writeDraft(start, "an edited draft")
			rig.finish(nil)
			return rig.drawn.String()
		},
		"a graphical editor is awaited": func() string {
			rig := newEditingRig(t, graphicalEditor, "draft")
			rig.press(ctrlG)
			rig.held.awaitStart(t)
			drawn := rig.drawn.String()
			rig.app.endEditing()
			return drawn
		},
		"a graphical editor is awaited on a narrow screen": func() string {
			rig := newEditingRigOfWidth(t, graphicalEditor, "draft", narrowColumns)
			rig.press(ctrlG)
			rig.held.awaitStart(t)
			drawn := rig.drawn.String()
			rig.app.endEditing()
			return drawn
		},
		"a graphical editor takes the draft": func() string {
			rig := newEditingRig(t, graphicalEditor, "draft")
			rig.press(ctrlG)
			start := rig.held.awaitStart(t)
			rig.writeDraft(start, "edited elsewhere")
			rig.finish(nil)
			return rig.drawn.String()
		},
		"a graphical editor is abandoned": func() string {
			rig := newEditingRig(t, graphicalEditor, "as it was")
			rig.press(ctrlG)
			start := rig.held.awaitStart(t)
			rig.writeDraft(start, "half done")
			rig.press(key.Key{Code: key.Escape})
			endHeldEditor(t, rig.app, rig.input)
			return rig.drawn.String()
		},
		"no editor can be found": func() string {
			t.Setenv("PATH", t.TempDir())
			t.Setenv("VISUAL", "")
			t.Setenv("EDITOR", "")
			t.Setenv("DISPLAY", "")
			t.Setenv("WAYLAND_DISPLAY", "")
			rig := newEditingRig(t, nil, "draft")
			rig.press(ctrlG)
			return rig.drawn.String()
		},
	}

	compareWithGolden(t, "editing-draft", ".ansi", passes)
	compareWithGolden(t, "editing-draft", ".screen", shownPasses(t, passes))
}

func TestGoldenEditingTheConfig(t *testing.T) {
	opened := func(command editor.Command, failure error) func() string {
		return func() string {
			rig := newEditingRig(t, command, "")
			configDirectory := t.TempDir()
			rig.withConfigCommands(configDirectory, filepath.Join(configDirectory, "config.toml"))
			rig.typeText("/conf")
			rig.press(key.Key{Code: key.Enter})
			rig.held.awaitStart(t)
			if command[0] == terminalEditor[0] {
				rig.drawn.WriteString(terminalEditorScreen)
			}
			rig.finish(failure)
			return rig.drawn.String()
		}
	}

	passes := map[string]func() string{
		"in a terminal editor":         opened(terminalEditor, nil),
		"in a terminal editor failing": opened(terminalEditor, errors.New("vim: exit status 1")),
		"in a graphical editor":        opened(graphicalEditor, nil),
		"in a graphical editor failing": opened(
			graphicalEditor,
			errors.New("subl: Timeout waiting for detached instance to start: No such file or directory"),
		),
	}

	compareWithGolden(t, "editing-config", ".ansi", passes)
	compareWithGolden(t, "editing-config", ".screen", shownPasses(t, passes))
}

type answeringProvider struct {
	quietProvider

	answer string
}

func (self answeringProvider) Send(_ context.Context, yield agent.Yield) (agent.Reply, error) {
	if yield(agent.Output{Kind: agent.ModelMessageEvent, Text: self.answer}) {
		yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true})
	}

	return agent.Reply{}, nil
}
