package harness

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/dropdown"
	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/feedback"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/pathref"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/trigger"
	"crdx.org/oh/pkg/ask"
)

var pathRefFixtureFiles = []string{
	"cmd/weather/main.go",
	"internal/app/harness/app.go",
	"internal/app/harness/main.go",
	"README.md",
}

func pathRefFixture(t *testing.T) (*App, *edit.Input) {
	t.Helper()

	self := slashCommandFixture(t, caps.Read)
	self.completer = trigger.New(pathref.NewSource(pathref.NewIndexWith(
		"/workspace",
		nil,
		func(context.Context, string, []string) ([]string, bool, error) {
			return pathRefFixtureFiles, false, nil
		},
		time.Now,
	)))

	return self, edit.NewInput(nil)
}

func typeIntoPathRef(t *testing.T, self *App, inputLine *edit.Input, text string) {
	t.Helper()

	for _, value := range text {
		pressForPathRef(self, inputLine, key.Key{Code: key.Rune, Value: value})
	}
}

func pressForPathRef(self *App, inputLine *edit.Input, keypress key.Key) {
	self.apply(inputLine, nil, keypress)
	self.syncCompleter(inputLine)
}

func awaitPathRefListing(t *testing.T, self *App, inputLine *edit.Input) {
	t.Helper()

	select {
	case <-self.triggerChanges():
	case <-time.After(5 * time.Second):
		t.Fatal("the listing never arrived")
	}
	self.receiveTriggerChange()
	self.syncCompleter(inputLine)
}

func selectedPathRef(self *App) string {
	selected, _ := self.completer.Selected()
	return selected.Text
}

func TestTypingTheSigilListsTheWorkspace(t *testing.T) {
	self, inputLine := pathRefFixture(t)

	typeIntoPathRef(t, self, inputLine, "look at @")
	if !self.completer.IsOpen() {
		t.Fatal("the dropdown did not open")
	}
	if rows := self.completer.Rows(80, dropdown.MaxRows); len(rows) != 1 || style.Plain(rows[0]) != "  listing files…" {
		t.Errorf("drew %q before the listing arrived", rows)
	}

	awaitPathRefListing(t, self, inputLine)
	if got := selectedPathRef(self); got != "cmd/" {
		t.Errorf("selected %q first", got)
	}
}

func TestChoosingAFileCompletesItAndClosesTheDropdown(t *testing.T) {
	self, inputLine := pathRefFixture(t)
	typeIntoPathRef(t, self, inputLine, "look at @")
	awaitPathRefListing(t, self, inputLine)

	typeIntoPathRef(t, self, inputLine, "harapp")
	if got := selectedPathRef(self); got != "internal/app/harness/app.go" {
		t.Fatalf("selected %q", got)
	}

	pressForPathRef(self, inputLine, key.Key{Code: key.Enter})
	if got := inputLine.Text(); got != "look at @internal/app/harness/app.go " {
		t.Errorf("completed to %q", got)
	}
	if self.completer.IsOpen() {
		t.Error("the dropdown stayed open after choosing a file")
	}
}

func TestChoosingAFileBeforeASpaceAddsNoSpace(t *testing.T) {
	self, inputLine := pathRefFixture(t)
	typeIntoPathRef(t, self, inputLine, "@ then")
	for range len(" then") {
		pressForPathRef(self, inputLine, key.Key{Code: key.Left})
	}
	pressForPathRef(self, inputLine, tabKey)
	awaitPathRefListing(t, self, inputLine)

	typeIntoPathRef(t, self, inputLine, "READ")
	pressForPathRef(self, inputLine, key.Key{Code: key.Enter})
	if got := inputLine.Text(); got != "@README.md then" {
		t.Errorf("completed to %q", got)
	}
	if self.completer.IsOpen() {
		t.Error("the dropdown reopened over the file just chosen")
	}
}

