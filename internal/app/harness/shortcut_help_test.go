package harness

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/ask"
)

func shortcutHelpKey() key.Key {
	return key.Key{Code: key.Rune, Value: '/', Mod: key.Ctrl}
}

func TestShortcutHelpPreservesTheDraftAndNeverSendsIt(t *testing.T) {
	for _, keypress := range []key.Key{
		shortcutHelpKey(),
		{Code: key.Rune, Value: '?', Mod: key.Ctrl},
		{Code: key.Rune, Value: '/', Mod: key.Ctrl | key.Shift},
		{Code: key.Rune, Value: '_', Mod: key.Ctrl},
	} {
		self, _, inputLine, history, _ := undoFixture(t)
		inputLine.SetText("keep this draft")
		before := len(self.recordedEvents)
		self.handleKeypressAndShowInput(inputLine, history, keypress)
		if !self.shortcutHelp.isOpen {
			t.Fatalf("%+v did not open shortcut help", keypress)
		}
		self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Enter})
		self.handleKeypressAndShowInput(inputLine, history, undoKey('z'))
		self.handleKeypressAndShowInput(inputLine, history, undoKey('c'))
		if self.shortcutHelp.isOpen || inputLine.Text() != "keep this draft" || self.currentTurn.Cancelled() || len(self.recordedEvents) != before {
			t.Fatal("shortcut help changed the draft, turn or conversation")
		}
		self.finish()
	}
}

func TestShortcutHelpDoesNotOpenInsideAPaste(t *testing.T) {
	self, _, inputLine, history, _ := undoFixture(t)
	self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.PasteStart})
	self.handleKeypressAndShowInput(inputLine, history, shortcutHelpKey())
	if self.shortcutHelp.isOpen || !inputLine.IsPasting() {
		t.Fatal("shortcut help took over a paste")
	}
	self.finish()
}

func TestShortcutHelpNamesOnlyTheUndoTargetThatIsAvailable(t *testing.T) {
	self, _, inputLine, history, _ := undoFixture(t)
	self.recordedEvents = append(self.recordedEvents, agent.Event{Kind: agent.ToolCallRequestEvent})
	self.handleKeypressAndShowInput(inputLine, history, shortcutHelpKey())
	for _, shortcut := range self.keyboardShortcuts(inputLine) {
		if shortcut.keys == "^Z" && strings.Contains(shortcut.action, "last turn") {
			t.Fatal("help offered undo after a tool call")
		}
	}
	self.currentTurn.Interject("queued message")
	isFound := false
	for _, shortcut := range self.keyboardShortcuts(inputLine) {
		if strings.HasPrefix(shortcut.keys, "^Z") {
			isFound = strings.Contains(shortcut.action, "queued")
		}
	}
	if !isFound {
		t.Fatal("help omitted the safe queued-message recall")
	}
	self.currentTurn.TakeInterjections()
	self.finish()
}

func TestShortcutHelpDoesNotAnswerTheQuestionBeneathIt(t *testing.T) {
	self, _, inputLine, history, _ := undoFixture(t)
	broker := ask.New()
	closeBroker := broker.Open()
	defer closeBroker()
	result := make(chan error, 1)
	go func() { result <- ask.Confirm(t.Context(), broker, ask.Confirmation{Label: "Continue?"}) }()
	<-broker.Changes()
	self.question.broker = broker
	self.onQuestionChange()
	self.handleKeypressAndShowInput(inputLine, history, shortcutHelpKey())
	self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Enter})
	self.handleKeypressAndShowInput(inputLine, history, undoKey('c'))
	select {
	case err := <-result:
		t.Fatalf("shortcut help answered the question: %v", err)
	default:
	}
	self.handleKeypressAndShowInput(inputLine, history, undoKey('c'))
	if err := <-result; !errors.Is(err, ask.ErrDenied) {
		t.Fatalf("the question was not available after closing help: %v", err)
	}
	self.finish()
}

