package width_test

import (
	"slices"
	"testing"

	"crdx.org/io/internal/app/width"
)

func rows(count int) []string {
	made := make([]string, count)
	for i := range made {
		made[i] = string(rune('a' + i))
	}
	return made
}

func TestEverythingFitsWithinTheBudget(t *testing.T) {
	window := width.WindowRows(rows(3), 5, 1)

	if !slices.Equal(window.Rows, rows(3)) || window.Focus != 1 {
		t.Errorf("kept %q focused at %d, want everything focused at 1", window.Rows, window.Focus)
	}
	if window.HiddenLinesAbove != 0 || window.HiddenLinesBelow != 0 {
		t.Errorf("hid %d above and %d below, want nothing hidden", window.HiddenLinesAbove, window.HiddenLinesBelow)
	}
}

func TestAFocusAtTheEndShedsFromTheTop(t *testing.T) {
	window := width.WindowRows(rows(6), 2, 5)

	if !slices.Equal(window.Rows, []string{"e", "f"}) {
		t.Errorf("kept %q, want the last two", window.Rows)
	}
	if window.Focus != 1 {
		t.Errorf("focused at %d, want 1", window.Focus)
	}
	if window.HiddenLinesAbove != 4 || window.HiddenLinesBelow != 0 {
		t.Errorf("hid %d above and %d below, want 4 and 0", window.HiddenLinesAbove, window.HiddenLinesBelow)
	}
}

func TestAFocusAtTheStartShedsFromTheBottom(t *testing.T) {
	window := width.WindowRows(rows(6), 2, 0)

	if !slices.Equal(window.Rows, []string{"a", "b"}) {
		t.Errorf("kept %q, want the first two", window.Rows)
	}
	if window.HiddenLinesAbove != 0 || window.HiddenLinesBelow != 4 {
		t.Errorf("hid %d above and %d below, want 0 and 4", window.HiddenLinesAbove, window.HiddenLinesBelow)
	}
}

func TestAFocusInTheMiddleShedsFromBothEnds(t *testing.T) {
	window := width.WindowRows(rows(9), 3, 4)

	if !slices.Equal(window.Rows, []string{"c", "d", "e"}) {
		t.Errorf("kept %q, want the rows around the focus", window.Rows)
	}
	if window.HiddenLinesAbove != 2 || window.HiddenLinesBelow != 4 {
		t.Errorf("hid %d above and %d below, want 2 and 4", window.HiddenLinesAbove, window.HiddenLinesBelow)
	}
}

func TestABudgetBelowOneStillKeepsARow(t *testing.T) {
	for _, budget := range []int{0, -1} {
		window := width.WindowRows(rows(4), budget, 3)

		if len(window.Rows) != 1 {
			t.Errorf("budget %d kept %d rows, want 1", budget, len(window.Rows))
		}
		if window.Focus != 0 {
			t.Errorf("budget %d focused at %d, want 0", budget, window.Focus)
		}
	}
}

func TestTheFocusAlwaysLandsInsideWhatIsKept(t *testing.T) {
	for focus := range 9 {
		window := width.WindowRows(rows(9), 4, focus)

		if window.Focus < 0 || window.Focus >= len(window.Rows) {
			t.Errorf("focus %d fell outside the %d rows kept, at %d", focus, len(window.Rows), window.Focus)
		}
	}
}
