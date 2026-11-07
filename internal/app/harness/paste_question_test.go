package harness

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/permission"
	"crdx.org/oh/pkg/question"
)

func pastedKeys(text string) []key.Key {
	keys := []key.Key{{Code: key.PasteStart}}
	for _, character := range text {
		if character == '\r' {
			keys = append(keys, key.Key{Code: key.Enter})
			continue
		}
		keys = append(keys, key.Key{Code: key.Rune, Value: character})
	}
	return append(keys, key.Key{Code: key.PasteEnd})
}

type pasteQuestionRig struct {
	chat    *App
	history *edit.History
	answer  chan error
}

func newPasteQuestionRig(t *testing.T) *pasteQuestionRig {
	t.Helper()

	var screenOutput strings.Builder
	chat := slashCommandFixture(t, caps.Read)
	chat.inputLine = edit.NewInput(nil)
	chat.screen = output.NewTerminalOfSize(&screenOutput, 60, replayLines)
	broker := question.NewBroker()
	t.Cleanup(broker.Open())
	chat.question.broker = broker

	return &pasteQuestionRig{
		chat:    chat,
		history: edit.NewHistory("", historyLimit),
		answer:  make(chan error, 1),
	}
}

func (self *pasteQuestionRig) ask(ctx context.Context) {
	go func() {
		self.answer <- hostNetworkApproval.ask(
			ctx, self.chat.question.broker, permission.Setting{Rule: permission.Ask}, "curl example.com",
		)
	}()
	synctest.Wait()
	self.chat.onQuestionChange()
}

func (self *pasteQuestionRig) press(keys ...key.Key) {
	for _, keypress := range keys {
		self.chat.handleKeypressAndShowInput(self.chat.inputLine, self.history, keypress)
	}
	synctest.Wait()
	self.chat.onQuestionChange()
}

func (self *pasteQuestionRig) requireUnanswered(t *testing.T) {
	t.Helper()

	select {
	case err := <-self.answer:
		t.Fatalf("a paste answered the question with %v", err)
	default:
	}
	if !self.chat.isAwaitingAnswer() {
		t.Fatal("the question no longer stands")
	}
}

func TestAPasteNeverAnswersAStandingQuestion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rig := newPasteQuestionRig(t)
		rig.ask(t.Context())

		rig.press(pastedKeys("y1\rtwo\rthree")...)

		rig.requireUnanswered(t)
		if text := rig.chat.inputLine.Text(); text != "" {
			t.Errorf("the paste reached the input as %q", text)
		}

		rig.press(key.Key{Code: key.Enter})
		if err := <-rig.answer; err != nil {
			t.Errorf("enter after the paste got %v, want the default allowed", err)
		}
	})
}

func TestAPasteBegunBeforeAQuestionStillReachesTheInput(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rig := newPasteQuestionRig(t)
		keys := pastedKeys("one\rtwo")

		rig.press(keys[:3]...)
		rig.ask(t.Context())
		rig.press(keys[3:]...)

		rig.requireUnanswered(t)
		if text := rig.chat.inputLine.Text(); text != "one\ntwo" {
			t.Errorf("got input %q, want the whole paste", text)
		}

		rig.press(key.Key{Code: key.Escape})
		if err := <-rig.answer; err == nil {
			t.Error("escape after the paste allowed the question")
		}
	})
}

func TestAPasteOutlivingItsQuestionIsStillDiscarded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rig := newPasteQuestionRig(t)
		ctx, cancel := context.WithCancel(t.Context())
		rig.ask(ctx)
		keys := pastedKeys("one\rtwo\r")

		rig.press(keys[:3]...)
		cancel()
		if err := <-rig.answer; err == nil {
			t.Fatal("the withdrawn question was allowed")
		}
		rig.press(keys[3:]...)

		if rig.chat.isAwaitingAnswer() {
			t.Error("the withdrawn question still stands")
		}
		if text := rig.chat.inputLine.Text(); text != "" {
			t.Errorf("the rest of the paste reached the input as %q", text)
		}
	})
}
