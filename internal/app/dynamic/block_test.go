package dynamic

import (
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
	"unicode"

	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/app/width"
)

const (
	wide   = 100
	narrow = 40
	tiny   = 12
)

func testBlock() *Block {
	return &Block{refresh: func() {}, stop: make(chan struct{})}
}

type textLabel struct {
	text string
}

func (self textLabel) Elide(room int) Label { return textLabel{text: width.Elide(self.text, room)} }

func (self textLabel) Render() string { return self.text }

func (self textLabel) Width() int { return width.Of(self.text) }

func rowLabel(name string, subject string) Label {
	return textLabel{text: name + " " + subject}
}

var colourEscape = regexp.MustCompile(`^\x1b\[[0-9;]*m`)

func paintedOnly(row string) bool {
	for at := 0; at < len(row); {
		if row[at] != '\x1b' {
			at++
			continue
		}

		found := colourEscape.FindString(row[at:])
		if found == "" {
			return false
		}

		at += len(found)
	}

	return true
}

func bounded(value int, limit int) int {
	within := value % limit
	if within < 0 {
		within = -within
	}

	return within
}

func FuzzARowFitsAndPaintsOnlyItsOwnColours(f *testing.F) {
	for _, seed := range []string{
		"hello",
		"one\ntwo\n",
		"\x1b[32mok\x1b[0m all six steps",
		"\x1b[2J\x1b[H",
		"\x1b]0;building\x07",
		strings.Repeat("wordy ", wide),
		"",
	} {
		for _, columns := range []int{0, 1, tiny, narrow, wide} {
			f.Add(seed, 12, columns, false)
			f.Add(seed, 12, columns, true)
		}
	}

	f.Fuzz(func(t *testing.T, summary string, labelWidth int, columns int, hasFailed bool) {
		const widestRow = 200

		columns = bounded(columns, widestRow)

		state := Done
		if hasFailed {
			state = Failed
		}

		block := testBlock()
		block.Add(rowLabel("$", strings.Repeat("a", bounded(labelWidth, widestRow))), 0)
		block.FinaliseRow(0, state, time.Second, summary, "1L ~1t")

		row := block.Rows(columns)[0]

		if columns > 0 && style.Width(row) > columns {
			t.Fatalf("expected the row to fit in %d columns, got %d in %q", columns, style.Width(row), row)
		}

		if !paintedOnly(row) {
			t.Fatalf("expected the row to paint only its own colours, got %q", row)
		}

		for _, character := range style.Plain(row) {
			if unicode.IsControl(character) {
				t.Fatalf("expected nothing a terminal would act on, got %q in %q", character, row)
			}
		}
	})
}

func TestARowSaysNothingOfItsProgressUntilItHasBeenGoingAWhile(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("read", "main.go"), 0)

	if got := block.Rows(wide)[0]; got != "read main.go" {
		t.Errorf("expected the label and nothing else, got %q", got)
	}
}

func TestAQuickRowIsMarkedWithoutATime(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("read", "main.go"), 0)

	block.FinaliseRow(0, Done, 500*time.Millisecond, "", "")

	if got := block.Rows(wide)[0]; !strings.Contains(got, "✓") || strings.Contains(got, "s") {
		t.Errorf("expected a mark and no time, got %q", got)
	}
}

func TestARowWorthWaitingForSaysHowLongItTook(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("read", "main.go"), 0)

	block.FinaliseRow(0, Done, 5*time.Second, "", "")

	if got := block.Rows(wide)[0]; !strings.Contains(got, style.Spinner("5s")) {
		t.Errorf("expected the time it took, got %q", got)
	}
}

func TestARowWorthWaitingForSaysHowLongItTookBesideWhatItMeasured(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("grep", "*.go"), 0)

	block.FinaliseRow(0, Done, 9*time.Second, "", "1L")

	got := block.Rows(wide)[0]
	if !strings.Contains(got, style.Spinner("9s")) {
		t.Errorf("expected the time it took, got %q", got)
	}
	if !strings.Contains(got, "1L") {
		t.Errorf("expected what it measured, got %q", got)
	}
}

