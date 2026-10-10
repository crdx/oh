package harness

import (
	"errors"
	"os"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/commands"
	"crdx.org/oh/internal/app/editor"
	"crdx.org/oh/internal/app/experimental"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/pkg/agent"
)

const (
	repliedAnswer = "First paragraph.\n\nSecond paragraph."
	quotedAnswer  = "> First paragraph.\n>\n> Second paragraph.\n\n"
)

func (self *editingRig) withReplyCommand() {
	self.t.Helper()

	self.app.recordedEvents = append(self.app.recordedEvents, agent.Event{Kind: agent.ModelMessageEvent, Text: repliedAnswer})
	self.withReplyToggle(true)
}

func (self *editingRig) withReplyToggle(isEnabled bool) {
	self.t.Helper()

	toggles := experimental.New(map[string]any{string(experimental.ReplyCommand): isEnabled})
	self.app.isReplyOffered = isReplyOffered(toggles)
	systemCommands, err := commands.New(commands.Options{
		Workspace:     work.At(self.t.TempDir()),
		ReplyInEditor: replyInEditor(toggles, self.app.replyInEditor),
		Session:       commands.Session{GetLastMessage: self.app.getLastMessage},
	})
	if err != nil {
		self.t.Fatal(err)
	}
	self.app.commands = fixtureRegistry(self.t, systemCommands)
}

func (self *editingRig) answer() {
	self.t.Helper()

	self.app.agent = agent.New("", answeringProvider{answer: goldenAnswer}, nil)
	self.app.start("which should we do")
	self.app.waitForCurrentTurn()
	self.app.show(self.input)
}

var ctrlDot = key.Key{Code: key.Rune, Value: '.', Mod: key.Ctrl}

func (self *editingRig) reply() editorStart {
	self.t.Helper()

	self.typeText("/reply")
	self.press(key.Key{Code: key.Enter})
	return self.held.awaitStart(self.t)
}

func TestAReplyOpensTheQuotedAnswerWithTheCursorBeneathIt(t *testing.T) {
	for name, command := range map[string]editor.Command{
		"a terminal editor":  terminalEditor,
		"a graphical editor": graphicalEditor,
	} {
		t.Run(name, func(t *testing.T) {
			rig := newEditingRig(t, command, "")
			rig.withReplyCommand()

			start := rig.reply()

			content, err := os.ReadFile(start.paths[0])
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != quotedAnswer {
				t.Errorf("the editor was handed %q, want %q", content, quotedAnswer)
			}
			if want := (editor.Position{Line: 5, Column: 1, ByteColumn: 1}); start.launch.Position != want {
				t.Errorf("got %+v, want the empty line beneath the quote", start.launch.Position)
			}
			if got := rig.input.Text(); got != "" {
				t.Errorf("got the input %q while the reply was out being edited", got)
			}

			reply := "> First paragraph.\n\nAgreed.\n\n> Second paragraph.\n\nNot so sure.\n"
			rig.writeDraft(start, reply)
			rig.finish(nil)

			if got := rig.input.Text(); got != strings.TrimRight(reply, "\n") {
				t.Errorf("got the input %q, want the reply", got)
			}
			rig.requireDraftGone(start)
		})
	}
}

func TestAReplyLeftUntouchedLeavesTheInputEmpty(t *testing.T) {
	rig := newEditingRig(t, terminalEditor, "")
	rig.withReplyCommand()

	start := rig.reply()
	rig.writeDraft(start, strings.TrimRight(quotedAnswer, "\n")+"\n")
	rig.finish(nil)

	if got := rig.input.Text(); got != "" {
		t.Errorf("got the input %q, want nothing for a reply nobody wrote", got)
	}
	rig.requireDraftGone(start)
}

func TestAReplyAbandonedInATerminalEditorLeavesTheInputEmpty(t *testing.T) {
	rig := newEditingRig(t, terminalEditor, "")
	rig.withReplyCommand()

	start := rig.reply()
	rig.writeDraft(start, "half a reply")
	rig.finish(errEditorExitedBad)

	if got := rig.input.Text(); got != "" {
		t.Errorf("got the input %q, want nothing from an editor that failed", got)
	}
	rig.requireDraftGone(start)
}

