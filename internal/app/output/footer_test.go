package output

import (
	"slices"
	"strings"
	"testing"
)

func footerRows(count int) []string {
	rows := make([]string, count)
	for i := range rows {
		rows[i] = "row"
	}
	return rows
}

func TestAFooterShorterThanTheTerminalIsLeftAlone(t *testing.T) {
	screen := NewTerminalOfSize(&strings.Builder{}, 40, 10)

	rows, cursorRow := screen.fitFooter(footerRows(4), 3, 0, screen.lines)

	if len(rows) != 4 || cursorRow != 3 {
		t.Errorf("kept %d rows focused at %d, want 4 at 3", len(rows), cursorRow)
	}
}

func TestAFooterTallerThanTheTerminalIsCutToFit(t *testing.T) {
	screen := NewTerminalOfSize(&strings.Builder{}, 40, 10)

	rows, cursorRow := screen.fitFooter(footerRows(40), 39, 0, screen.lines)

	if len(rows) != 10 {
		t.Fatalf("kept %d rows, want the terminal's 10", len(rows))
	}
	if cursorRow != 9 {
		t.Errorf("focused at %d, want the last row", cursorRow)
	}
	if !strings.Contains(rows[0], "31 more lines") {
		t.Errorf("first row is %q, want it to say what was hidden", rows[0])
	}
}

func TestAFooterCutAtBothEndsSaysSoAtBoth(t *testing.T) {
	screen := NewTerminalOfSize(&strings.Builder{}, 40, 10)

	rows, cursorRow := screen.fitFooter(footerRows(40), 20, 0, screen.lines)

	if len(rows) != 10 {
		t.Fatalf("kept %d rows, want the terminal's 10", len(rows))
	}
	if !strings.Contains(rows[0], "more lines") || !strings.Contains(rows[len(rows)-1], "more lines") {
		t.Errorf("kept %q, want a notice at each end", rows)
	}
	if cursorRow <= 0 || cursorRow >= len(rows)-1 {
		t.Errorf("focused at %d, want it between the two notices", cursorRow)
	}
}

func TestOneHiddenLineIsSaidInTheSingular(t *testing.T) {
	screen := NewTerminalOfSize(&strings.Builder{}, 40, 10)

	rows, _ := screen.fitFooter(footerRows(40), 38, 0, screen.lines)

	last := rows[len(rows)-1]
	if !strings.Contains(last, "1 more line") || strings.Contains(last, "lines") {
		t.Errorf("last row is %q, want one line said in the singular", last)
	}
}

func TestAOneRowOverflowHidesTwoLinesBecauseTheNoticeCostsOne(t *testing.T) {
	screen := NewTerminalOfSize(&strings.Builder{}, 40, 10)

	rows, _ := screen.fitFooter(footerRows(11), 10, 0, screen.lines)

	if !strings.Contains(rows[0], "2 more lines") {
		t.Errorf("first row is %q, want the notice to count itself", rows[0])
	}
}

func TestAFooterIsLeftAloneWhenTheHeightIsUnknown(t *testing.T) {
	screen := &Screen{writer: &strings.Builder{}, isTerminal: true, canRepaint: true}

	screen.Footer(footerRows(40), 39, 0)

	if len(screen.canvas.rows) != 40 || screen.canvas.cursorRow != 39 {
		t.Errorf("kept %d rows focused at %d, want all 40 at 39", len(screen.canvas.rows), screen.canvas.cursorRow)
	}
}

func TestATerminalTooShortForANoticeStillFits(t *testing.T) {
	for _, lines := range []int{1, 2} {
		screen := NewTerminalOfSize(&strings.Builder{}, 40, lines)

		rows, cursorRow := screen.fitFooter(footerRows(40), 39, 0, screen.lines)

		if len(rows) != lines {
			t.Errorf("height %d kept %d rows, want %d", lines, len(rows), lines)
		}
		if cursorRow < 0 || cursorRow >= len(rows) {
			t.Errorf("height %d focused at %d, outside the %d rows kept", lines, cursorRow, len(rows))
		}
	}
}

func TestAFooterFillingTheTerminalStaysOffTheRowAboveIt(t *testing.T) {
	screen := NewTerminalOfSize(&strings.Builder{}, 40, 10)
	screen.Line("said before")

	screen.Footer(footerRows(40), 20, 0)

	if got := len(screen.canvas.rows); got != 9 {
		t.Errorf("kept %d rows, want 9 beneath the row said before", got)
	}
	if screen.canvas.rows[0] == "" {
		t.Error("spent a row of a full footer on a blank above it")
	}
}

func TestAFooterShorterThanTheTerminalKeepsTheBlanksAboveIt(t *testing.T) {
	screen := NewTerminalOfSize(&strings.Builder{}, 40, 10)
	screen.Line("said before")

	screen.Footer(footerRows(4), 3, 0)

	if len(screen.canvas.rows) != 5 || screen.canvas.rows[0] != "" {
		t.Errorf("drew %q, want one blank above a short footer", screen.canvas.rows)
	}
}