func TestAQuickRowSaysOnlyWhatItMeasured(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("grep", "*.go"), 0)

	block.FinaliseRow(0, Done, 500*time.Millisecond, "", "1L")

	if got := block.Rows(wide)[0]; strings.Contains(got, "0.5s") {
		t.Errorf("expected no time on a quick row, got %q", got)
	}
}

func TestRunningTimerUsesWholeSecondGranularity(t *testing.T) {
	block := &Block{isSlow: true}
	item := row{state: Running, startedAt: time.Now().Add(-5500 * time.Millisecond)}

	if got := style.Plain(block.getResult(item)); got != "✦· 5s" {
		t.Errorf("expected a whole-second timer, got %q", got)
	}
}

func TestABoundedRowCountsUpTowardsTheTimeItWasGiven(t *testing.T) {
	block := &Block{isSlow: true}
	item := row{
		state:     Running,
		startedAt: time.Now().Add(-12 * time.Second),
		timeLimit: 20 * time.Second,
	}

	if got := style.Plain(block.getResult(item)); got != "✦· 12s/20s" {
		t.Errorf("expected the timer to name the bound, got %q", got)
	}
}

func TestABoundedRowDrawsItsBoundBeneathTheTimeItHasTaken(t *testing.T) {
	block := &Block{isSlow: true}
	item := row{
		state:     Running,
		startedAt: time.Now().Add(-12 * time.Second),
		timeLimit: 20 * time.Second,
	}

	want := style.Spinner("12s") + style.Subtle("/20s")

	if got := block.getResult(item); !strings.HasSuffix(got, want) {
		t.Errorf("expected the bound drawn apart from the elapsed time, got %q", got)
	}
}

func TestARowThatFinishedNeverNamesTheTimeItWasGiven(t *testing.T) {
	block := &Block{}
	item := row{state: Done, timeTaken: 12 * time.Second, timeLimit: 20 * time.Second}

	if got := style.Plain(block.getResult(item)); got != "✓ 12s" {
		t.Errorf("expected the settled row to say only what it took, got %q", got)
	}
}

func TestARowThatFailedIsMarkedApartFromOneThatDidNot(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("read", "main.go"), 0)
	block.Add(rowLabel("read", "nowhere.go"), 0)

	block.FinaliseRow(0, Done, 0, "", "")
	block.FinaliseRow(1, Failed, 0, "", "")

	rows := block.Rows(wide)

	if !strings.Contains(rows[0], "✓") || !strings.Contains(rows[1], "✗") {
		t.Errorf("expected a tick and a cross, got %q", rows)
	}
}

func TestARowThatFailedSaysWhy(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("write", "main.go"), 0)

	block.FinaliseRow(0, Failed, 0, "permission denied", "")

	if got := block.Rows(wide)[0]; !strings.Contains(got, style.Failure("permission denied")) {
		t.Errorf("expected the reason in the colour of a failure, got %q", got)
	}
}

func TestAReasonIsPutOnTheOneRow(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("bash", "make"), 0)

	block.FinaliseRow(0, Failed, 0, "no rule to make target\n\tstop.\r", "")

	if got := block.Rows(wide)[0]; strings.ContainsAny(got, "\n\r\t") {
		t.Errorf("expected one row, got %q", got)
	}
}

func TestAReasonTakesTheRoomTheLabelDoesNotWant(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("read", "main.go"), 0)

	reason := strings.Repeat("a", wide/2+10)

	block.FinaliseRow(0, Failed, 0, reason, "")

	if got := block.Rows(wide)[0]; !strings.Contains(got, reason) {
		t.Errorf("expected the whole reason, got %q", got)
	}
}

