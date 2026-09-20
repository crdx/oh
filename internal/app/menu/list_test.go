package menu

import (
	"errors"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"crdx.org/io/internal/app/key"
	"crdx.org/io/internal/app/style"
)

type fakeList struct {
	rows       []string
	unrunnable []bool
	adjusted   []int
}

func (self *fakeList) Len() int { return len(self.rows) }

func (self *fakeList) IsChoosable(index int) bool {
	return index >= len(self.unrunnable) || !self.unrunnable[index]
}

func (self *fakeList) Text(index int) string { return self.rows[index] }

func (self *fakeList) ColumnHeader(room int) string { return Clip("  Agent  Title", room) }

func (self *fakeList) Row(index int, isChosen bool, room int) string {
	return Clip(Mark(isChosen)+" "+self.rows[index], room)
}

func (self *fakeList) Adjust(index int, direction int) {
	if self.adjusted == nil {
		self.adjusted = make([]int, len(self.rows))
	}

	self.adjusted[index] += direction
}

func listState(rows List, cursor int) *state {
	self := newState(rows)
	self.cursor = cursor
	self.startWork = func(work func()) { work() }

	return self
}

func rowsNamed(names ...string) *fakeList {
	return &fakeList{rows: names}
}

func defaultState() *state {
	return listState(rowsNamed("first", "second", "third"), 0)
}

func TestClipReturnsNothingWhenThereAreNoColumns(t *testing.T) {
	if got := Clip("session", 0); got != "" {
		t.Errorf("expected no text in no columns, got %q", got)
	}
}

func TestEveryWayOfAbandoningTheChoice(t *testing.T) {
	for name, keypress := range map[string]key.Key{
		"escape":       {Code: key.Escape},
		"csi-u escape": {Code: key.Escape, Mod: key.Ctrl},
		"ctrl+c":       {Code: key.Rune, Value: 'c', Mod: key.Ctrl},
	} {
		if got := defaultState().apply(keypress); got != choiceCancelled {
			t.Errorf("%s: expected the choice to be abandoned, got %v", name, got)
		}
	}
}

func TestAnUnrecognisedSequenceChangesNothing(t *testing.T) {
	if got := defaultState().apply(key.Key{Code: key.Unknown}); got != continuePicking {
		t.Errorf("expected nothing to happen, got %v", got)
	}
}

func TestPlainLettersAreNotACancellation(t *testing.T) {
	for _, value := range []rune{'a', 'c', 'q', 'Q'} {
		if got := defaultState().apply(key.Key{Code: key.Rune, Value: value}); got != continuePicking {
			t.Errorf("%q: expected nothing to happen, got %v", value, got)
		}
	}
}

func TestTheCursorStopsAtEitherEnd(t *testing.T) {
	self := defaultState()

	for range 5 {
		self.apply(key.Key{Code: key.Up})
	}

	if self.cursor != 0 {
		t.Errorf("expected the first row, got %d", self.cursor)
	}

	for range 5 {
		self.apply(key.Key{Code: key.Down})
	}

	if self.cursor != 2 {
		t.Errorf("expected the last row, got %d", self.cursor)
	}
}

func TestTheCursorSkipsRowsThatCannotBeChosen(t *testing.T) {
	rows := &fakeList{
		rows:       []string{"one", "two", "three", "four", "five"},
		unrunnable: []bool{true, false, true, false, true},
	}
	self := listState(rows, 1)

	self.apply(key.Key{Code: key.Down})
	if self.cursor != 3 {
		t.Errorf("expected the next row that can be chosen, got %d", self.cursor)
	}
	self.apply(key.Key{Code: key.Down})
	if self.cursor != 3 {
		t.Errorf("expected the cursor to stop at the last row that can be chosen, got %d", self.cursor)
	}
	self.apply(key.Key{Code: key.Home})
	if self.cursor != 1 {
		t.Errorf("expected home to choose the first row that can be chosen, got %d", self.cursor)
	}
	self.apply(key.Key{Code: key.End})
	if self.cursor != 3 {
		t.Errorf("expected end to choose the last row that can be chosen, got %d", self.cursor)
	}
}

