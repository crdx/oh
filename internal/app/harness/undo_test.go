package harness

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/pkg/agent"
)

func undoFixture(t *testing.T) (*App, *plainTurnProvider, *edit.Input, *edit.History, *bytes.Buffer) {
	t.Helper()
	return undoFixtureAtWidth(t, replayColumns)
}

func undoFixtureAtWidth(t *testing.T, columns int) (*App, *plainTurnProvider, *edit.Input, *edit.History, *bytes.Buffer) {
	t.Helper()
	drawn := &bytes.Buffer{}
	self := testConversation(t, drawn)
	self.screen = output.NewTerminalOfSize(drawn, columns, replayLines)
	provider := &plainTurnProvider{}
	self.agent = agent.New("", provider, nil)
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)
	self.inputLine = inputLine
	self.start("a mistaken message")
	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	return self, provider, inputLine, history, drawn
}

func undoKey(character rune) key.Key {
	return key.Key{Code: key.Rune, Value: character, Mod: key.Ctrl}
}

func TestTakingBackATurnRestoresTheMessageAndProviderWithoutRestarting(t *testing.T) {
	for _, undoBeforeFinish := range []bool{false, true} {
		t.Run(map[bool]string{false: "settled", true: "rapid keys"}[undoBeforeFinish], func(t *testing.T) {
			self, provider, inputLine, history, _ := undoFixture(t)
			originalAgent := self.agent
			if !undoBeforeFinish {
				self.finish()
			}
			self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
			if undoBeforeFinish {
				self.finish()
			}
			if self.agent != originalAgent || self.currentTurn.Running() || inputLine.Text() != "a mistaken message" || !self.feedback.IsEmpty() || self.undoHint(replayColumns) != "" {
				t.Fatalf("undo did not restore the draft in place: %q", inputLine.Text())
			}
			if len(provider.items) != 0 || self.metrics.TurnCount() != 0 {
				t.Fatalf("undo retained provider state or turn count: %s", provider.items)
			}
			for _, event := range self.recordedEvents {
				if event.Kind == agent.UserMessageEvent || event.Kind == agent.ModelMessageEvent {
					t.Fatalf("undo retained conversation event: %+v", event)
				}
			}
			inputLine.SetText("the corrected message")
			self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Enter})
			self.waitForCurrentTurn()
			if len(provider.items) != 2 || !strings.Contains(string(provider.items[0]), "the corrected message") {
				t.Fatalf("the replacement turn did not use the restored history: %s", provider.items)
			}
		})
	}
}

func TestControlCCancelsAPendingUndoAndIsSafeToRepeat(t *testing.T) {
	self, provider, inputLine, history, _ := undoFixture(t)
	self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
	for range 5 {
		if !self.handleKeypressAndShowInput(inputLine, history, undoKey('c')) {
			t.Fatal("ctrl+c closed the session")
		}
	}
	self.finish()
	if inputLine.Text() != "" || len(provider.items) == 0 || self.undo.isPending || self.undoHint(replayColumns) != "" {
		t.Fatal("ctrl+c did not cancel the pending undo")
	}
	inputLine.SetText("a disposable draft")
	for range 5 {
		self.handleKeypressAndShowInput(inputLine, history, undoKey('c'))
	}
	if inputLine.Text() != "" || len(provider.items) == 0 {
		t.Fatal("repeated ctrl+c did not clear only the draft")
	}
}

func TestTakingBackATurnRefusesToolsAndInterveningState(t *testing.T) {
	for _, kind := range []agent.Kind{agent.ToolCallRequestEvent, agent.ToolCallResultEvent, agent.StateChangeEvent, agent.Kind("task_listing"), agent.UserMessageEvent} {
		t.Run(string(kind), func(t *testing.T) {
			self, provider, inputLine, history, _ := undoFixture(t)
			self.recordedEvents = append(self.recordedEvents, agent.Event{Kind: kind, Text: "another message"})
			self.finish()
			self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
			if !strings.Contains(self.undoHint(replayColumns), "cannot undo") || len(provider.items) == 0 || inputLine.Text() != "" {
				t.Fatalf("unsafe undo was not refused: %s", self.undoHint(replayColumns))
			}
		})
	}
}

func TestPastingOrTypingCancelsAPendingUndo(t *testing.T) {
	for _, keypress := range []key.Key{{Code: key.PasteStart}, {Code: key.Rune, Value: 'x'}} {
		self, provider, inputLine, history, _ := undoFixture(t)
		self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
		self.handleKeypressAndShowInput(inputLine, history, keypress)
		self.finish()
		if self.undo.isPending || len(provider.items) == 0 {
			t.Fatal("typing or pasting did not cancel the pending undo")
		}
	}
}