func TestALongReasonLeavesTheLabelItIsAbout(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("write", "main.go"), 0)

	block.FinaliseRow(0, Failed, 0, strings.Repeat("wordy ", wide), "")

	row := block.Rows(wide)[0]

	if style.Width(row) > wide {
		t.Errorf("expected the row to fit in %d columns, got %d in %q", wide, style.Width(row), row)
	}

	if !strings.Contains(row, "main.go") {
		t.Errorf("expected the call to survive the reason, got %q", row)
	}
}

func TestARowThatSucceededSaysWhatItHadToShowQuietly(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("$", "echo hello"), 0)

	block.FinaliseRow(0, Done, 0, "hello", "")

	if got := block.Rows(wide)[0]; !strings.Contains(got, style.Subtle("hello")) {
		t.Errorf("expected the output in a subtle colour, got %q", got)
	}
}

func TestASummaryIsPutOnTheOneRow(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("$", "make"), 0)

	block.FinaliseRow(0, Done, 0, "one\ntwo\r\n\tthree\n", "")

	got := block.Rows(wide)[0]

	if strings.ContainsAny(got, "\n\r\t") {
		t.Errorf("expected one row, got %q", got)
	}

	if !strings.Contains(got, "one two three") {
		t.Errorf("expected the output flattened onto the row, got %q", got)
	}
}

func TestASummaryPaintsNoColourOfItsOwn(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("$", "just check"), 0)

	block.FinaliseRow(0, Done, 0, "\x1b[32mok\x1b[0m all six steps", "")

	if got := block.Rows(wide)[0]; !strings.Contains(got, style.Subtle("ok all six steps")) {
		t.Errorf("expected the row to keep its own colours, got %q", got)
	}
}

func TestALongSummaryIsReadNoFurtherThanARowCanDraw(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("$", "yes"), 0)

	block.FinaliseRow(0, Done, 0, strings.Repeat("y\n", widestSummaryRead), "")

	if got := width.Of(block.rows[0].summary); got > widestSummaryRead {
		t.Errorf("expected no more than %d cells kept, got %d", widestSummaryRead, got)
	}
}

func TestASummaryTakesOnlyTheRoomALabelLeaves(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("$", strings.Repeat("a", wide)), 0)

	block.FinaliseRow(0, Done, 0, "hello", "")

	row := block.Rows(wide)[0]

	if style.Width(row) > wide {
		t.Errorf("expected the row to fit in %d columns, got %d in %q", wide, style.Width(row), row)
	}

	if strings.Contains(row, "hello") {
		t.Errorf("expected the command to keep the room, got %q", row)
	}
}

func TestClosingTheBlockMarksWhateverWasStillRunning(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("read", "main.go"), 0)
	block.Add(rowLabel("grep", "spinner"), 0)
	block.FinaliseRow(0, Done, 0, "", "")

	block.Close(Cancelled)

	rows := block.Rows(wide)

	if !strings.Contains(rows[0], "✓") {
		t.Errorf("expected the call that finished to keep its tick, got %q", rows[0])
	}

	if !strings.Contains(rows[1], "–") {
		t.Errorf("expected the call still running to be marked cancelled, got %q", rows[1])
	}
}

func TestARunningRowStandsStillWhileTimingIsHeld(t *testing.T) {
	block := testBlock()
	block.isSlow = true

	block.Add(rowLabel("bash", "curl example.com"), 20*time.Second)
	block.rows[0].startedAt = time.Now().Add(-70 * time.Second)

	block.HoldTiming()
	block.heldAt = block.heldAt.Add(-time.Minute)

	if got := style.Plain(block.getResult(block.rows[0])); got != "✦· 10s/20s" {
		t.Errorf("expected the row to stand still, got %q", got)
	}
}