func TestAReplyIsRefusedWithoutAnInteractiveTerminal(t *testing.T) {
	rig := newEditingRig(t, graphicalEditor, "")
	rig.handover.isHeld = false

	if err := rig.app.replyInEditor(quotedAnswer); err == nil || err.Error() != "replying needs the interactive terminal" {
		t.Errorf("got error %v, want the terminal named", err)
	}
}

func TestAReplyIsRefusedWhileAnEditorIsOpen(t *testing.T) {
	rig := newEditingRig(t, graphicalEditor, "draft")
	rig.withReplyCommand()

	rig.press(ctrlG)
	start := rig.held.awaitStart(t)

	if err := rig.app.replyInEditor(quotedAnswer); !errors.Is(err, errEditorUnderway) {
		t.Errorf("got error %v, want the open editor named", err)
	}
	rig.finish(nil)
	rig.requireDraftGone(start)
}

func TestAReplyIsRefusedWhenNoAnswerHasArrived(t *testing.T) {
	rig := newEditingRig(t, terminalEditor, "")
	rig.withReplyCommand()
	rig.app.recordedEvents = nil

	rig.typeText("/reply")
	rig.press(key.Key{Code: key.Enter})

	shown := style.Plain(strings.Join(rig.app.feedback.Render(replayColumns, rig.app.getNow()), "\n"))
	if !strings.Contains(shown, "/reply: No model message has been received yet") {
		t.Errorf("got status %q, want the missing answer named", shown)
	}
	if got := rig.input.Text(); got != "/reply" {
		t.Errorf("got the input %q, want the command kept", got)
	}
}

func TestReplyIsOfferedOnlyUnderItsExperimentalToggle(t *testing.T) {
	reply := func(string) error { return nil }

	if replyInEditor(experimental.New(nil), reply) != nil {
		t.Error("/reply was offered with its toggle absent")
	}
	if replyInEditor(experimental.New(map[string]any{string(experimental.ReplyCommand): false}), reply) != nil {
		t.Error("/reply was offered with its toggle off")
	}
	if replyInEditor(experimental.New(map[string]any{string(experimental.ReplyCommand): true}), reply) == nil {
		t.Error("/reply was withheld with its toggle on")
	}
}

const goldenAnswer = "Two options stand out.\n\n" +
	"- Keep the cache, which costs memory but saves a rebuild every time the session resumes after a long gap.\n" +
	"- Drop it.\n\n" +
	"```go\nreturn nil\n```\n\n" +
	"Which do you prefer?"