func TestARowThatCannotBeChosenIsNotChosen(t *testing.T) {
	self := listState(&fakeList{rows: []string{"one"}, unrunnable: []bool{true}}, 0)
	if got := self.apply(key.Key{Code: key.Enter}); got != continuePicking {
		t.Errorf("expected the row to be ignored, got %v", got)
	}
	if got := self.firstSelectable(); got != -1 {
		t.Errorf("expected no initial choice, got %d", got)
	}
}

func TestTypingNarrowsTheListToWhatWasTyped(t *testing.T) {
	self := listState(rowsNamed(
		"chewy-sardine why the spinner stutters",
		"thick-poodle reasoning traces",
		"funny-badger the spinner again",
	), 0)

	for _, value := range []rune{'s', 'p', 'i', 'n'} {
		self.apply(key.Key{Code: key.Rune, Value: value})
	}

	if self.query != "spin" {
		t.Errorf("expected what was typed, got %q", self.query)
	}
	if !slices.Equal(self.matches, []int{0, 2}) {
		t.Errorf("expected the rows that say it, got %v", self.matches)
	}

	self.apply(key.Key{Code: key.Down})
	if self.chosen() != 2 {
		t.Errorf("expected the second of the narrowed rows, got %d", self.chosen())
	}

	self.apply(key.Key{Code: key.Backspace})
	if self.query != "spi" {
		t.Errorf("expected the last letter to be taken back, got %q", self.query)
	}
	if self.chosen() != 2 {
		t.Errorf("expected the cursor to stay where it was, got %d", self.chosen())
	}
}

func TestEveryWordTypedMustAppearSomewhereInTheRow(t *testing.T) {
	self := listState(rowsNamed(
		"chewy-sardine why the spinner stutters",
		"thick-poodle the spinner is fine",
	), 0)
	self.narrow("poodle spinner")

	if !slices.Equal(self.matches, []int{1}) {
		t.Errorf("expected the row both words describe, got %v", self.matches)
	}
}

func TestAFilterThatMatchesNothingLeavesNothingToChoose(t *testing.T) {
	self := listState(rowsNamed("chewy-sardine"), 0)
	self.narrow("nothing at all")

	if self.cursor != -1 {
		t.Errorf("expected no cursor, got %d", self.cursor)
	}
	if got := self.apply(key.Key{Code: key.Enter}); got != continuePicking {
		t.Errorf("expected nothing to be chosen, got %v", got)
	}

	self.narrow("")
	if self.cursor != 0 {
		t.Errorf("expected the cursor back on the only row, got %d", self.cursor)
	}
}

func TestTakingBackWhatWasNeverTypedIsNothing(t *testing.T) {
	self := listState(rowsNamed("chewy-sardine"), 0)
	self.apply(key.Key{Code: key.Backspace})

	if self.query != "" {
		t.Errorf("expected an empty filter, got %q", self.query)
	}
}

func TestAControlSequenceIsNotTypedIntoTheFilter(t *testing.T) {
	self := listState(rowsNamed("chewy-sardine"), 0)
	self.apply(key.Key{Code: key.Rune, Value: 'a', Mod: key.Alt})

	if self.query != "" {
		t.Errorf("expected an empty filter, got %q", self.query)
	}
}

func TestAnEmptyListIsNothingToChooseFrom(t *testing.T) {
	if _, err := Choose(&fakeList{}, nil, nil); !errors.Is(err, ErrCancelled) {
		t.Errorf("expected the choice to be abandoned, got %v", err)
	}
}

type drawnScreen chan struct{}

func (self drawnScreen) Write(written []byte) (int, error) {
	self <- struct{}{}
	return len(written), nil
}

func TestTheListIsDrawnAgainWhenTheTerminalChangesSize(t *testing.T) {
	self := listState(rowsNamed("chewy-sardine"), 0)

	drawn := make(drawnScreen)
	self.screen = drawn
	self.measure = func() (int, int) { return 80, 24 }

	keys := make(chan key.Key)
	self.keys = keys

	resizes := make(chan os.Signal, 1)
	resizes <- syscall.SIGWINCH

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		if _, err := self.pick(resizes); !errors.Is(err, ErrCancelled) {
			t.Errorf("expected the choice to be abandoned, got %v", err)
		}
	}()

	<-drawn
	<-drawn

	keys <- key.Key{Code: key.Escape}
	<-finished
}