func TestARunningRowLeavesOutTheTimeItWasHeldFor(t *testing.T) {
	block := testBlock()
	block.isSlow = true

	block.Add(rowLabel("bash", "curl example.com"), 20*time.Second)
	block.Add(rowLabel("read", "main.go"), 0)
	block.FinaliseRow(1, Done, 8*time.Second, "", "")
	block.rows[0].startedAt = time.Now().Add(-70 * time.Second)

	block.HoldTiming()
	block.heldAt = block.heldAt.Add(-time.Minute)
	block.ResumeTiming()

	if got := style.Plain(block.getResult(block.rows[0])); got != "✦· 10s/20s" {
		t.Errorf("expected the time awaiting an answer to be left out, got %q", got)
	}
	if got := style.Plain(block.getResult(block.rows[1])); got != "✓ 8s" {
		t.Errorf("expected a settled row to keep what it took, got %q", got)
	}
}

func TestARowAddedWhileTimingIsHeldStartsWhenTheHoldLifts(t *testing.T) {
	block := testBlock()
	block.isSlow = true

	block.Add(rowLabel("bash", "curl example.com"), 0)

	block.HoldTiming()
	block.heldAt = block.heldAt.Add(-time.Minute)
	block.Add(rowLabel("read", "main.go"), 0)

	if held := block.elapsedTime(block.rows[1]); held != 0 {
		t.Errorf("got %s while held, want a row that has counted nothing yet", held)
	}

	block.ResumeTiming()

	if resumed := time.Since(block.rows[1].startedAt); resumed < 0 {
		t.Errorf("got %s after the hold lifted, want a row that has not started in the future", resumed)
	}
}

func TestAHeldBlockStopsSpinning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var refreshes atomic.Int64

		block := NewBlock(func() { refreshes.Add(1) })
		block.Add(rowLabel("bash", "curl example.com"), 0)

		time.Sleep(spinning)
		synctest.Wait()
		whileRunning := refreshes.Load()
		if whileRunning == 0 {
			t.Fatal("a running block drew nothing")
		}

		block.HoldTiming()
		time.Sleep(spinning)
		synctest.Wait()

		if held := refreshes.Load(); held != whileRunning+1 {
			t.Errorf("got %d draws while held, want the one that held it", held-whileRunning)
		}

		block.ResumeTiming()
		time.Sleep(spinning)
		synctest.Wait()

		if resumed := refreshes.Load(); resumed <= whileRunning+2 {
			t.Error("a resumed block stayed still")
		}

		block.Stop()
	})
}

const spinning = 3 * time.Second

func TestAHeldRowThatIsCancelledSaysOnlyWhatItTook(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("bash", "curl example.com"), 0)
	block.rows[0].startedAt = time.Now().Add(-70 * time.Second)

	block.HoldTiming()
	block.heldAt = block.heldAt.Add(-time.Minute)
	block.Close(Cancelled)

	if got := block.rows[0].timeTaken.Truncate(time.Second); got != 10*time.Second {
		t.Errorf("got %s, want the time awaiting an answer left out", got)
	}
}

func TestARowIsMarkedOnlyOnce(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("read", "main.go"), 0)
	block.FinaliseRow(0, Failed, 0, "", "")

	block.FinaliseRow(0, Done, 0, "", "")

	if got := block.Rows(wide)[0]; !strings.Contains(got, "✗") {
		t.Errorf("expected the first mark to stand, got %q", got)
	}
}

func TestALabelIsCutToLeaveRoomForTheOutcome(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("grep", strings.Repeat("x", wide)), 0)

	block.FinaliseRow(0, Done, 3*time.Second, "", "")

	for _, row := range block.Rows(wide) {
		if style.Width(row) > wide {
			t.Errorf("expected the row to fit %d columns, got %d in %q", wide, style.Width(row), row)
		}
	}
}

func TestALabelTakesTheRoomATimeWouldHaveTakenWhereNoTimeIsShown(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("bash", strings.Repeat("x", wide)), 0)

	block.FinaliseRow(0, Done, time.Millisecond, "", "")

	for _, row := range block.Rows(wide) {
		if style.Width(row) != wide {
			t.Errorf(
				"expected the row to fill %d columns, got width %d in %q",
				wide, style.Width(row), row,
			)
		}
	}
}