func TestGoldenReplyingToTheLastAnswer(t *testing.T) {
	replied := func(command editor.Command, columns int, write func(*editingRig, editorStart), failure error) func() string {
		return func() string {
			rig := newEditingRigOfWidth(t, command, "", columns)
			rig.withReplyToggle(true)
			rig.answer()
			rig.typeText("/reply")
			rig.press(key.Key{Code: key.Enter})
			start := rig.held.awaitStart(t)
			if command[0] == terminalEditor[0] {
				rig.drawn.WriteString(terminalEditorScreen)
			}
			write(rig, start)
			rig.finish(failure)
			return rig.drawn.String()
		}
	}
	answered := func(rig *editingRig, start editorStart) {
		rig.writeDraft(start, strings.Replace(quoteOf(t, start), "> - Drop it.\n", "> - Drop it.\n\nDrop it, the memory matters more.\n\n", 1)+"Thanks.\n")
	}
	untouched := func(*editingRig, editorStart) {}
	pressed := func(draft string) func() string {
		return func() string {
			rig := newEditingRig(t, terminalEditor, draft)
			rig.withReplyToggle(true)
			rig.answer()
			rig.press(ctrlDot)
			start := rig.held.awaitStart(t)
			rig.drawn.WriteString(terminalEditorScreen)
			rig.writeDraft(start, strings.Replace(quoteOf(t, start), "> Which do you prefer?\n", "> Which do you prefer?\n\nThe second.\n", 1))
			rig.finish(nil)
			return rig.drawn.String()
		}
	}
	awaited := func(columns int) func() string {
		return func() string {
			rig := newEditingRigOfWidth(t, graphicalEditor, "", columns)
			rig.withReplyToggle(true)
			rig.answer()
			rig.typeText("/reply")
			rig.press(key.Key{Code: key.Enter})
			rig.held.awaitStart(t)
			drawn := rig.drawn.String()
			rig.app.endEditing()
			return drawn
		}
	}

	passes := map[string]func() string{
		"a terminal editor takes the reply":                    replied(terminalEditor, replayColumns, answered, nil),
		"a terminal editor takes the reply on a narrow screen": replied(terminalEditor, narrowColumns, answered, nil),
		"a terminal editor leaves the reply untouched":         replied(terminalEditor, replayColumns, untouched, nil),
		"a terminal editor that fails drops the reply":         replied(terminalEditor, replayColumns, answered, errors.New("vim: exit status 1")),
		"a graphical editor takes the reply":                   replied(graphicalEditor, replayColumns, answered, nil),
		"a graphical editor leaves the reply untouched":        replied(graphicalEditor, replayColumns, untouched, nil),
		"a graphical editor is awaited":                        awaited(replayColumns),
		"a graphical editor is awaited on a narrow screen":     awaited(narrowColumns),
		"a graphical editor is abandoned": func() string {
			rig := newEditingRig(t, graphicalEditor, "")
			rig.withReplyToggle(true)
			rig.answer()
			rig.typeText("/reply")
			rig.press(key.Key{Code: key.Enter})
			start := rig.held.awaitStart(t)
			rig.writeDraft(start, "half a reply")
			rig.press(key.Key{Code: key.Escape})
			endHeldEditor(t, rig.app, rig.input)
			return rig.drawn.String()
		},
		"nothing has been answered yet": func() string {
			rig := newEditingRig(t, terminalEditor, "")
			rig.withReplyToggle(true)
			rig.typeText("/reply")
			rig.press(key.Key{Code: key.Enter})
			return rig.drawn.String()
		},
		"no editor can be found": func() string {
			t.Setenv("PATH", t.TempDir())
			t.Setenv("VISUAL", "")
			t.Setenv("EDITOR", "")
			t.Setenv("DISPLAY", "")
			t.Setenv("WAYLAND_DISPLAY", "")
			rig := newEditingRig(t, nil, "")
			rig.withReplyToggle(true)
			rig.answer()
			rig.typeText("/reply")
			rig.press(key.Key{Code: key.Enter})
			return rig.drawn.String()
		},
		"no terminal can be handed over": func() string {
			rig := newEditingRig(t, graphicalEditor, "")
			rig.handover.isHeld = false
			rig.withReplyToggle(true)
			rig.answer()
			rig.typeText("/reply")
			rig.press(key.Key{Code: key.Enter})
			return rig.drawn.String()
		},
		"ctrl+. takes the reply":                     pressed(""),
		"ctrl+. carries the draft beneath the quote": pressed("my own draft"),
		"ctrl+. with nothing answered yet": func() string {
			rig := newEditingRig(t, terminalEditor, "my own draft")
			rig.withReplyToggle(true)
			rig.press(ctrlDot)
			return rig.drawn.String()
		},
		"ctrl+. with the toggle off": func() string {
			rig := newEditingRig(t, terminalEditor, "my own draft")
			rig.withReplyToggle(false)
			rig.answer()
			rig.press(ctrlDot)
			return rig.drawn.String()
		},
		"the toggle is off": func() string {
			rig := newEditingRig(t, terminalEditor, "")
			rig.withReplyToggle(false)
			rig.answer()
			rig.typeText("/reply")
			rig.press(key.Key{Code: key.Enter})
			return rig.drawn.String()
		},
	}

	compareWithGolden(t, "editing-reply", ".ansi", passes)
	compareWithGolden(t, "editing-reply", ".screen", shownPasses(t, passes))
}