func TestTheChoiceEndsWhenThereIsNothingLeftToRead(t *testing.T) {
	self := listState(rowsNamed("chewy-sardine"), 0)
	self.screen = io.Discard
	self.measure = func() (int, int) { return 80, 24 }

	keys := make(chan key.Key)
	close(keys)
	self.keys = keys

	if _, err := self.pick(nil); !errors.Is(err, ErrCancelled) {
		t.Errorf("expected the choice to be abandoned, got %v", err)
	}
}

func TestAPageMovesTheCursorByWhatIsOnScreen(t *testing.T) {
	rows := make([]string, 20)
	for i := range rows {
		rows[i] = "able-dolphin"
	}

	self := listState(&fakeList{rows: rows}, 0)
	self.window = 5

	self.apply(key.Key{Code: key.PageDown})
	if self.cursor != 5 {
		t.Errorf("expected a page down the list, got %d", self.cursor)
	}

	self.apply(key.Key{Code: key.PageUp})
	if self.cursor != 0 {
		t.Errorf("expected a page back up the list, got %d", self.cursor)
	}

	self.apply(key.Key{Code: key.PageUp})
	if self.cursor != 0 {
		t.Errorf("expected the cursor to stop at the top, got %d", self.cursor)
	}

	for range 5 {
		self.apply(key.Key{Code: key.PageDown})
	}
	if self.cursor != len(rows)-1 {
		t.Errorf("expected the cursor to stop at the bottom, got %d", self.cursor)
	}
}

func TestAPageIsAtLeastOneRowWhenNothingHasBeenDrawn(t *testing.T) {
	self := listState(rowsNamed("one", "two", "three"), 0)

	self.apply(key.Key{Code: key.PageDown})
	if self.cursor != 1 {
		t.Errorf("expected a single row of movement, got %d", self.cursor)
	}
}

type removableList struct {
	fakeList

	removed []string
	refused []string
	failure error
}

func (self *removableList) Removal(index int, keypress key.Key) (Removal, bool) {
	if !self.IsChoosable(index) || keypress != archiveKey() {
		return Removal{}, false
	}

	return Removal{
		Prompt:   "Press ctrl+a again to archive",
		Progress: "Archiving…",
		Perform:  func() error { return self.perform(index) },
		Apply:    func() { self.apply(index) },
	}, true
}

func (self *removableList) perform(index int) error {
	if self.failure != nil {
		return self.failure
	}
	if slices.Contains(self.refused, self.rows[index]) {
		return errors.New("the session is already open elsewhere")
	}

	return nil
}

func (self *removableList) apply(index int) {
	self.removed = append(self.removed, self.rows[index])
	self.rows = slices.Delete(self.rows, index, index+1)
}

type switchableList struct {
	removableList

	other      []string
	isSwitched bool
}

func (self *switchableList) Switch(int) bool {
	self.rows, self.other = self.other, self.rows
	self.isSwitched = !self.isSwitched

	return true
}

func archiveKey() key.Key {
	return key.Key{Code: key.Rune, Value: 'a', Mod: key.Ctrl}
}

func removableRows(names ...string) *removableList {
	return &removableList{fakeList: fakeList{rows: names}}
}

func TestARowIsRemovedOnlyOnASecondPress(t *testing.T) {
	rows := removableRows("first", "second", "third")
	self := listState(rows, 1)

	self.apply(archiveKey())
	if self.removalState.index != 1 {
		t.Fatalf("expected the second row to be awaiting an answer, got %d", self.removalState.index)
	}

	self.apply(key.Key{Code: key.Rune, Value: 'n'})
	if self.removalState.index != -1 || len(rows.removed) != 0 {
		t.Fatalf("expected any other key to leave the row alone, got %v", rows.removed)
	}
	if self.query != "" {
		t.Errorf("expected the key that cancelled to be swallowed, got the filter %q", self.query)
	}

	self.apply(archiveKey())
	self.apply(archiveKey())

	if !slices.Equal(rows.removed, []string{"second"}) {
		t.Fatalf("got the rows removed as %v", rows.removed)
	}
	if !slices.Equal(rows.rows, []string{"first", "third"}) {
		t.Fatalf("got the rows left as %v", rows.rows)
	}
	if self.cursor != 1 || self.chosen() != 1 {
		t.Errorf("expected the cursor to hold its place, got %d", self.cursor)
	}
}