func TestUndoRefusalKeepsTheInputOnTheSameRow(t *testing.T) {
	for _, columns := range []int{replayColumns, 38} {
		self, _, inputLine, history, drawn := undoFixtureAtWidth(t, columns)
		self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Escape})
		self.finish()
		self.recordedEvents = append(self.recordedEvents, agent.Event{Kind: agent.ToolCallRequestEvent})
		self.show(inputLine)
		before := playScreen(t, drawn.String(), columns).CursorRow()
		beforeRefusal := drawn.Len()
		self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
		if after := playScreen(t, drawn.String(), columns).CursorRow(); after != before {
			t.Fatalf("%d columns: the refusal moved the input from %d to %d", columns, before, after)
		}
		if !self.feedback.IsEmpty() || strings.Contains(drawn.String()[beforeRefusal:], "\x1b[3J") {
			t.Fatal("the refusal added feedback or cleared scrollback")
		}
		visible := strings.Join(visibleScreen(t, drawn.String(), columns), "\n")
		if hint := self.undoHint(columns); hint == "" || !strings.Contains(visible, hint) {
			t.Fatalf("%d columns: the refusal is missing:\n%s", columns, visible)
		}
		self.handleKeypressAndShowInput(inputLine, history, undoKey('c'))
		if after := playScreen(t, drawn.String(), columns).CursorRow(); after != before {
			t.Fatalf("%d columns: dismissing the refusal moved the input from %d to %d", columns, before, after)
		}
	}
}

func TestRapidUndoFinishesInOneTerminalUpdate(t *testing.T) {
	self, _, inputLine, history, drawn := undoFixture(t)
	self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
	beforeFinish := drawn.Len()
	self.finish()
	finish := drawn.String()[beforeFinish:]
	if updates := strings.Count(finish, "\x1b[?2026h"); updates != 1 {
		t.Fatalf("undo finished across %d terminal updates, want one", updates)
	}
	if resets := strings.Count(finish, "\x1b[3J"); resets != 1 {
		t.Fatalf("undo cleared scrollback %d times, want one", resets)
	}
}

func TestGoldenTakingBackATurnStaysInTheSameSession(t *testing.T) {
	passes := map[string]func() string{}
	screens := map[string]func() string{}
	for _, columns := range []int{replayColumns, 38} {
		for _, stage := range []string{"1 stopped", "2 restored", "3 refused"} {
			name := fmt.Sprintf("%d columns / %s", columns, stage)
			passes[name] = func() string {
				self, _, inputLine, history, drawn := undoFixtureAtWidth(t, columns)
				self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Escape})
				self.finish()
				self.show(inputLine)
				if stage == "1 stopped" {
					return drawn.String()
				}
				if stage == "3 refused" {
					self.recordedEvents = append(self.recordedEvents, agent.Event{Kind: agent.ToolCallRequestEvent})
				}
				self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
				return drawn.String()
			}
			screens[name] = func() string { return shown(t, passes[name](), columns) }
		}
	}
	compareWithGolden(t, "take-back-turn", ".ansi", passes)
	compareWithGolden(t, "take-back-turn", ".screen", screens)
}

func TestUndoLeavesSearchAndModePrefixCancellationAlone(t *testing.T) {
	for _, character := range []rune{'r', 'x'} {
		self, _, inputLine, history, _ := undoFixture(t)
		self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Rune, Value: character, Mod: key.Ctrl})
		if !inputLine.IsSearching() && !inputLine.IsPrefixPending() {
			t.Fatalf("ctrl+%c did not enter an input mode", character)
		}
		self.handleKeypressAndShowInput(inputLine, history, undoKey('c'))
		if self.currentTurn.Cancelled() || self.undo.isPending || inputLine.IsSearching() || inputLine.IsPrefixPending() {
			t.Fatalf("ctrl+c did not leave ctrl+%c without stopping the turn", character)
		}
		self.finish()
	}
}

func TestARefusedUndoDoesNotLeaveAnEmptyEnterArmed(t *testing.T) {
	self, _, inputLine, history, _ := undoFixture(t)
	self.finish()
	self.recordedEvents = append(self.recordedEvents, agent.Event{Kind: agent.ToolCallRequestEvent})
	inputLine.TakeTimeFrom(func() time.Time { return time.Unix(100, 0) })
	if action := inputLine.Apply(key.Key{Code: key.Enter}, false); action != edit.DrawInput {
		t.Fatalf("first empty enter returned %v", action)
	}
	self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
	if action := inputLine.Apply(key.Key{Code: key.Enter}, false); action != edit.DrawInput {
		t.Fatalf("an enter before the refused undo survived it: %v", action)
	}
}