func quoteOf(t *testing.T, start editorStart) string {
	t.Helper()

	content, err := os.ReadFile(start.paths[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestControlDotRepliesAsTheCommandDoes(t *testing.T) {
	rig := newEditingRig(t, terminalEditor, "")
	rig.withReplyCommand()

	rig.press(ctrlDot)
	start := rig.held.awaitStart(t)

	if content := quoteOf(t, start); content != quotedAnswer {
		t.Errorf("the editor was handed %q, want %q", content, quotedAnswer)
	}
	if want := (editor.Position{Line: 5, Column: 1, ByteColumn: 1}); start.launch.Position != want {
		t.Errorf("got %+v, want the empty line beneath the quote", start.launch.Position)
	}
	rig.writeDraft(start, quotedAnswer+"Agreed.\n")
	rig.finish(nil)

	if got := rig.input.Text(); got != quotedAnswer+"Agreed." {
		t.Errorf("got the input %q, want the reply", got)
	}
	rig.requireDraftGone(start)
}

func TestControlDotCarriesTheDraftBeneathTheQuoteWithItsCursor(t *testing.T) {
	rig := newEditingRig(t, terminalEditor, "half a thought")
	rig.withReplyCommand()
	rig.press(key.Key{Code: key.Left})
	rig.press(key.Key{Code: key.Left})

	rig.press(ctrlDot)
	start := rig.held.awaitStart(t)

	if content := quoteOf(t, start); content != quotedAnswer+"half a thought" {
		t.Errorf("the editor was handed %q, want the quote above the draft", content)
	}
	if want := (editor.Position{Line: 5, Column: 13, ByteColumn: 13}); start.launch.Position != want {
		t.Errorf("got %+v, want the draft's own cursor", start.launch.Position)
	}
	rig.finish(nil)

	if got := rig.input.Text(); got != "half a thought" {
		t.Errorf("got the input %q, want the draft kept for a reply nobody wrote", got)
	}
	rig.typeText("X")
	if got := rig.input.Text(); got != "half a thougXht" {
		t.Errorf("got the input %q, want the cursor where it stood", got)
	}
	rig.requireDraftGone(start)
}

func TestControlDotDoesNothingWithItsToggleOff(t *testing.T) {
	rig := newEditingRig(t, terminalEditor, "draft")
	rig.withReplyCommand()
	rig.withReplyToggle(false)

	rig.press(ctrlDot)

	if rig.app.isEditorAwaited() {
		t.Error("ctrl+. opened an editor with /reply switched off")
	}
	if shown := rig.app.feedback.Render(replayColumns, rig.app.getNow()); len(shown) != 0 {
		t.Errorf("ctrl+. said %q with /reply switched off", shown)
	}
	if got := rig.input.Text(); got != "draft" {
		t.Errorf("got the input %q, want it untouched", got)
	}
}

func TestControlDotSaysWhyItCannotReply(t *testing.T) {
	for name, test := range map[string]struct {
		prepare func(*editingRig)
		want    string
	}{
		"nothing answered": {
			prepare: func(rig *editingRig) { rig.app.recordedEvents = nil },
			want:    "The reply could not be written: no model message has been received yet",
		},
		"no terminal": {
			prepare: func(rig *editingRig) { rig.handover.isHeld = false },
			want:    "The reply could not be written: replying needs the interactive terminal",
		},
	} {
		t.Run(name, func(t *testing.T) {
			rig := newEditingRig(t, terminalEditor, "draft")
			rig.withReplyCommand()
			test.prepare(rig)

			rig.press(ctrlDot)

			shown := style.Plain(strings.Join(rig.app.feedback.Render(replayColumns, rig.app.getNow()), "\n"))
			if !strings.Contains(strings.Join(strings.Fields(shown), " "), test.want) {
				t.Errorf("got status %q, want %q", shown, test.want)
			}
			if got := rig.input.Text(); got != "draft" {
				t.Errorf("got the input %q, want the draft kept", got)
			}
		})
	}
}
