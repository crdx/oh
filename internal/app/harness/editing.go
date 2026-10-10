package harness

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"crdx.org/oh/internal/app/commands"
	"crdx.org/oh/internal/app/editor"
	"crdx.org/oh/internal/app/experimental"
	"crdx.org/oh/internal/app/feedback"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/painter"
	"crdx.org/oh/internal/app/tty"
	"crdx.org/oh/pkg/agent"
)

const (
	draftPattern    = "oh-draft-*.md"
	editingOutcomes = 8
)

var (
	errEditorUnderway = errors.New("an editor is already open")
	errNoTerminal     = errors.New("needs the interactive terminal")
)

type terminalHandover interface {
	IsHeld() bool
	Release()
	Claim() error
}

type heldTerminal struct {
	app *App
}

func (self heldTerminal) IsHeld() bool {
	return self.app.keypresses != nil && self.app.restoreTTY != nil
}

func (self heldTerminal) Release() {
	self.app.keypresses.Release()
	self.app.takeBackTTY()
}

func (self heldTerminal) Claim() error {
	restoreTTY, err := tty.Raw(self.app.getKeyboard(), os.Stdout)
	if err != nil {
		return err
	}
	self.app.restoreTTY = restoreTTY
	self.app.screen.BeginEditing()
	self.app.keypresses.Resume()

	return nil
}

type externalEditState struct {
	run      func(context.Context, editor.Launch, []string, editor.Terminal) error
	terminal terminalHandover
	outcomes chan editor.Outcome

	launch        editor.Launch
	draftPath     string
	draftOriginal string
	draftPurpose  painter.DraftPurpose
	isAwaited     bool
	stop          context.CancelFunc
	release       func()
	columns       int
	lines         int
}

func (self *App) editorOutcomes() chan editor.Outcome {
	if self.externalEdit.outcomes == nil {
		self.externalEdit.outcomes = make(chan editor.Outcome, editingOutcomes)
	}

	return self.externalEdit.outcomes
}

func (self *App) isEditorAwaited() bool {
	return self.externalEdit.isAwaited
}

func (self *App) isTerminalHandedOver() bool {
	return self.externalEdit.release != nil
}

func (self *App) resolveEditor() (editor.Launch, error) {
	var command editor.Command
	if self.editorConfig != nil {
		command = self.editorConfig.GetCommand()
	}

	return editor.Resolve(command, os.Getenv)
}

func (self *App) openEditor(paths []string) error {
	launch, err := self.resolveEditor()
	if err != nil {
		return err
	}

	if !launch.IsTerminal {
		self.startEditor(launch, paths, false)
		return nil
	}

	return self.handOverTerminal(launch, paths, "")
}

func (self *App) editDraft() {
	if self.isEditorAwaited() || self.inputLine == nil {
		return
	}

	err := self.startDraft()
	if err != nil {
		self.showFeedback(feedback.Command, feedback.Message{
			Text:   "The draft could not be edited: " + err.Error(),
			Status: agent.ErrorStatus,
		})
	}
}

func (self *App) startDraft() error {
	text := self.inputLine.Text()
	position := editor.PositionIn(self.inputLine.Runes(), self.inputLine.Cursor())
	return self.openDraft(text, position, painter.EditingTheDraft)
}

func (self *App) replyInEditor(text string) error {
	return self.replyInEditorAt(text, len([]rune(text)))
}

func (self *App) replyToLastAnswer() {
	if !self.isReplyOffered || self.isEditorAwaited() || self.inputLine == nil {
		return
	}

	err := self.startReplyToLastAnswer()
	if err != nil {
		self.showFeedback(feedback.Command, feedback.Message{
			Text:   "The reply could not be written: " + err.Error(),
			Status: agent.ErrorStatus,
		})
	}
}