func TestControlZRecallsTheNewestQueuedMessageAtOnceAndPreservesTheDraft(t *testing.T) {
	self, _, inputLine, history, _ := undoFixture(t)
	self.currentTurn.Interject("older message")
	self.currentTurn.Interject("newest message")
	inputLine.SetText("current draft")
	self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
	if self.currentTurn.Cancelled() || inputLine.Text() != "newest message\n\ncurrent draft" {
		t.Fatalf("queued recall lost the draft or interrupted the turn: %q", inputLine.Text())
	}
	if self.undoHint(replayColumns) != "" {
		t.Fatalf("queued recall asked for confirmation: %q", self.undoHint(replayColumns))
	}
	remaining, _ := self.currentTurn.TakeInterjections()
	if remaining != "older message" {
		t.Fatalf("queued recall took the wrong message: %q", remaining)
	}
	self.finish()
}

func TestGoldenQueuedUndoRestoresTheDraftBeforeTheTurn(t *testing.T) {
	passes := map[string]func() string{}
	for _, stage := range []string{"1 restored", "2 turn taken back"} {
		passes[stage] = func() string {
			self, _, inputLine, history, drawn := undoFixture(t)
			self.currentTurn.Interject("queued correction")
			inputLine.SetText("unfinished draft")
			self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
			if stage == "2 turn taken back" {
				self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
			}
			self.finish()
			return drawn.String()
		}
	}
	compareWithGolden(t, "queued-undo", ".ansi", passes)
	compareWithGolden(t, "queued-undo", ".screen", shownPasses(t, passes))
}

func TestAnUnsafeDeferredUndoKeepsTheNewQueuedMessage(t *testing.T) {
	self, provider, inputLine, history, _ := undoFixture(t)
	self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
	self.currentTurn.Interject("new queued message")
	self.finish()
	if self.currentTurn.Running() || !self.currentTurn.HasInterjections() {
		t.Fatal("refused undo did not leave the new message pending")
	}
	if !strings.Contains(self.feedback.Message().Text, "undo failed; the turn remains stopped") {
		t.Fatal("refused undo did not explain that the turn remains stopped")
	}
	self.continueOrFlush(inputLine, history)
	if strings.Contains(self.feedback.Message().Text, "the turn remains stopped") {
		t.Fatal("starting a new turn retained the stopped-turn warning")
	}
	self.waitForCurrentTurn()
	if len(provider.items) < 4 || inputLine.Text() != "" {
		t.Fatalf("refused undo changed the draft or dropped provider history: %s", provider.items)
	}
}

func TestAFailedJournalUndoKeepsTheConversation(t *testing.T) {
	self, provider, inputLine, history, _ := undoFixture(t)
	self.undo.restoreJournal = func() error { return errors.New("disk unavailable") }
	self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
	self.finish()
	if len(provider.items) == 0 || inputLine.Text() != "" || self.undo.isPending {
		t.Fatal("failed undo changed the conversation")
	}
	if self.feedback.Message().Text != "undo failed; the turn remains stopped: disk unavailable" {
		t.Fatalf("failed undo did not explain the failure and stopped turn: %q", self.feedback.Message().Text)
	}
}

func TestAQueuedRecallBreaksTheDraftClearingSequence(t *testing.T) {
	self, _, inputLine, history, _ := undoFixture(t)
	self.currentTurn.Interject("queued")
	inputLine.SetText("draft")
	self.handleKeypressAndShowInput(inputLine, history, undoKey('c'))
	self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
	self.handleKeypressAndShowInput(inputLine, history, undoKey('c'))
	if inputLine.Text() != "queued\n\ndraft" {
		t.Fatalf("an old ctrl+c cleared the draft after the recall: %q", inputLine.Text())
	}
	self.finish()
}

func TestUndoStopsARunningTurnBeforeTakingItBack(t *testing.T) {
	self, _, inputLine, history, _ := undoFixture(t)
	self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
	if !self.currentTurn.Cancelled() || !self.undo.isPending || self.undoHint(replayColumns) != "undoing…" {
		t.Fatal("undo did not stop the turn before taking it back")
	}
	self.finish()
	if self.undo.isPending || inputLine.Text() != "a mistaken message" {
		t.Fatalf("undo did not take back the stopped turn: %q", inputLine.Text())
	}
}