func TestRemovingTheLastRowLeavesTheCursorOnTheOneBefore(t *testing.T) {
	rows := removableRows("first", "second")
	self := listState(rows, 1)

	self.apply(archiveKey())
	self.apply(archiveKey())

	if self.cursor != 0 {
		t.Errorf("expected the cursor to fall back a row, got %d", self.cursor)
	}

	self.apply(archiveKey())
	self.apply(archiveKey())

	if self.cursor != -1 {
		t.Errorf("expected an empty list to have no cursor, got %d", self.cursor)
	}
}

func TestARowThatCannotBeRemovedIsNotAskedAbout(t *testing.T) {
	rows := removableRows("first", "second")
	rows.unrunnable = []bool{false, true}
	self := listState(rows, 1)

	self.apply(archiveKey())
	if self.removalState.index != -1 {
		t.Errorf("expected the running row to be left alone, got %d", self.removalState.index)
	}
}

func TestAFailedRemovalIsReportedInPlaceOfTheFilter(t *testing.T) {
	rows := removableRows("first", "second")
	rows.failure = errors.New("the session is already open elsewhere")
	self := listState(rows, 0)

	self.apply(archiveKey())
	self.apply(archiveKey())

	if !strings.Contains(self.promptLine(80), rows.failure.Error()) {
		t.Errorf("expected the failure on the prompt line, got %q", self.promptLine(80))
	}

	self.apply(key.Key{Code: key.Down})
	if !strings.Contains(self.promptLine(80), filterPrompt) {
		t.Errorf("expected the filter back on the prompt line, got %q", self.promptLine(80))
	}
}

func TestSwitchingTheViewRefiltersAndTakesTheCursorToTheTop(t *testing.T) {
	rows := &switchableList{
		removableList: removableList{fakeList: fakeList{rows: []string{"first", "second", "third"}}},
		other:         []string{"archived-otter"},
	}
	self := listState(rows, 2)

	self.apply(key.Key{Code: key.Right})
	if !rows.isSwitched || len(self.matches) != 1 {
		t.Fatalf("expected the other view, got %d rows", len(self.matches))
	}
	if self.cursor != 0 || self.chosen() != 0 {
		t.Errorf("expected the cursor at the top, got %d", self.cursor)
	}

	self.apply(key.Key{Code: key.Left})
	if rows.isSwitched || len(self.matches) != 3 {
		t.Fatalf("expected the first view back, got %d rows", len(self.matches))
	}
}

func TestALisWithoutSwitchingStillAdjustsARow(t *testing.T) {
	rows := rowsNamed("first", "second")
	self := listState(rows, 1)

	self.apply(key.Key{Code: key.Right})
	if rows.adjusted == nil || rows.adjusted[1] != 1 {
		t.Errorf("expected the row to be adjusted, got %v", rows.adjusted)
	}
}

func TestTheRemovalKeyIsNotTypedIntoTheFilter(t *testing.T) {
	rows := removableRows("first", "second")
	self := listState(rows, 0)

	self.apply(archiveKey())
	self.apply(key.Key{Code: key.Escape})

	if self.query != "" {
		t.Errorf("expected nothing to have been typed, got %q", self.query)
	}

	self.apply(archiveKey())
	if self.removalState.index != 0 {
		t.Fatalf("expected the removal to be asked about again, got %d", self.removalState.index)
	}
	if self.query != "" {
		t.Errorf("expected the removal key to stay out of the filter, got %q", self.query)
	}
}

func TestALisWithoutRemovalIgnoresTheRemovalKey(t *testing.T) {
	self := defaultState()

	self.apply(archiveKey())
	if self.removalState.index != -1 {
		t.Errorf("expected nothing to be asked, got %d", self.removalState.index)
	}
	if self.query != "" {
		t.Errorf("expected the key to be swallowed rather than typed, got %q", self.query)
	}
}