func (self *App) startReplyToLastAnswer() error {
	message, found := self.getLastMessage()
	if !found {
		return commands.ErrNoModelMessage
	}

	quote := commands.QuoteReply(message)
	return self.replyInEditorAt(quote+self.inputLine.Text(), len([]rune(quote))+self.inputLine.Cursor())
}

func (self *App) replyInEditorAt(text string, cursor int) error {
	if self.inputLine == nil || !self.terminalHandover().IsHeld() {
		return fmt.Errorf("replying %w", errNoTerminal)
	}
	if self.isEditorAwaited() {
		return errEditorUnderway
	}

	return self.openDraft(text, editor.PositionIn([]rune(text), cursor), painter.WritingAReply)
}

func replyInEditor(toggles *experimental.Toggles, reply func(string) error) func(string) error {
	if !isReplyOffered(toggles) {
		return nil
	}

	return reply
}

func isReplyOffered(toggles *experimental.Toggles) bool {
	return toggles.IsEnabled(experimental.ReplyCommand)
}

func (self *App) openDraft(text string, position editor.Position, purpose painter.DraftPurpose) error {
	launch, err := self.resolveEditor()
	if err != nil {
		return err
	}
	launch.Position = position

	draft, err := os.CreateTemp("", draftPattern)
	if err != nil {
		return err
	}
	_, err = draft.WriteString(text)
	if closeErr := draft.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(draft.Name())
		return err
	}

	if launch.IsTerminal {
		err = self.handOverTerminal(launch, []string{draft.Name()}, draft.Name())
		if err != nil {
			_ = os.Remove(draft.Name())
			return err
		}
		self.externalEdit.draftOriginal = text
		self.externalEdit.draftPurpose = purpose
		return nil
	}

	self.externalEdit.draftPath = draft.Name()
	self.externalEdit.draftOriginal = text
	self.externalEdit.draftPurpose = purpose
	self.startEditor(launch, []string{draft.Name()}, true)

	return nil
}

func (self *App) startEditor(launch editor.Launch, paths []string, isAwaited bool) {
	ctx := context.Background()
	if isAwaited {
		var stop context.CancelFunc
		ctx, stop = context.WithCancel(ctx)
		self.externalEdit.launch = launch
		self.externalEdit.isAwaited = true
		self.externalEdit.stop = stop
	}

	self.runEditor(ctx, editor.Outcome{Launch: launch, Paths: paths, IsAwaited: isAwaited}, editor.Terminal{})
}

func (self *App) runEditor(ctx context.Context, outcome editor.Outcome, terminal editor.Terminal) {
	run := self.externalEdit.run
	if run == nil {
		run = func(ctx context.Context, launch editor.Launch, paths []string, terminal editor.Terminal) error {
			return launch.Run(ctx, paths, terminal)
		}
	}

	outcomes := self.editorOutcomes()
	go func() {
		outcome.Failure = run(ctx, outcome.Launch, outcome.Paths, terminal)
		select {
		case outcomes <- outcome:
		default:
		}
	}()
}

func (self *App) handOverTerminal(launch editor.Launch, paths []string, draftPath string) error {
	if !self.terminalHandover().IsHeld() {
		return fmt.Errorf("%s %w", launch.Name(), errNoTerminal)
	}
	if self.isEditorAwaited() {
		return errEditorUnderway
	}

	columns, lines := self.screen.Size()
	release := self.screen.Hold()
	self.terminalHandover().Release()

	self.externalEdit.launch = launch
	self.externalEdit.draftPath = draftPath
	self.externalEdit.isAwaited = true
	self.externalEdit.stop = func() {}
	self.externalEdit.release = release
	self.externalEdit.columns = columns
	self.externalEdit.lines = lines

	self.runEditor(
		context.Background(),
		editor.Outcome{Launch: launch, Paths: paths, IsAwaited: true},
		editor.Terminal{Input: self.getKeyboard(), Output: os.Stdout},
	)

	return nil
}

func (self *App) terminalHandover() terminalHandover {
	if self.externalEdit.terminal == nil {
		return heldTerminal{app: self}
	}

	return self.externalEdit.terminal
}