func TestALabelGivesRoomBackWhenTheTimeAppears(t *testing.T) {
	block := testBlock()

	block.Add(rowLabel("bash", strings.Repeat("x", wide)), 0)

	block.FinaliseRow(0, Done, 3*time.Second, "", "")

	for _, row := range block.Rows(wide) {
		if style.Width(row) != wide {
			t.Errorf(
				"expected the row to fill %d columns, got width %d in %q",
				wide, style.Width(row), row,
			)
		}
	}
}

func TestACompletedOutcomeReachesTheTerminalEdge(t *testing.T) {
	block := testBlock()
	const narrow = 67

	block.Add(rowLabel("grep", "RESOLVE_UNIX|resolve_unix|unix.*socket|Landlock|landlock"), 0)

	block.FinaliseRow(0, Done, 7420*time.Millisecond, "", "")

	row := block.Rows(narrow)[0]
	if got := style.Plain(row); !strings.HasSuffix(got, "✓ 7s") {
		t.Errorf("expected the complete outcome at the end, got %q", got)
	}
	if got := style.Width(row); got > narrow {
		t.Errorf("expected the row to fit %d columns, got width %d in %q", narrow, got, row)
	}
}

func TestARowTooNarrowForWhatItMeasuredKeepsItsLabelAndItsMark(t *testing.T) {
	const tiny = 12

	block := testBlock()

	index := block.Add(rowLabel("bash", "if [[ -f one ]]; then echo one; fi"), 0)
	block.FinaliseRow(index, Done, time.Second, "", "900L+ ~500t (of ~225Kt)")

	row := block.Rows(tiny)[index]

	if got := style.Width(row); got > tiny {
		t.Errorf("expected the row to fit %d columns, got %d in %q", tiny, got, row)
	}

	plain := style.Plain(row)
	if !strings.HasPrefix(plain, "bash") || !strings.HasSuffix(plain, "✓") {
		t.Errorf("expected the call and its mark, got %q", plain)
	}
}

func TestARowWideEnoughKeepsEverythingItMeasured(t *testing.T) {
	block := testBlock()

	index := block.Add(rowLabel("bash", "echo one"), 0)
	block.FinaliseRow(index, Done, time.Second, "", "1L ~1t")

	if plain := style.Plain(block.Rows(wide)[index]); !strings.HasSuffix(plain, "✓ 1L ~1t") {
		t.Errorf("expected everything the call measured, got %q", plain)
	}
}

func TestTheOutcomeAppearsAsSoonAsOneLabelCellFitsBesideIt(t *testing.T) {
	block := testBlock()

	index := block.Add(rowLabel("bash", "echo one"), 0)
	block.FinaliseRow(index, Done, 3*time.Second, "", "1L ~1t")

	drawn := style.Plain(block.Rows(wide)[index])
	at := strings.Index(drawn, "✓")
	if at < 0 {
		t.Fatalf("expected a mark in %q", drawn)
	}

	outcome := drawn[at:]
	fits := width.Of(outcome) + 2

	if plain := style.Plain(block.Rows(fits)[index]); !strings.HasSuffix(plain, outcome) {
		t.Errorf("expected %q beside a cell of the call in %d columns, got %q", outcome, fits, plain)
	}

	if plain := style.Plain(block.Rows(fits - 1)[index]); strings.HasSuffix(plain, outcome) {
		t.Errorf("expected the mark alone in %d columns, got %q", fits-1, plain)
	}
}

func TestALongSummaryIsElidedToTheTerminalEdge(t *testing.T) {
	block := testBlock()

	index := block.Add(rowLabel("bash", "echo one"), 0)
	block.FinaliseRow(index, Done, 3*time.Second, strings.Repeat("summary ", 20), "")

	row := block.Rows(narrow)[index]
	if got := style.Width(row); got != narrow {
		t.Errorf("expected the row to fill %d columns, got width %d in %q", narrow, got, row)
	}
	if plain := style.Plain(row); !strings.HasSuffix(plain, width.Ellipsis) {
		t.Errorf("expected the summary to be elided at the edge, got %q", plain)
	}
}
