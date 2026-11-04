package harness

import (
	"bytes"
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

func TestEscapeStopsBothTheTurnAndAConcurrentHostCommand(t *testing.T) {
	self, _, inputLine, history, _ := undoFixture(t)
	held := holdHostCommands(self)
	if err := self.startHostCommand(t.TempDir(), "held command"); err != nil {
		t.Fatal(err)
	}
	<-held.started
	self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Escape})
	if !self.currentTurn.Cancelled() || !self.hostCommand.isStopping {
		t.Fatal("escape left either the turn or the host command running")
	}
	endHeldHostCommand(t, self)
	self.finish()
}
