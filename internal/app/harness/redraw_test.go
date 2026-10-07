package harness

import (
	"bytes"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/internal/app/ansi"
	"crdx.org/oh/internal/app/feedback"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/output"
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
	self.feedback.Dismiss()
	self.show(self.inputLine)

	if !strings.Contains(screenOutput.String()[before:], ansi.EraseScreen) {
		t.Fatal("expected the screen to be cleared and drawn again")
	}

	return screenOutput.String()
}

func TestControlLRedrawsAStreamingAnswer(t *testing.T) {
	pressed := streamHalfAnAnswer(t, pressControlL)
	redrawn := streamHalfAnAnswer(t, (*App).redraw)

	requireSameVisibleScreenInColumns(t, "ctrl+l changed the streamed answer", replayColumns, pressed, redrawn)
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

		go func() { _ = approveHostNetwork(t.Context(), broker, permission.Ask, tallQuestionCommand(), "") }()
		<-broker.Changes()
		self.onQuestionChange()
		self.show(self.inputLine)
		redraw(self)
		if !self.feedback.IsEmpty() {
			t.Error("ctrl+l covered a standing question with timing feedback")
		}
		self.feedback.Dismiss()
		self.show(self.inputLine)

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

	requireSameVisibleScreenOfSize(t, "ctrl+l changed the standing question", replayColumns, questionLines, pressed, redrawn)
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

func TestGoldenSubmittedMessagesSurviveFullRedraw(t *testing.T) {
	passes := map[string]func() string{}
	columnsByName := map[string]int{
		"wide":       replayColumns,
		"narrow":     narrowColumns,
		"one column": oneColumn,
	}
	for name, columns := range columnsByName {
		passes[name] = func() string { return submittedMessageRedraw(t, columns, false) }
	}
	passes["resized wide to narrow"] = func() string { return submittedMessageRedraw(t, narrowColumns, true) }

	compareWithGolden(t, "submitted-message-redraw", ".ansi", passes)

	screens := map[string]func() string{}
	for name, pass := range passes {
		columns := columnsByName[name]
		if name == "resized wide to narrow" {
			columns = narrowColumns
		}
		screens[name] = func() string { return shown(t, pass(), columns) }
	}
	compareWithGolden(t, "submitted-message-redraw", ".screen", screens)
}

func submittedMessageRedraw(t *testing.T, columns int, isResized bool) string {
	t.Helper()

	initialColumns := columns
	if isResized {
		initialColumns = replayColumns
	}
	rig := newReplayRig(t, initialColumns)
	rig.chat.recordedEvents = []agent.Event{
		{Kind: agent.UserMessageEvent, Text: "**A long message** with 日本語 and 🐸 beside [a link](https://example.test/path), then more words to wrap.\n\n- a list item\n- another item"},
		{Kind: agent.ModelMessageEvent, Text: "Noted."},
		{Kind: agent.UserMessageEvent, Text: "A second message that ends in an unbrokenlongword."},
	}
	rig.chat.replay()
	initial := rig.drawn()
	rig.written.Reset()
	if isResized {
		rig.chat.screen = output.NewTerminalOfSize(rig.written, columns, replayLines).
			LinkPathsUnder(link.Roots{Workspace: rig.workspace.GetDir()})
	}
	rig.chat.redraw()
	redrawn := rig.drawn()
	if !isResized {
		requireSameVisibleScreenInColumns(t, "redrawing submitted messages changed their screen", columns, initial, redrawn)
	}
	requireNothingDrawnAboveTheScreen(t, "redrawing submitted messages", redrawn, replayLines)

	return redrawn
}

func TestControlLReportsCompletedRedrawTimeAndDismissesIt(t *testing.T) {
	var screenOutput bytes.Buffer
	self := frameEdgeConversation(t, &screenOutput, replayLines)
	base := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	self.now = func() time.Time { return base }

	pressControlL(self)
	if got := self.feedback.Message().Text; got != "Redrawn in 0ms" {
		t.Errorf("ctrl+l feedback = %q, want redraw time", got)
	}
	if got := self.nextRefresh(base); !got.Equal(base.Add(time.Second)) {
		t.Errorf("next refresh = %s, want feedback countdown tick", got)
	}
	self.feedback.ClearExpired(base.Add(redrawFeedbackDuration))
	if !self.feedback.IsEmpty() {
		t.Error("redraw feedback did not expire")
	}
}

func TestControlLLeavesStandingSystemWarningsVisible(t *testing.T) {
	var screenOutput bytes.Buffer
	self := frameEdgeConversation(t, &screenOutput, replayLines)
	self.showFeedback(feedback.System, feedback.Message{Text: "recording failed", Status: agent.ErrorStatus})
	pressControlL(self)

	if got := self.feedback.Message().Text; got != "recording failed" {
		t.Errorf("ctrl+l replaced a standing warning with %q", got)
	}
}

func TestRedrawElapsedUsesMillisecondsUntilOneSecond(t *testing.T) {
	for _, test := range []struct {
		elapsed time.Duration
		want    string
	}{
		{0, "0ms"},
		{35 * time.Millisecond, "35ms"},
		{999 * time.Millisecond, "999ms"},
		{1500 * time.Millisecond, "1s"},
	} {
		if got := redrawElapsed(test.elapsed); got != test.want {
			t.Errorf("redrawElapsed(%s) = %q, want %q", test.elapsed, got, test.want)
		}
	}
}

type redrawTimingWriter struct {
	bytes.Buffer

	onWrite func()
}

func (self *redrawTimingWriter) Write(data []byte) (int, error) {
	if self.onWrite != nil {
		self.onWrite()
		self.onWrite = nil
	}
	return self.Buffer.Write(data)
}

func (self *redrawTimingWriter) WriteString(text string) (int, error) {
	return self.Write([]byte(text))
}

func TestControlLMeasuresThroughTheTerminalWrite(t *testing.T) {
	var initialOutput bytes.Buffer
	self := frameEdgeConversation(t, &initialOutput, replayLines)
	base := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	current := base
	self.now = func() time.Time { return current }
	writer := &redrawTimingWriter{onWrite: func() { current = base.Add(42 * time.Millisecond) }}
	self.screen = output.NewTerminalOfSize(writer, replayColumns, replayLines)

	pressControlL(self)
	if got := self.feedback.Message().Text; got != "Redrawn in 42ms" {
		t.Errorf("feedback = %q, want time through the terminal write", got)
	}
}

func TestGoldenControlLShowsRedrawFeedbackOverConversation(t *testing.T) {
	columnsByName := map[string]int{
		"wide":   replayColumns,
		"narrow": narrowColumns,
	}
	passes := map[string]func() string{}
	screens := map[string]func() string{}
	for name, columns := range columnsByName {
		passes[name] = func() string { return controlLFeedbackStream(t, columns) }
		screens[name] = func() string { return shown(t, controlLFeedbackStream(t, columns), columns) }
	}
	compareWithGolden(t, "redraw-feedback", ".ansi", passes)
	compareWithGolden(t, "redraw-feedback", ".screen", screens)
}

func controlLFeedbackStream(t *testing.T, columns int) string {
	t.Helper()

	var screenOutput bytes.Buffer
	self := frameEdgeConversation(t, &screenOutput, replayLines)
	screenOutput.Reset()
	self.screen = output.NewTerminalOfSize(&screenOutput, columns, replayLines)
	self.now = func() time.Time { return time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC) }
	self.show(self.inputLine)
	pressControlL(self)
	if got := self.feedback.Message().Text; got != "Redrawn in 0ms" {
		t.Errorf("redraw feedback = %q", got)
	}
	requireNothingDrawnAboveTheScreen(t, "ctrl+l feedback", screenOutput.String(), replayLines)

	return screenOutput.String()
}