func (self *App) takeTerminalBack() error {
	if err := self.terminalHandover().Claim(); err != nil {
		return err
	}

	if columns, lines := self.screen.Size(); columns != self.externalEdit.columns || lines != self.externalEdit.lines {
		self.redraw()
	}

	return nil
}

func (self *App) editorEnded(outcome editor.Outcome) {
	if !outcome.IsAwaited {
		if outcome.Failure != nil {
			self.showEditorFailure("The editor failed: " + outcome.Failure.Error())
		}
		return
	}

	failureLead := "The editor failed: "
	if self.children.viewedConversation != "" {
		failureLead = "The pager failed: "
	}
	self.forgetViewedConversation()
	draftPath := self.externalEdit.draftPath
	draftOriginal := self.externalEdit.draftOriginal
	draftPurpose := self.externalEdit.draftPurpose
	release := self.externalEdit.release
	if release != nil {
		if err := self.takeTerminalBack(); err != nil && self.onFailure != nil {
			self.onFailure(err)
		}
	}
	self.externalEdit = externalEditState{run: self.externalEdit.run, terminal: self.externalEdit.terminal, outcomes: self.externalEdit.outcomes}

	switch {
	case draftPath == "":
		if outcome.Failure != nil {
			self.showEditorFailure(failureLead + outcome.Failure.Error())
		}
	case outcome.Failure != nil:
		_ = os.Remove(draftPath)
		if !errors.Is(outcome.Failure, context.Canceled) {
			self.showEditorFailure(draftPurpose.FailureLead() + outcome.Failure.Error())
		}
	default:
		self.takeDraft(draftPath, draftOriginal, draftPurpose)
	}

	if release != nil {
		if self.inputLine != nil {
			self.show(self.inputLine)
		}
		release()
	}
}

func (self *App) takeDraft(draftPath string, draftOriginal string, draftPurpose painter.DraftPurpose) {
	defer func() { _ = os.Remove(draftPath) }()

	content, err := os.ReadFile(draftPath) //nolint:gosec // the draft this session wrote
	if err != nil {
		self.showEditorFailure(draftPurpose.FailureLead() + err.Error())
		return
	}

	draft := strings.TrimRight(string(content), "\n")
	if draft == strings.TrimRight(draftOriginal, "\n") {
		return
	}
	self.inputLine.SetText(draft)
}

func (self *App) showEditorFailure(text string) {
	self.showFeedback(feedback.Command, feedback.Message{Text: text, Status: agent.ErrorStatus})
}

func (self *App) abandonDraft() {
	if self.externalEdit.stop != nil && !self.isTerminalHandedOver() {
		self.externalEdit.stop()
	}
}

func (self *App) endEditing() {
	if !self.isEditorAwaited() {
		return
	}

	self.abandonDraft()
	if self.externalEdit.draftPath != "" {
		_ = os.Remove(self.externalEdit.draftPath)
	}
	self.externalEdit = externalEditState{run: self.externalEdit.run, terminal: self.externalEdit.terminal, outcomes: self.externalEdit.outcomes}
}

func (self *App) isDraftEditedElsewhere() bool {
	return self.isEditorAwaited() && !self.isTerminalHandedOver() && self.externalEdit.draftPath != ""
}

func (self *App) editingRows(columns int) []string {
	if !self.isDraftEditedElsewhere() {
		return nil
	}

	return painter.RenderDraftEditing(self.externalEdit.launch.Name(), self.externalEdit.draftPurpose, columns)
}

func (self *App) applyWhileEditing(keypress key.Key) {
	if !isStopKey(keypress) {
		return
	}

	if !self.feedback.Dismiss() {
		self.abandonDraft()
	}
}

func isStopKey(keypress key.Key) bool {
	return keypress.Code == key.Escape ||
		keypress.Code == key.Rune && keypress.Value == 'd' && keypress.Mod == key.Ctrl
}
