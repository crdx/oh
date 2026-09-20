package painter

import (
	"slices"
	"strings"
	"testing"

	"crdx.org/io/internal/app/output"
)

func TestShortTextIsDrawnForEveryDeltaThatArrives(t *testing.T) {
	text := liveText{streamingMode: output.StreamingModePaced}

	for range liveTextFraction {
		text.Write("a")

		if !text.IsDue() {
			t.Fatalf("expected every delta of %d bytes to be drawn", text.Len())
		}

		text.MarkDrawn()
	}
}

func TestALongTextIsRedrawnOnceEveryStepAndNoMoreOften(t *testing.T) {
	text := liveText{streamingMode: output.StreamingModePaced}

	text.Write(strings.Repeat("a", 8*liveTextFraction))
	text.MarkDrawn()

	for range 7 {
		text.Write("a")

		if text.IsDue() {
			t.Fatalf("expected %d bytes to be too little to redraw at a length of %d", 7, text.Len())
		}
	}

	text.Write("a")

	if !text.IsDue() {
		t.Errorf("expected the eighth byte to be drawn at a length of %d", text.Len())
	}
}

func TestTheStepStopsGrowingSoThatALongAnswerStaysLive(t *testing.T) {
	text := liveText{streamingMode: output.StreamingModePaced}

	text.Write(strings.Repeat("a", 100*liveTextStepCap))
	text.MarkDrawn()

	if got := text.step(); got != liveTextStepCap {
		t.Errorf("step is %d bytes, want it capped at %d", got, liveTextStepCap)
	}
}

func TestTextThatArrivedWithoutBeingDrawnIsOwedUntilItIsTaken(t *testing.T) {
	text := liveText{streamingMode: output.StreamingModePaced}

	if text.IsOwed() {
		t.Error("expected nothing to be owed before anything arrives")
	}

	text.Write("some text")

	if !text.IsOwed() {
		t.Error("expected text that arrived to be owed")
	}

	text.MarkDrawn()

	if text.IsOwed() {
		t.Error("expected taken text to settle what was owed")
	}
}

func TestHoldingTheUnfinishedRowBackNeverWithdrawsOneAlreadyDrawn(t *testing.T) {
	tests := map[string]struct {
		drawnRowCount int
		rows          []string
		isTailHidden  bool
		want          []string
	}{
		"a settled answer shows every row":        {drawnRowCount: 0, rows: []string{"one", "two"}, isTailHidden: false, want: []string{"one", "two"}},
		"an unfinished row is held back":          {drawnRowCount: 1, rows: []string{"one", "two"}, isTailHidden: true, want: []string{"one"}},
		"a tail that renders to nothing hides it": {drawnRowCount: 2, rows: []string{"one", "two"}, isTailHidden: true, want: []string{"one", "two"}},
		"nothing drawn yet still holds one back":  {drawnRowCount: 0, rows: []string{"one"}, isTailHidden: true, want: []string{}},
		"a render that shrank is not padded out":  {drawnRowCount: 5, rows: []string{"one", "two"}, isTailHidden: true, want: []string{"one", "two"}},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			text := liveText{streamingMode: output.StreamingModeLine, drawnRowCount: test.drawnRowCount}

			rows := test.rows
			if test.isTailHidden {
				rows = text.WithoutLastRow(rows)
			}

			if got := text.Take(rows, test.isTailHidden); !slices.Equal(got, test.want) {
				t.Errorf("Take(%q, %t) = %q, want %q", test.rows, test.isTailHidden, got, test.want)
			}

			if text.drawnRowCount != len(test.want) {
				t.Errorf("drawn rows recorded as %d, want %d", text.drawnRowCount, len(test.want))
			}
		})
	}
}

func TestResettingForgetsWhatWasDrawnAsWellAsWhatArrived(t *testing.T) {
	text := liveText{streamingMode: output.StreamingModePaced}

	text.Write(strings.Repeat("a", 4*liveTextStepCap))
	text.MarkDrawn()
	text.Reset()

	if text.Len() != 0 || text.IsOwed() {
		t.Errorf("expected a reset to leave nothing arrived and nothing owed, got %d bytes", text.Len())
	}

	text.Write("a")

	if !text.IsDue() {
		t.Error("expected the first byte after a reset to be drawn")
	}
}
