package harness

import (
	"fmt"
	"slices"
	"strings"

	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/feedback"
	"crdx.org/oh/internal/app/interrupt"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/metrics"
	"crdx.org/oh/internal/app/tr"
	"crdx.org/oh/internal/app/width"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tasks"
)

const undoHintPadding = 2

type undoState struct {
	restoreConversation func() error
	restoreJournal      func() error
	message             string
	events              int
	metrics             metrics.Tracker
	notes               []agent.Note
	previousTurn        Turn
	blocker             string
	refusal             string
	isPending           bool
}

func (self *App) checkpointTurn(message string, wereNoticesPending bool) {
	self.undo = undoState{message: message}
	if message == "" {
		self.undo.blocker = tr.NoMessageBlocker
		return
	}
	if wereNoticesPending {
		self.undo.blocker = tr.NoticesBlocker
		return
	}
	if blocker := self.undoWorkBlocker(); blocker != "" {
		self.undo.blocker = blocker
		return
	}
	restoreConversation, err := self.agent.Checkpoint()
	if err != nil {
		self.undo.blocker = err.Error()
		return
	}
	restoreJournal, err := self.recorder.Checkpoint()
	if err != nil {
		self.undo.blocker = err.Error()
		return
	}
	self.undo.restoreConversation = restoreConversation
	self.undo.restoreJournal = restoreJournal
	self.undo.events = len(self.recordedEvents)
	self.undo.metrics = self.metrics
	self.undo.notes = slices.Clone(self.settledNotes)
	self.undo.previousTurn = self.currentTurn
}

func (self *App) undoWorkBlocker() string {
	if self.children.isBusy() || self.isHostCommandUnderway() {
		return tr.BackgroundBlocker
	}
	for _, task := range self.getTasks() {
		if task.State == tasks.Running || task.State == tasks.Queued || task.State == tasks.Stopping {
			return tr.BackgroundBlocker
		}
	}
	if self.hasUntoldPendingNotices() || self.currentTurn.HasInterjections() {
		return tr.PendingBlocker
	}
	return ""
}

func (self *App) undoBlocker() string {
	if self.undo.blocker != "" {
		return self.undo.blocker
	}
	if self.undo.restoreJournal == nil {
		return tr.UndoUnavailable
	}
	if blocker := self.undoWorkBlocker(); blocker != "" {
		return blocker
	}
	messages := 0
	for _, event := range self.recordedEvents[self.undo.events:] {
		switch event.Kind {
		case agent.UserMessageEvent:
			messages++
			if messages > 1 || event.Text != self.undo.message {
				return tr.NextMessageBlocker
			}
		case agent.ModelMessageEvent, agent.ModelReasoningEvent, agent.CacheRebuildEvent,
			agent.PrefixRewriteEvent, agent.RetryingEvent, agent.FailureEvent:
		case agent.InterruptionEvent:
			if !slices.Contains([]string{string(interrupt.ControlZ), string(interrupt.Escape), string(interrupt.ControlD)}, event.Name) {
				return tr.InterruptionBlocker
			}
		case agent.ToolCallRequestEvent, agent.ToolCallResultEvent:
			return tr.ToolCallBlocker
		case agent.StartupEvent, agent.SilentTurnEvent, agent.StateChangeEvent:
			return tr.StateChangeBlocker
		default:
			return tr.StateChangeBlocker
		}
	}
	return ""
}

func isControlKey(keypress key.Key, character rune) bool {
	return keypress.Code == key.Rune && keypress.Value == character && keypress.Mod == key.Ctrl
}

func (self *App) applyUndo(inputLine *edit.Input, keypress key.Key) bool {
	if keypress.Code == key.FocusIn || keypress.Code == key.FocusOut {
		return false
	}
	isPlainInput := !inputLine.IsPasting() && !inputLine.IsSearching() && !inputLine.IsPrefixPending()
	if isControlKey(keypress, 'c') && isPlainInput && self.undo.isPending {
		inputLine.BreakKeySequence()
		self.undo.isPending = false
		self.undo.refusal = ""
		return true
	}
	if !isPlainInput || !isControlKey(keypress, 'z') {
		self.undo.isPending = false
		self.undo.refusal = ""
		return false
	}
	inputLine.BreakKeySequence()
	self.undo.refusal = ""
	if self.takeBackQueuedMessage(inputLine) {
		return true
	}
	if blocker := self.undoBlocker(); blocker != "" {
		self.refuseUndo(blocker)
		return true
	}
	self.undo.isPending = true
	if self.currentTurn.Running() {
		self.cancelTurn(interrupt.ControlZ)
	} else {
		self.takeBackTurn(inputLine)
	}
	return true
}

func (self *App) takeBackQueuedMessage(inputLine *edit.Input) bool {
	if inputLine == nil {
		return false
	}
	message, isQueued := self.currentTurn.Interjections().TakeLast()
	if isQueued {
		restoreRecalledDraft(inputLine, message)
	}
	return isQueued
}

func restoreRecalledDraft(inputLine *edit.Input, message string) {
	if typedText := inputLine.Text(); typedText != "" {
		message += agent.InterjectionSeparator + typedText
	}
	inputLine.Reset()
	inputLine.SetText(message)
}

func (self *App) refuseUndo(blocker string) {
	if blocker == tr.UndoUnavailable {
		self.undo.refusal = tr.UndoUnavailable
		return
	}
	self.undo.refusal = fmt.Sprintf(tr.UndoRefusal, blocker)
}

func (self *App) undoHint(columns int) string {
	if self.undo.refusal != "" {
		return width.Elide(self.undo.refusal, max(columns-undoHintPadding, 0))
	}
	if self.undo.isPending {
		return tr.UndoProgress
	}
	return ""
}

func (self *App) clearUndoFailure() {
	if isUndoFailure(self.feedback.Message().Text) {
		self.feedback.Clear(feedback.System)
	}
}

func (self *App) failUndo(reason string) {
	self.queuedTurn.Drop()
	if self.currentTurn.Cancelled() {
		self.notifyFailure(fmt.Sprintf(tr.UndoFailureStopped, reason))
		return
	}
	self.notifyFailure(fmt.Sprintf(tr.UndoFailure, reason))
}

func isUndoFailure(text string) bool {
	return strings.HasPrefix(text, strings.TrimSuffix(tr.UndoFailure, "%s")) ||
		strings.HasPrefix(text, strings.TrimSuffix(tr.UndoFailureStopped, "%s"))
}

func (self *App) takeBackTurn(inputLine *edit.Input) bool {
	self.undo.isPending = false
	if blocker := self.undoBlocker(); blocker != "" {
		self.failUndo(blocker)
		return false
	}
	if err := self.undo.restoreJournal(); err != nil {
		self.failUndo(err.Error())
		return false
	}
	if err := self.undo.restoreConversation(); err != nil {
		self.failUndo(fmt.Sprintf(tr.ConversationRestoreReason, err.Error()))
		return false
	}
	restoreRecalledDraft(inputLine, self.undo.message)
	self.recordedEvents = self.recordedEvents[:self.undo.events]
	self.metrics = self.undo.metrics
	self.settledNotes = self.undo.notes
	self.currentTurn = self.undo.previousTurn
	self.queuedTurn.Drop()
	self.undo = undoState{}
	self.clearUndoFailure()
	self.redraw()
	self.showStorageWarnings()
	return true
}