func TestKeysAreDiscardedWhileTheWorkRuns(t *testing.T) {
	rows := removableRows("first", "second")
	self := listState(rows, 0)
	self.removalState = removalState{index: -1, isWorking: true, work: Removal{Progress: "Archiving…"}}

	for _, keypress := range []key.Key{
		{Code: key.Enter},
		{Code: key.Down},
		{Code: key.Escape},
		{Code: key.Rune, Value: 'x'},
		archiveKey(),
	} {
		if got := self.apply(keypress); got != continuePicking {
			t.Errorf("expected %v to be discarded, got action %v", keypress, got)
		}
	}

	if self.cursor != 0 {
		t.Errorf("expected the cursor to stay put, got %d", self.cursor)
	}
	if self.query != "" {
		t.Errorf("expected nothing to be typed, got %q", self.query)
	}
	if !strings.Contains(self.promptLine(40), "Archiving…") {
		t.Errorf("expected the work to be named, got %q", self.promptLine(40))
	}
}

func TestTheWorkIsNamedBeforeItStarts(t *testing.T) {
	rows := removableRows("first", "second")

	var drawnWhileWorking string
	self := listState(rows, 0)
	self.screen = &strings.Builder{}
	self.startWork = func(work func()) {
		drawnWhileWorking = self.promptLine(40)
		work()
	}

	self.apply(archiveKey())
	self.apply(archiveKey())

	if !strings.Contains(drawnWhileWorking, "Archiving…") {
		t.Errorf("expected the work to be named before it ran, got %q", drawnWhileWorking)
	}
	if !slices.Equal(rows.removed, []string{"first"}) {
		t.Errorf("expected the work to have been done, got %v", rows.removed)
	}
	if strings.Contains(self.promptLine(40), "Archiving…") {
		t.Errorf("expected the work to be over, got %q", self.promptLine(40))
	}
}

func TestARemovalLeavesTheViewportWhereItWas(t *testing.T) {
	rows := make([]string, 40)
	for i := range rows {
		rows[i] = "session-" + strconv.Itoa(i)
	}

	list := &removableList{fakeList: fakeList{rows: rows}}
	self := listState(list, 25)
	self.measure = func() (int, int) { return 80, 12 }
	self.offset = 20

	self.apply(archiveKey())
	self.apply(archiveKey())

	if !slices.Equal(list.removed, []string{"session-25"}) {
		t.Fatalf("got the rows removed as %v", list.removed)
	}
	if self.offset != 20 {
		t.Errorf("expected the viewport to stay at 20, got %d", self.offset)
	}
	if self.cursor != 25 {
		t.Errorf("expected the cursor to hold its place, got %d", self.cursor)
	}

	self.draw()
	if self.offset != 20 || self.cursor != 25 {
		t.Errorf("expected the drawing to leave both alone, got offset %d and cursor %d", self.offset, self.cursor)
	}
}

func TestRemovingFromTheEndPullsTheViewportBackToFillTheScreen(t *testing.T) {
	rows := make([]string, 12)
	for i := range rows {
		rows[i] = "session-" + strconv.Itoa(i)
	}

	list := &removableList{fakeList: fakeList{rows: rows}}
	self := listState(list, 11)
	self.measure = func() (int, int) { return 80, 12 }
	self.offset = 2

	self.apply(archiveKey())
	self.apply(archiveKey())

	if self.offset != 1 {
		t.Errorf("expected the viewport to pull back to 1, got %d", self.offset)
	}
	if self.cursor != 10 {
		t.Errorf("expected the cursor to fall back a row, got %d", self.cursor)
	}
}

type previewableList struct {
	fakeList

	read    map[string][]string
	failure error
	reads   int
	rooms   []int
}

func (self *previewableList) Preview(index int, keypress key.Key) (Preview, bool) {
	if keypress.Code != key.Enter || !self.IsChoosable(index) {
		return Preview{}, false
	}

	name := self.rows[index]

	return Preview{
		Title: "reading " + name,
		Read: func(room int) ([]string, error) {
			self.reads++
			self.rooms = append(self.rooms, room)
			if self.failure != nil {
				return nil, self.failure
			}

			return self.read[name], nil
		},
	}, true
}

func previewableRows(names ...string) *previewableList {
	rows := &previewableList{fakeList: fakeList{rows: names}, read: map[string][]string{}}
	for _, name := range names {
		rows.read[name] = []string{name + " one", name + " two", name + " three", name + " four"}
	}

	return rows
}

