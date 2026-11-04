package harness

import (
	"errors"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/interrupt"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/pkg/agent"
)

func TestUndoStopsAreRecordedWithoutDrawingTheRoutineNotice(t *testing.T) {
	self, _, inputLine, history, drawn := undoFixture(t)
	inputLine.SetText("keep this draft")
	self.undo.restoreJournal = func() error { return errors.New("disk unavailable") }
	self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
	self.finish()
	isRecorded := false
	for _, event := range self.recordedEvents {
		if event.Kind == agent.InterruptionEvent && event.Name == string(interrupt.ControlZ) {
			isRecorded = true
		}
	}
	if !isRecorded {
		t.Fatal("undo discarded its interruption reason after a failed restore")
	}
	if strings.Contains(style.Plain(drawn.String()), interrupt.Notice(interrupt.Event(interrupt.ControlZ))) {
		t.Fatal("undo drew its internal interruption notice")
	}
	if self.feedback.Message().Text != "undo failed; the turn remains stopped: disk unavailable" {
		t.Fatalf("unexpected failure: %q", self.feedback.Message().Text)
	}
	if inputLine.Text() != "keep this draft" || self.currentTurn.Running() {
		t.Fatal("failed undo changed the draft or restarted the turn")
	}
}

func TestFailedUndoOfACompletedTurnDoesNotClaimToHaveStoppedIt(t *testing.T) {
	self, _, inputLine, history, _ := undoFixture(t)
	self.finish()
	self.undo.restoreJournal = func() error { return errors.New("disk unavailable") }
	self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
	if self.feedback.Message().Text != "undo failed: disk unavailable" {
		t.Fatalf("completed-turn failure claimed an interruption: %q", self.feedback.Message().Text)
	}
}

func TestCancellingAPendingUndoLeavesQueuedInputPending(t *testing.T) {
	self, _, inputLine, history, _ := undoFixture(t)
	self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
	self.currentTurn.Interject("keep this queued")
	self.handleKeypressAndShowInput(inputLine, history, undoKey('c'))
	self.finish()
	if self.currentTurn.Running() || !self.currentTurn.HasInterjections() {
		t.Fatal("cancelling undo sent or lost queued input")
	}
	self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
	if inputLine.Text() != "keep this queued" {
		t.Fatalf("the queued input could not be recalled: %q", inputLine.Text())
	}
}

func TestGoldenFailedUndoExplainsTheFailureWithoutAnExtraStopNotice(t *testing.T) {
	passes := map[string]func() string{}
	for _, stage := range []string{"1 stopped failure", "2 completed failure", "3 queued input"} {
		passes[stage] = func() string {
			self, _, inputLine, history, drawn := undoFixture(t)
			inputLine.SetText("keep this draft")
			if stage == "2 completed failure" {
				self.finish()
			}
			self.undo.restoreJournal = func() error { return errors.New("disk unavailable") }
			self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
			if stage == "3 queued input" {
				self.currentTurn.Interject("new queued message")
			}
			if self.currentTurn.Running() {
				self.finish()
			}
			self.show(inputLine)
			return drawn.String()
		}
	}
	compareWithGolden(t, "undo-failure", ".ansi", passes)
	compareWithGolden(t, "undo-failure", ".screen", shownPasses(t, passes))
}

func TestSuccessfulUndoRetryClearsItsPreviousFailure(t *testing.T) {
	self, _, inputLine, history, _ := undoFixture(t)
	restoreJournal := self.undo.restoreJournal
	self.undo.restoreJournal = func() error { return errors.New("disk unavailable") }
	self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
	self.finish()
	self.undo.restoreJournal = restoreJournal
	self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
	if !self.feedback.IsEmpty() || inputLine.Text() != "a mistaken message" {
		t.Fatal("successful retry left its failure message or did not restore the draft")
	}
}
