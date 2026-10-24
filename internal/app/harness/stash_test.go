package harness

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/bar"
	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/stash"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/work"
)

var (
	stashKey = key.Key{Code: key.Rune, Value: 's', Mod: key.Ctrl}
	popKey   = key.Key{Code: key.Rune, Value: 's', Mod: key.Ctrl | key.Shift}
)

func TestStashingDuringATurnDoesNotQueueOrSendTheDraft(t *testing.T) {
	self := testConversation(t, &bytes.Buffer{})
	self.currentTurn.Stream = testRunningTurnStream()
	self.draftStash = stash.NewMemory()
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)
	inputLine.SetText("ask this later")

	if !self.apply(inputLine, history, stashKey) {
		t.Fatal("stashing ended the session")
	}

	if got := inputLine.Text(); got != "" {
		t.Errorf("stashing left input %q", got)
	}
	if self.draftStash.Count() != 1 {
		t.Errorf("got %d stashed drafts, want 1", self.draftStash.Count())
	}
	if self.currentTurn.HasInterjections() {
		t.Error("the stashed draft entered the turn queue")
	}

	recalled := edit.NewInput(history)
	recalled.Apply(key.Key{Code: key.Up}, true)
	if got := recalled.Text(); got != "" {
		t.Errorf("the stashed draft entered history as %q", got)
	}
}

func TestAStashWriteFailureKeepsTheDraft(t *testing.T) {
	self := testConversation(t, &bytes.Buffer{})
	failure := errors.New("disk full")
	var err error
	self.draftStash, err = stash.Open(t.TempDir(), func() error { return failure })
	if err != nil {
		t.Fatal(err)
	}
	inputLine := edit.NewInput(nil)
	inputLine.SetText("do not lose this")

	self.apply(inputLine, nil, stashKey)
	if got := inputLine.Text(); got != "do not lose this" {
		t.Errorf("failed stash changed the draft to %q", got)
	}
	if self.draftStash.Count() != 0 {
		t.Errorf("failed stash left %d entries", self.draftStash.Count())
	}
	if got := self.feedback.Message().Text; !strings.Contains(got, failure.Error()) {
		t.Errorf("failure feedback was %q", got)
	}
}

func TestStashedDraftsPopNewestFirst(t *testing.T) {
	self := testConversation(t, &bytes.Buffer{})
	self.draftStash = stash.NewMemory()
	inputLine := edit.NewInput(nil)

	for _, draft := range []string{"first", "second"} {
		inputLine.SetText(draft)
		self.apply(inputLine, nil, stashKey)
	}

	self.apply(inputLine, nil, popKey)
	if got := inputLine.Text(); got != "second" {
		t.Errorf("restored %q, want the newest draft", got)
	}
	if self.draftStash.Count() != 1 {
		t.Errorf("got %d drafts left, want 1", self.draftStash.Count())
	}

	inputLine.Reset()
	self.apply(inputLine, nil, popKey)
	if got := inputLine.Text(); got != "first" {
		t.Errorf("restored %q, want the oldest draft", got)
	}
	if self.draftStash.Count() != 0 {
		t.Errorf("got %d drafts left, want none", self.draftStash.Count())
	}
}

func TestPoppingWithADraftSwapsItIntoTheStash(t *testing.T) {
	self := testConversation(t, &bytes.Buffer{})
	self.draftStash = stash.NewMemory()
	if _, err := self.draftStash.Push("stashed"); err != nil {
		t.Fatal(err)
	}
	inputLine := edit.NewInput(nil)
	inputLine.SetText("current")

	self.apply(inputLine, nil, popKey)
	if got := inputLine.Text(); got != "stashed" {
		t.Errorf("restored %q, want the stashed draft", got)
	}
	if self.draftStash.Count() != 1 {
		t.Errorf("got %d drafts, want the current draft suspended", self.draftStash.Count())
	}

	inputLine.Reset()
	self.apply(inputLine, nil, popKey)
	if got := inputLine.Text(); got != "current" {
		t.Errorf("restored %q, want the suspended current draft", got)
	}
}

func TestControlSOnEmptyInputPopsTheStash(t *testing.T) {
	self := testConversation(t, &bytes.Buffer{})
	self.draftStash = stash.NewMemory()
	if _, err := self.draftStash.Push("come back to this"); err != nil {
		t.Fatal(err)
	}
	inputLine := edit.NewInput(nil)

	self.apply(inputLine, nil, stashKey)
	if got := inputLine.Text(); got != "come back to this" {
		t.Errorf("restored %q", got)
	}
	if self.draftStash.Count() != 0 {
		t.Errorf("got %d drafts left, want none", self.draftStash.Count())
	}
}

func TestControlSOnEmptyInputAndEmptyStashSaysNothing(t *testing.T) {
	self := testConversation(t, &bytes.Buffer{})
	self.draftStash = stash.NewMemory()
	inputLine := edit.NewInput(nil)

	self.apply(inputLine, nil, stashKey)
	if got := self.feedback.Message().Text; got != "" {
		t.Errorf("empty stash said %q", got)
	}
}

func TestPoppingAnEmptyStashLeavesTheDraftAndSaysWhy(t *testing.T) {
	self := testConversation(t, &bytes.Buffer{})
	self.draftStash = stash.NewMemory()
	inputLine := edit.NewInput(nil)
	inputLine.SetText("keep this")

	self.apply(inputLine, nil, popKey)
	if got := inputLine.Text(); got != "keep this" {
		t.Errorf("empty pop changed the draft to %q", got)
	}
	if got := self.feedback.Message().Text; got != "Stash is empty" {
		t.Errorf("empty pop said %q", got)
	}
}

func stashGoldenStream(t *testing.T, drafts int, doesPop bool) string {
	t.Helper()

	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
	self.draftStash = stash.NewMemory()

	layout, err := configFrom(t, "").BuildLayout(availableSegments(work.At(workspaceMarker), self))
	if err != nil {
		t.Fatal(err)
	}
	self.display.bar = bar.NewConfiguration(nil, layout)

	inputLine := edit.NewInput(nil)
	self.show(inputLine)
	for index := range drafts {
		inputLine.SetText(strings.Repeat("later ", index) + "future task")
		self.handleKeypressAndShowInput(inputLine, nil, stashKey)
	}
	if doesPop {
		self.handleKeypressAndShowInput(inputLine, nil, popKey)
	}

	return screenOutput.String()
}

func TestGoldenTheDefaultBarShowsTheStashCount(t *testing.T) {
	passes := map[string]func() string{
		"empty stash has no mark": func() string { return stashGoldenStream(t, 0, false) },
		"one stashed draft":       func() string { return stashGoldenStream(t, 1, false) },
		"two stashed drafts":      func() string { return stashGoldenStream(t, 2, false) },
		"one draft restored":      func() string { return stashGoldenStream(t, 2, true) },
	}

	compareWithGolden(t, "stash", ".ansi", passes)
	compareWithGolden(t, "stash", ".screen", shownPasses(t, passes))
}

func TestTheDefaultStashSegmentDrawsNothingWhenEmpty(t *testing.T) {
	self := testConversation(t, &bytes.Buffer{})
	self.draftStash = stash.NewMemory()
	layout, err := configFrom(t, "").BuildLayout(availableSegments(work.At(workspaceMarker), self))
	if err != nil {
		t.Fatal(err)
	}

	drawn := style.Plain(bar.Render(layout, segment.BottomCenter, segment.Context{}))
	if strings.Contains(drawn, "🗃️") {
		t.Errorf("empty stash drew %q", drawn)
	}
}