func TestRecallingAPathReferenceOpensNothingUntilTab(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	fixture, _ := pathRefFixture(t)
	self.completer = fixture.completer
	history := edit.NewHistory("", historyLimit)
	history.Add("look at @READ")
	inputLine := edit.NewInput(history)

	self.apply(inputLine, history, key.Key{Code: key.Up})
	self.syncCompleter(inputLine)
	if got := inputLine.Text(); got != "look at @READ" {
		t.Fatalf("recalled %q", got)
	}
	if self.completer.IsOpen() {
		t.Fatal("recalling a path reference opened the dropdown")
	}

	self.apply(inputLine, history, tabKey)
	awaitPathRefListing(t, self, inputLine)
	if got := inputLine.Text(); got != "look at @READ" {
		t.Errorf("tab changed the input to %q", got)
	}
	if got := selectedPathRef(self); got != "README.md" {
		t.Errorf("tab opened on %q", got)
	}
}

func TestTabAwayFromAPathReferenceIsLeftToTheInput(t *testing.T) {
	self, inputLine := pathRefFixture(t)
	typeIntoPathRef(t, self, inputLine, "@go plain")

	pressForPathRef(self, inputLine, tabKey)
	if self.completer.IsOpen() {
		t.Error("tab away from a path reference opened the dropdown")
	}
	if got := inputLine.Text(); got != "@go plain    " {
		t.Errorf("tab changed the input to %q", got)
	}
}

func TestChoosingADirectoryKeepsListingItsContents(t *testing.T) {
	self, inputLine := pathRefFixture(t)
	typeIntoPathRef(t, self, inputLine, "@")
	awaitPathRefListing(t, self, inputLine)

	typeIntoPathRef(t, self, inputLine, "harness")
	if got := selectedPathRef(self); got != "internal/app/harness/" {
		t.Fatalf("selected %q", got)
	}

	pressForPathRef(self, inputLine, key.Key{Code: key.Enter})
	if got := inputLine.Text(); got != "@internal/app/harness/" {
		t.Errorf("completed to %q", got)
	}
	if !self.completer.IsOpen() {
		t.Fatal("the dropdown closed after choosing a directory")
	}
	if got := selectedPathRef(self); got != "internal/app/harness/app.go" {
		t.Errorf("selected %q inside the directory", got)
	}
}

func TestTabChoosesTheSelectedPath(t *testing.T) {
	self, inputLine := pathRefFixture(t)
	typeIntoPathRef(t, self, inputLine, "@")
	awaitPathRefListing(t, self, inputLine)
	typeIntoPathRef(t, self, inputLine, "main")
	pressForPathRef(self, inputLine, key.Key{Code: key.Down})

	chosen := selectedPathRef(self)
	pressForPathRef(self, inputLine, tabKey)
	if got := inputLine.Text(); got != "@"+chosen+" " {
		t.Errorf("tab completed %q to %q", chosen, got)
	}
	if self.completer.IsOpen() {
		t.Error("the dropdown stayed open after tab chose a file")
	}
}

func TestEscapeDismissesTheDropdownUntilThePathReferenceEnds(t *testing.T) {
	self, inputLine := pathRefFixture(t)
	typeIntoPathRef(t, self, inputLine, "@")
	awaitPathRefListing(t, self, inputLine)

	pressForPathRef(self, inputLine, key.Key{Code: key.Escape})
	if self.completer.IsOpen() {
		t.Fatal("escape left the dropdown open")
	}

	typeIntoPathRef(t, self, inputLine, "main")
	if self.completer.IsOpen() {
		t.Error("typing reopened a dismissed dropdown")
	}

	typeIntoPathRef(t, self, inputLine, " @")
	if !self.completer.IsOpen() {
		t.Error("a new path reference did not reopen the dropdown")
	}
}