func TestAPreviewOpensOnTheLastRowsOfWhatItRead(t *testing.T) {
	rows := previewableRows("first", "second")
	self := listState(rows, 1)
	self.measure = func() (int, int) { return 40, 9 }

	self.apply(key.Key{Code: key.Enter})
	if !self.preview.isOpen {
		t.Fatal("expected enter to open the preview")
	}

	self.draw()
	if self.preview.offset != 2 {
		t.Errorf("expected the preview to open at its end, got the offset %d", self.preview.offset)
	}
	if rows.reads != 1 {
		t.Errorf("expected what was read to be kept, got %d reads", rows.reads)
	}
}

func TestAPreviewIsReadAgainOnlyWhenTheRoomChanges(t *testing.T) {
	rows := previewableRows("first")
	self := listState(rows, 0)
	room := 40
	self.measure = func() (int, int) { return room, 10 }

	self.apply(key.Key{Code: key.Enter})
	self.draw()
	self.draw()
	if rows.reads != 1 {
		t.Errorf("expected one read while nothing changed, got %d", rows.reads)
	}

	room = 60
	self.draw()
	if !slices.Equal(rows.rooms, []int{40, 60}) {
		t.Errorf("expected the wider terminal to be read again, got %v", rows.rooms)
	}
}

func TestAPreviewScrollsWithoutMovingTheCursorBehindIt(t *testing.T) {
	rows := previewableRows("first", "second")
	self := listState(rows, 1)
	self.measure = func() (int, int) { return 40, 9 }

	self.apply(key.Key{Code: key.Enter})
	self.draw()

	self.apply(key.Key{Code: key.Up})
	if self.preview.offset != 1 {
		t.Errorf("expected the preview to scroll back a row, got %d", self.preview.offset)
	}
	if self.cursor != 1 {
		t.Errorf("expected the cursor to stay where it was, got %d", self.cursor)
	}

	self.apply(key.Key{Code: key.Home})
	if self.preview.offset != 0 {
		t.Errorf("expected home to reach the start, got %d", self.preview.offset)
	}

	self.apply(key.Key{Code: key.Up})
	if self.preview.offset != 0 {
		t.Errorf("expected the start to hold, got %d", self.preview.offset)
	}

	self.apply(key.Key{Code: key.End})
	self.apply(key.Key{Code: key.Down})
	if self.preview.offset != 2 {
		t.Errorf("expected the end to hold, got %d", self.preview.offset)
	}
}

func TestAPreviewIsClosedByEscapeAndByQ(t *testing.T) {
	for _, keypress := range []key.Key{{Code: key.Escape}, {Code: key.Rune, Value: 'q'}} {
		rows := previewableRows("first", "second")
		self := listState(rows, 0)
		self.measure = func() (int, int) { return 40, 6 }

		self.apply(key.Key{Code: key.Enter})
		self.draw()

		if action := self.apply(keypress); action != continuePicking {
			t.Errorf("expected %v to close the preview alone, got the action %v", keypress, action)
		}
		if self.preview.isOpen {
			t.Errorf("expected %v to close the preview", keypress)
		}
		if self.query != "" {
			t.Errorf("expected the key that closed the preview to be swallowed, got %q", self.query)
		}
	}
}

func TestAPreviewChoosesItsRowOnASecondEnter(t *testing.T) {
	rows := previewableRows("first", "second")
	self := listState(rows, 1)
	self.measure = func() (int, int) { return 40, 6 }

	self.apply(key.Key{Code: key.Enter})
	self.draw()

	if action := self.apply(key.Key{Code: key.Enter}); action != rowChosen {
		t.Errorf("expected a second enter to choose the row, got the action %v", action)
	}
	if self.chosen() != 1 {
		t.Errorf("expected the previewed row to be the chosen one, got %d", self.chosen())
	}
}

func TestAPreviewThatCouldNotBeReadSaysSoAndStaysOpen(t *testing.T) {
	rows := previewableRows("first")
	rows.failure = errors.New("the journal could not be read")
	self := listState(rows, 0)
	self.measure = func() (int, int) { return 40, 6 }

	self.apply(key.Key{Code: key.Enter})
	self.draw()

	if !self.preview.isOpen {
		t.Fatal("expected the preview to stay open after a failure")
	}
	if self.preview.failure != "the journal could not be read" {
		t.Errorf("expected the failure to be kept, got %q", self.preview.failure)
	}
}

