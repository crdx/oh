package harness

import (
	"bytes"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/key"
)

func TestControlCOnlyClearsTheDraftWithoutStoppingOrRecalling(t *testing.T) {
	for _, draft := range []string{"", "unfinished draft"} {
		self, _, inputLine, history, _ := undoFixture(t)
		self.currentTurn.Interject("queued message")
		inputLine.SetText(draft)
		for range 5 {
			self.handleKeypressAndShowInput(inputLine, history, undoKey('c'))
		}
		if self.currentTurn.Cancelled() || !self.currentTurn.Running() || inputLine.Text() != "" || self.undo.isPending {
			t.Fatal("ctrl+c did more than clear the draft")
		}
		queued, _ := self.currentTurn.TakeInterjections()
		if queued != "queued message" {
			t.Fatalf("ctrl+c changed the queued message: %q", queued)
		}
		self.finish()
	}
}

func TestControlCDoesNotStopAHostCommand(t *testing.T) {
	var drawn bytes.Buffer
	self := testConversation(t, &drawn)
	self.settleAccess()
	held := holdHostCommands(self)
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)
	if err := self.startHostCommand(t.TempDir(), "held command"); err != nil {
		t.Fatal(err)
	}
	<-held.started
	for range 5 {
		self.apply(inputLine, history, undoKey('c'))
	}
	if self.hostCommand.isStopping || !self.isHostCommandUnderway() {
		t.Fatal("ctrl+c stopped the host command")
	}
	self.apply(inputLine, history, key.Key{Code: key.Escape})
	endHeldHostCommand(t, self)
}

func TestEscapeClosesHelpBeforeStoppingTheTurn(t *testing.T) {
	self, _, inputLine, history, _ := undoFixture(t)
	self.currentTurn.Interject("queued message")
	inputLine.SetText("unfinished draft")
	self.handleKeypressAndShowInput(inputLine, history, shortcutHelpKey())
	self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Escape})
	if self.shortcutHelp.isOpen || self.currentTurn.Cancelled() || inputLine.Text() != "unfinished draft" {
		t.Fatal("the first escape did more than close help")
	}
	self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Escape})
	if !self.currentTurn.Cancelled() || inputLine.Text() != "unfinished draft" {
		t.Fatalf("the second escape did not stop the turn alone: %q", inputLine.Text())
	}
	self.finish()
}

func TestAStopKeyStopsTheTurnAndHoldsTheQueue(t *testing.T) {
	for name, stopKey := range map[string]key.Key{
		"escape": {Code: key.Escape},
		"ctrl+d": undoKey('d'),
	} {
		t.Run(name, func(t *testing.T) {
			self, provider, inputLine, history, _ := undoFixture(t)
			self.currentTurn.Interject("queued message")
			inputLine.SetText("unfinished draft")
			self.handleKeypressAndShowInput(inputLine, history, stopKey)
			if !self.currentTurn.Cancelled() || inputLine.Text() != "unfinished draft" {
				t.Fatalf("the key took the queue back rather than stop the turn: %q", inputLine.Text())
			}
			self.finish()
			if self.currentTurn.Running() {
				t.Fatal("the stop started another turn")
			}
			for _, item := range provider.items {
				if strings.Contains(string(item), "queued message") {
					t.Fatalf("the stop sent the queued message: %s", item)
				}
			}
			if queued := self.currentTurn.GetInterjections(); len(queued) != 1 || queued[0] != "queued message" {
				t.Fatalf("the stop lost the queued message: %q", queued)
			}
		})
	}
}

func TestEscapeStopsTheForegroundCommandBeforeTheTurn(t *testing.T) {
	self, _, inputLine, history, _ := undoFixture(t)
	held := holdHostCommands(self)
	if err := self.startHostCommand(t.TempDir(), "held command"); err != nil {
		t.Fatal(err)
	}
	<-held.started
	self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Escape})
	if self.currentTurn.Cancelled() || !self.hostCommand.isStopping {
		t.Fatal("the first escape did not stop only the host command")
	}
	endHeldHostCommand(t, self)
	self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Escape})
	if !self.currentTurn.Cancelled() {
		t.Fatal("the second escape did not stop the turn")
	}
	self.finish()
}