func TestEscapeDismissesFeedbackBeforeTheDropdown(t *testing.T) {
	self, inputLine := pathRefFixture(t)
	typeIntoPathRef(t, self, inputLine, "@")
	self.feedback.Show(feedback.Command, feedback.Message{Text: "Command not found: /unknown"}, time.Now())

	pressForPathRef(self, inputLine, key.Key{Code: key.Escape})
	if !self.feedback.IsEmpty() {
		t.Error("escape left the feedback standing")
	}
	if !self.completer.IsOpen() {
		t.Error("escape closed the dropdown along with the feedback")
	}
}

func TestASigilWithinAWordOpensNothing(t *testing.T) {
	self, inputLine := pathRefFixture(t)
	typeIntoPathRef(t, self, inputLine, "mail foo@bar")

	if self.completer.IsOpen() {
		t.Error("the dropdown opened within a word")
	}
}

func TestTheDropdownDrawsOnlyTheMatchingPaths(t *testing.T) {
	self, inputLine := pathRefFixture(t)
	typeIntoPathRef(t, self, inputLine, "@READ")
	awaitPathRefListing(t, self, inputLine)

	rows := self.completer.Rows(80, dropdown.MaxRows)
	if len(rows) != 1 || style.Plain(rows[0]) != "› README.md" {
		t.Errorf("drew %q", rows)
	}
}

var pathRefGoldenFiles = []string{
	".github/workflows/check.yml",
	"CHANGELOG.md",
	"Justfile",
	"README.md",
	"cmd/weather/main.go",
	"go.mod",
	"internal/app/dropdown/dropdown.go",
	"internal/app/edit/input.go",
	"internal/app/harness/app.go",
	"internal/app/harness/main.go",
	"internal/app/harness/pathref.go",
	"internal/app/pathref/index.go",
	"internal/app/pathref/match.go",
	"main.go",
}

type pathRefRig struct {
	t       *testing.T
	app     *App
	input   *edit.Input
	history *edit.History
	release chan struct{}
	output  *strings.Builder
}

type pathRefScenario struct {
	columns       int
	lines         int
	historyLines  []string
	listingErr    error
	files         []string
	isTurnRunning bool
	steps         func(rig *pathRefRig)
}

func newPathRefRig(t *testing.T, scenario pathRefScenario) *pathRefRig {
	t.Helper()

	rig := &pathRefRig{
		t:       t,
		app:     slashCommandFixture(t, caps.Read),
		history: edit.NewHistory("", historyLimit),
		release: make(chan struct{}),
		output:  &strings.Builder{},
	}
	for _, line := range scenario.historyLines {
		rig.history.Add(line)
	}
	rig.input = edit.NewInput(rig.history)
	rig.app.screen = output.NewTerminalOfSize(rig.output, scenario.columns, scenario.lines)
	if scenario.isTurnRunning {
		rig.app.currentTurn.Stream = testRunningTurnStream()
	}

	files := pathRefGoldenFiles
	if scenario.files != nil {
		files = scenario.files
	}

	clock := time.Unix(0, 0)
	rig.app.completer = trigger.New(pathref.NewSource(pathref.NewIndexWith(
		"/workspace",
		nil,
		func(context.Context, string, []string) ([]string, bool, error) {
			<-rig.release
			return files, false, scenario.listingErr
		},
		func() time.Time { return clock },
	)))

	return rig
}

func (self *pathRefRig) show() {
	self.app.show(self.input)
}

func (self *pathRefRig) press(keypresses ...key.Key) {
	for _, keypress := range keypresses {
		self.app.handleKeypressAndShowInput(self.input, self.history, keypress)
	}
}

func (self *pathRefRig) typeText(text string) {
	for _, value := range text {
		self.press(key.Key{Code: key.Rune, Value: value})
	}
}

func (self *pathRefRig) paste(text string) {
	self.press(key.Key{Code: key.PasteStart})
	self.typeText(text)
	self.press(key.Key{Code: key.PasteEnd})
}

func (self *pathRefRig) listingArrives() {
	self.t.Helper()

	close(self.release)
	select {
	case <-self.app.triggerChanges():
	case <-time.After(5 * time.Second):
		self.t.Fatal("the listing never arrived")
	}
	self.app.receiveTriggerChange()
	self.show()
}