func TestAListThatCannotBePreviewedChoosesOnEnter(t *testing.T) {
	self := listState(rowsNamed("first", "second"), 1)

	if action := self.apply(key.Key{Code: key.Enter}); action != rowChosen {
		t.Errorf("expected enter to choose when nothing can be previewed, got %v", action)
	}
}

type reachableList struct {
	previewableList
}

func (self *reachableList) IsReachable(int) bool { return true }

func (self *reachableList) Preview(index int, keypress key.Key) (Preview, bool) {
	if keypress.Code != key.Enter {
		return Preview{}, false
	}

	name := self.rows[index]

	return Preview{
		Title: "reading " + name,
		Read:  func(int) ([]string, error) { return self.read[name], nil },
	}, true
}

func reachableRows(names ...string) *reachableList {
	return &reachableList{previewableList: *previewableRows(names...)}
}

func TestARowThatCannotBeChosenIsStillReached(t *testing.T) {
	rows := reachableRows("first", "second")
	rows.unrunnable = []bool{true, false}
	self := listState(rows, 0)

	if self.cursor != 0 {
		t.Fatalf("expected the cursor to rest on the first row, got %d", self.cursor)
	}

	self.move(1)
	self.move(-1)
	if self.cursor != 0 {
		t.Errorf("expected the cursor to come back to the row that cannot be chosen, got %d", self.cursor)
	}
}

func TestARowThatCannotBeChosenIsReadButNeverOpened(t *testing.T) {
	rows := reachableRows("first", "second")
	rows.unrunnable = []bool{true, false}
	self := listState(rows, 0)
	self.measure = func() (int, int) { return 40, 6 }

	if action := self.apply(key.Key{Code: key.Enter}); action != continuePicking {
		t.Fatalf("expected enter to read the row rather than choose it, got %v", action)
	}
	if !self.preview.isOpen {
		t.Fatal("expected the preview to open on a row that cannot be chosen")
	}
	if self.preview.isOpenable {
		t.Error("expected the preview to know the row cannot be opened")
	}

	self.draw()

	if action := self.apply(key.Key{Code: key.Enter}); action != continuePicking {
		t.Errorf("expected a second enter to refuse to open the row, got %v", action)
	}
	if !self.preview.isOpen {
		t.Error("expected the preview to stay open when the row cannot be opened")
	}
}

func TestAListWithoutReachabilityFollowsWhatCanBeChosen(t *testing.T) {
	rows := previewableRows("first", "second")
	rows.unrunnable = []bool{true, false}
	self := listState(rows, 1)

	self.move(-1)
	if self.cursor != 1 {
		t.Errorf("expected the row that cannot be chosen to be skipped, got %d", self.cursor)
	}
}

func TestAPreviewMarksWhyItCannotBeOpened(t *testing.T) {
	rows := reachableRows("first", "second")
	rows.unrunnable = []bool{true, false}
	self := listState(rows, 0)
	self.measure = func() (int, int) { return 40, 6 }

	self.apply(key.Key{Code: key.Enter})

	mark, rest, _ := strings.Cut(runningPreviewHint, " ")
	if got, want := self.previewHint(40), style.Success(mark)+style.Subtle(" "+rest); got != want {
		t.Errorf("got the hint %q, want %q", got, want)
	}

	self.preview.isOpenable = true
	if got, want := self.previewHint(40), style.Subtle(openablePreviewHint); got != want {
		t.Errorf("got the hint %q, want %q", got, want)
	}
}

func TestAHintWithNoRoomForItsMarkIsStillDrawn(t *testing.T) {
	rows := reachableRows("first")
	rows.unrunnable = []bool{true}
	self := listState(rows, 0)
	self.measure = func() (int, int) { return 4, 6 }

	self.apply(key.Key{Code: key.Enter})

	if got, want := self.previewHint(4), style.Success(Clip(runningPreviewHint, 4)); got != want {
		t.Errorf("got the hint %q, want %q", got, want)
	}
}
