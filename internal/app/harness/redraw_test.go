package harness

import (
	"bytes"
	"strings"
	"testing"
	"testing/synctest"

	"crdx.org/oh/internal/app/ansi"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/permission"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/ask"
)

var controlL = key.Key{Code: key.Rune, Value: 'l', Mod: key.Ctrl}

func pressControlL(self *App) {
	self.handleKeypressAndShowInput(self.inputLine, nil, controlL)
}

func streamHalfAnAnswer(t *testing.T, redraw func(*App)) string {
	t.Helper()

	var screenOutput bytes.Buffer
	self := frameEdgeConversation(t, &screenOutput, replayLines)
	middle := len(longProse) / 2
	streamProse(self, agent.ModelMessageEvent, longProse[:middle])
	before := screenOutput.Len()
	redraw(self)
	streamProse(self, agent.ModelMessageEvent, longProse[middle:])

	if !strings.Contains(screenOutput.String()[before:], ansi.EraseScreen) {
		t.Fatal("expected the screen to be cleared and drawn again")
	}

	return screenOutput.String()
}

func TestControlLRedrawsAStreamingAnswer(t *testing.T) {
	pressed := streamHalfAnAnswer(t, pressControlL)
	redrawn := streamHalfAnAnswer(t, (*App).redraw)

	if pressed != redrawn {
		t.Errorf("ctrl+l drew something other than a redraw\npressed:\n%q\nredrawn:\n%q", pressed, redrawn)
	}
}

func askBeneathTwoCalls(t *testing.T, redraw func(*App)) string {
	t.Helper()

	var drawn string

	synctest.Test(t, func(t *testing.T) {
		var screenOutput bytes.Buffer
		self := frameEdgeConversation(t, &screenOutput, replayLines)
		requestTwoCalls(self)

		broker := ask.New()
		t.Cleanup(broker.Open())
		self.question.broker = broker

		go func() { _ = approveHostNetwork(t.Context(), broker, permission.Ask, tallQuestionCommand()) }()
		<-broker.Changes()
		self.onQuestionChange()
		self.show(self.inputLine)
		redraw(self)

		if !self.isAwaitingAnswer() {
			t.Fatal("expected the question to stand after the redraw")
		}

		drawn = screenOutput.String()

		broker.Current().Cancel()
		self.currentTurn.painter.Stop()
	})

	return drawn
}

func TestControlLRedrawsBeneathAQuestionWithoutAnsweringIt(t *testing.T) {
	pressed := askBeneathTwoCalls(t, pressControlL)
	redrawn := askBeneathTwoCalls(t, (*App).redraw)

	if pressed != redrawn {
		t.Errorf("ctrl+l drew something other than a redraw\npressed:\n%q\nredrawn:\n%q", pressed, redrawn)
	}
}

func TestAPastedFormFeedDrawsNothingAgain(t *testing.T) {
	var screenOutput bytes.Buffer
	self := frameEdgeConversation(t, &screenOutput, replayLines)

	self.handleKeypressAndShowInput(self.inputLine, nil, key.Key{Code: key.PasteStart})
	before := screenOutput.Len()
	pressControlL(self)
	self.handleKeypressAndShowInput(self.inputLine, nil, key.Key{Code: key.PasteEnd})

	if strings.Contains(screenOutput.String()[before:], ansi.EraseScreen) {
		t.Error("a form feed inside a paste redrew the screen")
	}
}