func (self *pathRefRig) typeAndList(text string) {
	self.show()
	self.typeText(text)
	self.listingArrives()
}

func pathRefStream(t *testing.T, scenario pathRefScenario) string {
	t.Helper()

	rig := newPathRefRig(t, scenario)
	scenario.steps(rig)

	return rig.output.String()
}

var (
	pathRefEnter  = key.Key{Code: key.Enter}
	pathRefEscape = key.Key{Code: key.Escape}
	pathRefDown   = key.Key{Code: key.Down}
	pathRefUp     = key.Key{Code: key.Up}
	pathRefLeft   = key.Key{Code: key.Left}
	pathRefBack   = key.Key{Code: key.Backspace}
)

func pathRefScenarios(t *testing.T) map[string]pathRefScenario {
	t.Helper()

	at := func(columns int, lines int, steps func(rig *pathRefRig)) pathRefScenario {
		return pathRefScenario{columns: columns, lines: lines, steps: steps}
	}

	scenarios := map[string]pathRefScenario{
		"01 a sigil before the listing arrives": at(60, 24, func(rig *pathRefRig) {
			rig.show()
			rig.typeText("look at @")
			close(rig.release)
		}),
		"02 a sigil once the listing arrives": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("look at @")
		}),
		"03 a query typed before the listing arrives": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("look at @harn")
		}),
		"04 a query narrows the paths": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("look at @")
			rig.typeText("pathref")
		}),
		"05 down moves the selection": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("@pathref")
			rig.press(pathRefDown)
		}),
		"06 up from the top of a short list wraps to the last path": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("@pathref")
			rig.press(pathRefUp)
		}),
		"07 down and up move the selection": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("@pathref")
			rig.press(pathRefDown, pathRefDown, pathRefUp)
		}),
		"08 moving past the window scrolls it": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("@")
			for range dropdown.MaxRows + 2 {
				rig.press(pathRefDown)
			}
		}),
		"09 enter chooses a file and closes the dropdown": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("look at @harapp")
			rig.press(pathRefEnter)
		}),
		"10 enter chooses a file before a space": at(60, 24, func(rig *pathRefRig) {
			rig.show()
			rig.typeText("@ then")
			for range len(" then") {
				rig.press(pathRefLeft)
			}
			rig.press(tabKey)
			rig.listingArrives()
			rig.typeText("Just")
			rig.press(pathRefEnter)
		}),
		"11 enter chooses a directory and lists inside it": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("@harness")
			rig.press(pathRefEnter)
		}),
		"12 a query matching nothing": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("@zzz")
		}),
		"13 backspace over the sigil closes the dropdown": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("look at @")
			rig.press(pathRefBack)
		}),
		"14 escape dismisses the dropdown": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("look at @")
			rig.press(pathRefEscape)
		}),
		"15 typing into a closed path reference leaves it closed": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("look at @")
			rig.press(pathRefEscape)
			rig.typeText("main")
		}),
		"16 a new path reference reopens the dropdown": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("look at @")
			rig.press(pathRefEscape)
			rig.typeText("main @go")
		}),
		"17 escape clears feedback before the dropdown": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("look at @")
			rig.app.feedback.Show(feedback.Command, feedback.Message{Text: "Command not found: /unknown"}, time.Now())
			rig.show()
			rig.press(pathRefEscape)
		}),
		"18 a sigil within a word opens nothing": at(60, 24, func(rig *pathRefRig) {
			rig.show()
			rig.typeText("mail foo@bar")
			close(rig.release)
		}),
		"19 a listing that failed": {
			columns:    60,
			lines:      24,
			listingErr: errors.New("rg: not found"),
			steps: func(rig *pathRefRig) {
				rig.typeAndList("@zzz")
			},
		},
		"20 a narrow terminal keeps the tail of each path": at(20, 24, func(rig *pathRefRig) {
			rig.typeAndList("@pathref")
		}),
		"21 a path reference on the first of several lines": at(60, 24, func(rig *pathRefRig) {
			rig.show()
			rig.typeText("first line")
			rig.press(key.Key{Code: key.Enter, Mod: key.Shift})
			rig.typeText("second line")
			rig.press(key.Key{Code: key.Up}, key.Key{Code: key.End})
			rig.typeText(" @main")
			rig.listingArrives()
		}),
		"22 a terminal too short for the dropdown": at(60, 8, func(rig *pathRefRig) {
			rig.typeAndList("@")
		}),
		"23 a short terminal scrolls to keep the selection in view": at(60, 8, func(rig *pathRefRig) {
			rig.typeAndList("@")
			for range dropdown.MaxRows {
				rig.press(pathRefDown)
			}
		}),
		"24 a short terminal beneath the conversation": at(60, 8, func(rig *pathRefRig) {
			rig.app.screen.Line("said before")
			rig.typeAndList("@")
		}),
		"25 a short terminal beneath a queued message": {
			columns:       60,
			lines:         10,
			isTurnRunning: true,
			steps: func(rig *pathRefRig) {
				rig.app.currentTurn.Interject("check the tests too")
				rig.typeAndList("@")
			},
		},
		"26 ctrl+u clears the input and the dropdown": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("look at @")
			rig.press(key.Key{Code: key.Rune, Value: 'u', Mod: key.Ctrl})
		}),
		"27 a pasted path reference opens nothing": at(60, 24, func(rig *pathRefRig) {
			rig.show()
			rig.paste("@harn")
		}),
		"28 history search hides the dropdown": {
			columns:      60,
			lines:        24,
			historyLines: []string{"look at @README.md"},
			steps: func(rig *pathRefRig) {
				rig.typeAndList("@harn")
				rig.press(key.Key{Code: key.Rune, Value: 'r', Mod: key.Ctrl})
			},
		},
		"29 enter during a running turn chooses rather than queues": {
			columns:       60,
			lines:         24,
			isTurnRunning: true,
			steps: func(rig *pathRefRig) {
				rig.typeAndList("@harapp")
				rig.press(pathRefEnter)
			},
		},
		"30 a question hides the dropdown": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("look at @")
			askPathRefQuestion(t, rig)
		}),
		"31 answering the question brings the dropdown back": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("look at @")
			askPathRefQuestion(t, rig)
			rig.press(key.Key{Code: key.Rune, Value: 'y'})
		}),
		"32 widening a query grows the dropdown": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("@harn")
			rig.press(pathRefBack, pathRefBack, pathRefBack, pathRefBack)
		}),
		"33 a listing arriving after escape stays hidden": at(60, 24, func(rig *pathRefRig) {
			rig.show()
			rig.typeText("look at @")
			rig.press(pathRefEscape)
			rig.listingArrives()
		}),
		"34 moving onto another path reference closes the dropdown": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("@harn @")
			rig.press(pathRefLeft, pathRefLeft)
		}),
		"35 tab opens the dropdown over a pasted path reference": at(60, 24, func(rig *pathRefRig) {
			rig.show()
			rig.paste("@harn")
			rig.press(tabKey)
			rig.listingArrives()
		}),
		"36 tab opens the dropdown over the path reference the cursor moved onto": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("@harn @")
			rig.press(pathRefLeft, pathRefLeft, tabKey)
		}),
		"37 recalling a path reference opens nothing": {
			columns:      60,
			lines:        24,
			historyLines: []string{"look at @Justfile"},
			steps: func(rig *pathRefRig) {
				rig.show()
				rig.press(pathRefUp)
			},
		},
		"38 tab opens the dropdown over a recalled path reference": {
			columns:      60,
			lines:        24,
			historyLines: []string{"look at @Just"},
			steps: func(rig *pathRefRig) {
				rig.show()
				rig.press(pathRefUp, tabKey)
				rig.listingArrives()
			},
		},
		"39 tab away from a path reference is left to the input": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("@go plain")
			rig.press(tabKey)
		}),
		"40 up at the top of a long list stays put": {
			columns: 60,
			lines:   24,
			files:   longPathRefListing(),
			steps: func(rig *pathRefRig) {
				rig.typeAndList("@")
				rig.press(pathRefUp)
			},
		},
		"41 moving past the first page of a long list counts every path": {
			columns: 60,
			lines:   24,
			files:   longPathRefListing(),
			steps: func(rig *pathRefRig) {
				rig.typeAndList("@")
				for range 205 {
					rig.press(pathRefDown)
				}
			},
		},
		"42 the end of a long list stops rather than wrapping": {
			columns: 60,
			lines:   24,
			files:   longPathRefListing(),
			steps: func(rig *pathRefRig) {
				rig.typeAndList("@")
				for range 400 {
					rig.press(pathRefDown)
				}
			},
		},
		"43 tab chooses the selected path": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("look at @pathref")
			rig.press(pathRefDown, tabKey)
		}),
		"44 tab on a directory lists inside it": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("@pathref")
			rig.press(tabKey)
		}),
		"45 tab with nothing to choose does nothing": at(60, 24, func(rig *pathRefRig) {
			rig.show()
			rig.typeText("look at @")
			rig.press(tabKey)
			close(rig.release)
		}),
		"46 shift-tab does nothing while the dropdown is open": at(60, 24, func(rig *pathRefRig) {
			rig.typeAndList("look at @")
			rig.press(key.Key{Code: key.Rune, Value: '\t', Mod: key.Shift})
		}),
	}

	return scenarios
}

