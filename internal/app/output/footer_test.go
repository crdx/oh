package output

import (
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

	rows, cursorRow := screen.fitFooter(footerRows(4), 3)

	if len(rows) != 4 || cursorRow != 3 {
		t.Errorf("kept %d rows focused at %d, want 4 at 3", len(rows), cursorRow)
	}
}

func TestAFooterTallerThanTheTerminalIsCutToFit(t *testing.T) {
	screen := NewTerminalOfSize(&strings.Builder{}, 40, 10)

	rows, cursorRow := screen.fitFooter(footerRows(40), 39)

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

	rows, cursorRow := screen.fitFooter(footerRows(40), 20)

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

	rows, _ := screen.fitFooter(footerRows(40), 38)

	last := rows[len(rows)-1]
	if !strings.Contains(last, "1 more line") || strings.Contains(last, "lines") {
		t.Errorf("last row is %q, want one line said in the singular", last)
	}
}

func TestAOneRowOverflowHidesTwoLinesBecauseTheNoticeCostsOne(t *testing.T) {
	screen := NewTerminalOfSize(&strings.Builder{}, 40, 10)

	rows, _ := screen.fitFooter(footerRows(11), 10)

	if !strings.Contains(rows[0], "2 more lines") {
		t.Errorf("first row is %q, want the notice to count itself", rows[0])
	}
}

func TestAFooterIsLeftAloneWhenTheHeightIsUnknown(t *testing.T) {
	screen := New(&strings.Builder{})

	rows, cursorRow := screen.fitFooter(footerRows(40), 39)

	if len(rows) != 40 || cursorRow != 39 {
		t.Errorf("kept %d rows focused at %d, want all 40 at 39", len(rows), cursorRow)
	}
}

func TestATerminalTooShortForANoticeStillFits(t *testing.T) {
	for _, lines := range []int{1, 2} {
		screen := NewTerminalOfSize(&strings.Builder{}, 40, lines)

		rows, cursorRow := screen.fitFooter(footerRows(40), 39)

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

	if got := len(screen.shownFooter.rows); got != 9 {
		t.Errorf("kept %d rows, want 9 beneath the row said before", got)
	}
	if got := screen.shownFooter.separators; got != 1 {
		t.Errorf("drew %d blanks above the footer, want the one that spares that row", got)
	}
}

func TestAFooterShorterThanTheTerminalKeepsTheBlanksAboveIt(t *testing.T) {
	screen := NewTerminalOfSize(&strings.Builder{}, 40, 10)
	screen.Line("said before")

	screen.Footer(footerRows(4), 3, 0)

	if got := screen.shownFooter.separators; got != apart {
		t.Errorf("drew %d blanks above a short footer, want %d", got, apart)
	}
}

func TestATallFooterKeepsItsHeightWhenContentAppearsAboveIt(t *testing.T) {
	screen := NewTerminalOfSize(&strings.Builder{}, 40, 6)
	rows := footerRows(10)

	screen.Footer(rows, 8, 0)
	if got := len(screen.input.rows); got != 6 {
		t.Fatalf("initial footer kept %d rows, want 6", got)
	}

	screen.hasPrinted = true
	screen.Footer(rows, 8, 0)
	if got := len(screen.input.rows); got != 6 {
		t.Errorf("footer shrank to %d rows when content appeared above it", got)
	}
}

func TestAFooterAndItsBlanksNeverOutgrowTheTerminal(t *testing.T) {
	for _, lines := range []int{1, 2, 3, 10, 24} {
		screenOutput := &strings.Builder{}
		screen := NewTerminalOfSize(screenOutput, 40, lines)
		screen.Line("said before")
		screenOutput.Reset()

		screen.Footer(footerRows(40), 20, 0)

		drawn := len(screen.shownFooter.rows) + screen.shownFooter.separators
		if drawn > lines {
			t.Errorf("height %d drew %d rows, which scrolls every repaint", lines, drawn)
		}
	}
}

func TestAWindowedFooterNeverOutgrowsItsRoomWhenBothEndsAreCut(t *testing.T) {
	for _, lines := range []int{1, 2, 3, 4, 5} {
		screen := NewTerminalOfSize(&strings.Builder{}, 40, lines)

		rows, cursorRow := screen.fitFooter(footerRows(40), 20)

		if len(rows) > lines {
			t.Errorf("height %d kept %d rows, more than the terminal holds", lines, len(rows))
		}
		if cursorRow < 0 || cursorRow >= len(rows) {
			t.Errorf("height %d focused at %d, outside the %d rows kept", lines, cursorRow, len(rows))
		}
	}
}