func TestGoldenShortcutHelpFitsAndScrollsWithoutChangingTheDraft(t *testing.T) {
	passes := map[string]func() string{}
	screens := map[string]func() string{}
	for _, scenario := range []struct {
		name    string
		columns int
		rows    int
		context string
	}{
		{name: "all shortcuts", columns: 100, rows: 70},
		{name: "short terminal", columns: 38, rows: 14},
		{name: "scrolled to end", columns: 38, rows: 14, context: "end"},
		{name: "history search", columns: 70, rows: 24, context: "search"},
		{name: "mode prefix", columns: 70, rows: 24, context: "mode"},
		{name: "approval", columns: 70, rows: 24, context: "question"},
		{name: "external editor", columns: 70, rows: 24, context: "editor"},
		{name: "running turn", columns: 70, rows: 24, context: "running"},
	} {
		name := fmt.Sprintf("%s / %d columns", scenario.name, scenario.columns)
		passes[name] = func() string {
			self, _, inputLine, history, drawn := undoFixtureAtWidth(t, scenario.columns)
			if scenario.context != "running" {
				self.finish()
			}
			drawn.Reset()
			self.screen = output.NewTerminalOfSize(drawn, scenario.columns, scenario.rows)
			switch scenario.context {
			case "search":
				self.handleKeypressAndShowInput(inputLine, history, undoKey('r'))
			case "mode":
				self.handleKeypressAndShowInput(inputLine, history, undoKey('x'))
			case "question":
				self.question.request = &ask.Request{Question: ask.Confirmation{Label: "Continue?"}.Question()}
			case "editor":
				self.externalEdit.isAwaited = true
			}
			self.handleKeypressAndShowInput(inputLine, history, shortcutHelpKey())
			if scenario.context == "end" {
				self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.End})
			}
			result := drawn.String()
			if scenario.context == "running" {
				self.finish()
			}
			return result
		}
		screens[name] = func() string { return shown(t, passes[name](), scenario.columns) }
	}
	compareWithGolden(t, "shortcut-help", ".ansi", passes)
	compareWithGolden(t, "shortcut-help", ".screen", screens)
}

func TestShortcutHelpThroughTheBinaryAcceptsBothSlashAndQuestionMark(t *testing.T) {
	rig := newInteractiveRig(t)
	conversation := rig.start("--yolo", "-m", "opencode-go/fake")
	conversation.waitFor(readyBanner)
	conversation.typeAndSettle("keep this draft")
	for _, sequence := range []string{"\x1f", "\x1b[47;5u", "\x1b[63;5u", "\x1b[47;6u"} {
		before := conversation.Count("Keyboard shortcuts")
		conversation.typeText(sequence)
		conversation.waitForCount("Keyboard shortcuts", before+1)
		conversation.requireShown("Quit from an empty draft")
		conversation.typeAndSettle(pressEnter + "\x1f")
		conversation.requireShown("keep this draft")
	}
	if sessions := rig.storedSessions(); len(sessions) != 0 {
		t.Fatal("opening shortcut help or pressing enter sent the draft")
	}
	conversation.typeText(clearInput)
	conversation.quit()
}

func TestShortcutHelpBreaksTheEmptyEnterSequence(t *testing.T) {
	self, _, inputLine, history, _ := undoFixture(t)
	self.finish()
	inputLine.TakeTimeFrom(func() time.Time { return time.Unix(100, 0) })
	inputLine.Apply(key.Key{Code: key.Enter}, false)
	self.handleKeypressAndShowInput(inputLine, history, shortcutHelpKey())
	self.handleKeypressAndShowInput(inputLine, history, undoKey('c'))
	if action := inputLine.Apply(key.Key{Code: key.Enter}, false); action != edit.DrawInput {
		t.Fatalf("shortcut help preserved an old empty enter: %v", action)
	}
}