func longPathRefListing() []string {
	files := make([]string, 300)
	for i := range files {
		files[i] = fmt.Sprintf("notes/%03d.md", i)
	}
	return files
}

func askPathRefQuestion(t *testing.T, rig *pathRefRig) {
	t.Helper()

	broker := ask.New()
	closeBroker := broker.Open()
	t.Cleanup(closeBroker)

	go func() { _ = ask.Confirm(context.Background(), broker, ask.Confirmation{Label: "Continue?"}) }()
	<-broker.Changes()

	rig.app.question.broker = broker
	rig.app.onQuestionChange()
	rig.show()
}

func TestGoldenPathRefsDrawWhatTheyDrewBefore(t *testing.T) {
	scenarios := pathRefScenarios(t)
	passes := streamPasses(t, pathRefStream, scenarios)

	shownAt := map[string]func() string{}
	for name, scenario := range scenarios {
		shownAt[name] = func() string {
			return shown(t, passes[name](), scenario.columns)
		}
	}

	compareWithGolden(t, "pathrefs", ".ansi", passes)
	compareWithGolden(t, "pathrefs", ".screen", shownAt)
}

func TestAClosedDropdownLeavesTheScreenAsAFreshDrawWould(t *testing.T) {
	for name, steps := range map[string]func(rig *pathRefRig){
		"chosen file": func(rig *pathRefRig) {
			rig.typeAndList("look at @harapp")
			rig.press(pathRefEnter)
		},
		"escape": func(rig *pathRefRig) {
			rig.typeAndList("look at @")
			rig.press(pathRefEscape)
		},
		"backspace": func(rig *pathRefRig) {
			rig.typeAndList("look at @")
			rig.press(pathRefBack)
		},
	} {
		t.Run(name, func(t *testing.T) {
			rig := newPathRefRig(t, pathRefScenario{columns: replayColumns, lines: replayLines})
			steps(rig)

			fresh := newPathRefRig(t, pathRefScenario{columns: replayColumns, lines: replayLines})
			fresh.app.completer = nil
			fresh.input.SetText(rig.input.Text())
			fresh.show()

			requireSameVisibleScreenInColumns(
				t, "a closed dropdown left something behind", replayColumns, rig.output.String(), fresh.output.String(),
			)
		})
	}
}