func TestATallFooterKeepsItsHeightWhenContentAppearsAboveIt(t *testing.T) {
	screen := NewTerminalOfSize(&strings.Builder{}, 40, 6)
	rows := footerRows(10)

	screen.Footer(rows, 8, 0)
	if got := len(screen.canvas.rows); got != 5 {
		t.Fatalf("initial footer kept %d rows, want the 5 that leave the terminal a row", got)
	}

	screen.Line("said")
	if got := len(screen.canvas.rows); got != 5 {
		t.Errorf("footer went to %d rows when content appeared above it", got)
	}
}

func TestAFooterAndItsBlanksNeverOutgrowTheTerminal(t *testing.T) {
	for _, lines := range []int{1, 2, 3, 10, 24} {
		screenOutput := &strings.Builder{}
		screen := NewTerminalOfSize(screenOutput, 40, lines)
		screen.Line("said before")
		screenOutput.Reset()

		screen.Footer(footerRows(40), 20, 0)

		if drawn := len(screen.canvas.rows); drawn > lines {
			t.Errorf("height %d drew %d rows, which scrolls every repaint", lines, drawn)
		}
	}
}

func TestAWindowedFooterNeverOutgrowsItsRoomWhenBothEndsAreCut(t *testing.T) {
	for _, lines := range []int{1, 2, 3, 4, 5} {
		screen := NewTerminalOfSize(&strings.Builder{}, 40, lines)

		rows, cursorRow := screen.fitFooter(footerRows(40), 20, 0, screen.lines)

		if len(rows) > lines {
			t.Errorf("height %d kept %d rows, more than the terminal holds", lines, len(rows))
		}
		if cursorRow < 0 || cursorRow >= len(rows) {
			t.Errorf("height %d focused at %d, outside the %d rows kept", lines, cursorRow, len(rows))
		}
	}
}

func TestAWindowedFooterKeepsTheRowsPinnedAtItsHead(t *testing.T) {
	screen := NewTerminalOfSize(&strings.Builder{}, 40, 10)
	rows := append([]string{"head", "label"}, footerRows(30)...)

	fitted, cursorRow := screen.fitFooter(rows, len(rows)-1, 2, 8)

	if len(fitted) != 8 {
		t.Fatalf("kept %d rows, want the 8 there is room for", len(fitted))
	}
	if fitted[0] != "head" || fitted[1] != "label" {
		t.Errorf("kept %q at the head, want the pinned rows", fitted[:2])
	}
	if !strings.Contains(fitted[2], "more lines") {
		t.Errorf("row after the pinned ones is %q, want it to say what was hidden", fitted[2])
	}
	if cursorRow != len(fitted)-1 {
		t.Errorf("focused at %d, want the last row", cursorRow)
	}
}

func TestAFooterWithRoomForOnlyItsFocusBesideItsPinnedRowsKeepsBoth(t *testing.T) {
	screen := NewTerminalOfSize(&strings.Builder{}, 40, 10)
	rows := append([]string{"head", "label"}, footerRows(30)...)
	rows[len(rows)-1] = "focus"

	fitted, cursorRow := screen.fitFooter(rows, len(rows)-1, 2, 3)

	if !slices.Equal(fitted, []string{"head", "label", "focus"}) || cursorRow != 2 {
		t.Errorf("kept %q focused at %d, want the pinned rows over the focus", fitted, cursorRow)
	}
}

func TestAFooterWithNoRoomBesideItsPinnedRowsIsWindowedWhole(t *testing.T) {
	screen := NewTerminalOfSize(&strings.Builder{}, 40, 10)
	rows := append([]string{"head", "label"}, footerRows(30)...)
	rows[len(rows)-1] = "focus"

	fitted, cursorRow := screen.fitFooter(rows, len(rows)-1, 2, 2)

	if len(fitted) != 2 || fitted[cursorRow] != "focus" {
		t.Errorf("kept %q focused at %d, want the focus windowed into both rows", fitted, cursorRow)
	}
	if !strings.Contains(fitted[0], "more lines") {
		t.Errorf("row above the focus is %q, want it to say what was hidden", fitted[0])
	}
}

func TestAFooterWindowedIntoTwoRowsSaysWhatItHidAboveTheFocus(t *testing.T) {
	screen := NewTerminalOfSize(&strings.Builder{}, 40, 10)
	rows := footerRows(30)
	rows[20] = "focus"

	fitted, cursorRow := screen.fitFooter(rows, 20, 0, 2)

	if len(fitted) != 2 || fitted[cursorRow] != "focus" || !strings.Contains(fitted[0], "20 more lines") {
		t.Errorf("kept %q focused at %d, want a notice for the 20 rows above over the focus", fitted, cursorRow)
	}
}
