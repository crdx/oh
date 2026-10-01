package output

import (
	"regexp"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/ansi"
	"crdx.org/oh/internal/app/width"
)

type sealedState struct {
	isMidLine        bool
	isBlankOwed      bool
	trailingNewlines int
	lastGroup        Group
	hasPrinted       bool
	column           int
}

func (self *Screen) sealedState() sealedState {
	return sealedState{
		isMidLine:        self.isMidLine,
		isBlankOwed:      self.isBlankOwed,
		trailingNewlines: self.trailingNewlines,
		lastGroup:        self.lastGroup,
		hasPrinted:       self.hasPrinted,
		column:           self.column,
	}
}

func screenWithInput() (*Screen, *strings.Builder) {
	screenOutput := &strings.Builder{}

	screen := &Screen{writer: screenOutput, isTerminal: true, canRepaint: true}
	screen.Line("said")
	screen.Footer([]string{"> hi"}, 0, 3)
	screenOutput.Reset()

	return screen, screenOutput
}

func TestSynchronisingHoldsNestedFramesBackUntilDrawingFinishes(t *testing.T) {
	screenOutput := &strings.Builder{}
	screen := &Screen{writer: screenOutput, isTerminal: true, canRepaint: true}

	screen.Sync(func() {
		screen.Footer([]string{"> hi"}, 0, 3)
		screen.Sync(func() {
			screen.Line("thinking")
		})

		if screenOutput.Len() != 0 {
			t.Errorf("expected output to be withheld while drawing, got %q", screenOutput.String())
		}
	})

	got := screenOutput.String()
	if strings.Count(got, beginFrame) != 1 || strings.Count(got, endFrame) != 1 {
		t.Errorf("expected one frame around the whole update, got %q", got)
	}

	if !strings.HasPrefix(got, beginFrame+hideCursor) || !strings.HasSuffix(got, showCursor+endFrame) {
		t.Errorf("expected the complete update to be synchronised, got %q", got)
	}
}

func TestAnUnchangedInputIsNotDrawnAgain(t *testing.T) {
	screen, screenOutput := screenWithInput()

	screen.Footer([]string{"> hi"}, 0, 3)

	if got := screenOutput.String(); got != "" {
		t.Errorf("expected an unchanged input to be left alone, got %q", got)
	}
}

func TestAnEscapeCodeIsWrittenWholeAndReportsThatItReachedTheTerminal(t *testing.T) {
	const escapeCode = "\x1b]99;i=1:d=0;VGl0bGU=\x1b\\\x1b]99;i=1;\x1b\\"

	screenOutput := &strings.Builder{}
	screen := &Screen{writer: screenOutput, isTerminal: true, canRepaint: true}

	if !screen.WriteEscape(escapeCode) {
		t.Error("expected a terminal to take the escape code")
	}

	if got := screenOutput.String(); got != escapeCode {
		t.Errorf("got %q, want the escape code whole", got)
	}
}

func TestAnEscapeCodeIsHeldBackUntilTheDrawingAroundItFinishes(t *testing.T) {
	const escapeCode = "\x1b]99;i=1;\x1b\\"

	screenOutput := &strings.Builder{}
	screen := &Screen{writer: screenOutput, isTerminal: true, canRepaint: true}

	screen.Sync(func() {
		screen.WriteEscape(escapeCode)

		if screenOutput.Len() != 0 {
			t.Errorf("expected the escape code to be withheld while drawing, got %q", screenOutput.String())
		}
	})

	if got := screenOutput.String(); !strings.Contains(got, escapeCode) {
		t.Errorf("got %q, want the escape code once the update closed", got)
	}
}

func TestAnEscapeCodeIsNotWrittenToRedirectedOutput(t *testing.T) {
	screenOutput := &strings.Builder{}
	screen := New(screenOutput)

	if screen.WriteEscape("\x1b]99;i=1;\x1b\\") {
		t.Error("expected a redirected screen to refuse the escape code")
	}

	if screenOutput.Len() != 0 {
		t.Errorf("expected nothing to be written, got %q", screenOutput.String())
	}
}

func TestProgressReportsAnIndeterminateTurnAndClearsIt(t *testing.T) {
	screenOutput := &strings.Builder{}
	screen := &Screen{writer: screenOutput, isTerminal: true, canRepaint: true}

	screen.ReportProgress(true)
	screen.ReportProgress(true)
	screen.ReportProgress(false)

	if got, want := screenOutput.String(), progressIndeterminate+progressClear; got != want {
		t.Errorf("got progress report %q, want %q", got, want)
	}
}

func TestProgressIsAnnouncedAgainWhileATurnRuns(t *testing.T) {
	screenOutput := &strings.Builder{}
	screen := &Screen{writer: screenOutput, isTerminal: true, canRepaint: true}

	screen.ReportProgress(true)
	screenOutput.Reset()

	screen.RefreshProgress()

	if got, want := screenOutput.String(), progressIndeterminate; got != want {
		t.Errorf("got progress refresh %q, want %q", got, want)
	}
}

func TestProgressIsNotAnnouncedAgainBetweenTurns(t *testing.T) {
	screenOutput := &strings.Builder{}
	screen := &Screen{writer: screenOutput, isTerminal: true, canRepaint: true}

	screen.RefreshProgress()

	if got := screenOutput.String(); got != "" {
		t.Errorf("expected an idle screen to report nothing, got %q", got)
	}
}

func TestProgressIsNotWrittenToRedirectedOutput(t *testing.T) {
	screenOutput := &strings.Builder{}
	screen := New(screenOutput)

	screen.ReportProgress(true)

	if screenOutput.Len() != 0 {
		t.Errorf("expected no progress report, got %q", screenOutput.String())
	}
}

func TestReleaseClearsActiveProgress(t *testing.T) {
	screen, screenOutput := screenWithInput()
	screen.ReportProgress(true)
	screenOutput.Reset()

	screen.Release(false)

	if got := screenOutput.String(); !strings.Contains(got, progressClear) {
		t.Errorf("expected progress to be cleared, got %q", got)
	}
}

func TestReleasingAUsedConversationComesDownBelowIt(t *testing.T) {
	screen, screenOutput := screenWithInput()

	screen.Release(true)

	got := screenOutput.String()
	if !strings.HasSuffix(got, "\r"+autoWrap+showCursor) || strings.Contains(got, clearBelow) {
		t.Errorf("expected the conversation to be left above the next line, got %q", got)
	}
}

func TestReleasingAConversationAddsNoRowBelowIt(t *testing.T) {
	screen, screenOutput := screenWithInput()
	footerRows := len(screen.canvas.rows)

	screen.Release(true)

	if got := screenOutput.String(); strings.Count(got, "\r\n") >= footerRows {
		t.Errorf("expected to stay within the %d rows the footer held, got %q", footerRows, got)
	}
}

func TestReleasingAnUnusedConversationErasesItsLine(t *testing.T) {
	screen, screenOutput := screenWithInput()

	screen.Release(false)

	if got := screenOutput.String(); !strings.Contains(got, "\r"+eraseRowsAbove(1)+autoWrap+showCursor) {
		t.Errorf("expected the unused conversation line to be erased, got %q", got)
	}
}

func TestReleasingAnUnusedConversationErasesEveryWrappedRow(t *testing.T) {
	screenOutput := &strings.Builder{}
	screen := &Screen{writer: screenOutput, isTerminal: true, canRepaint: true, columns: 4}

	screen.Line("banner")
	screen.Footer([]string{">"}, 0, 0)
	screenOutput.Reset()
	screen.Release(false)

	if got := screenOutput.String(); !strings.Contains(got, "\r"+eraseRowsAbove(2)) || strings.Contains(got, clearBelow) {
		t.Errorf("expected both rows of the unused conversation to be erased, got %q", got)
	}
}

func TestTheInputTakesNoRowOfItsOwnUntilSomethingHasBeenSaid(t *testing.T) {
	screenOutput := &strings.Builder{}

	screen := &Screen{writer: screenOutput, isTerminal: true, canRepaint: true}

	screen.Footer([]string{"> hi"}, 0, 3)

	if got := screenOutput.String(); strings.Contains(got, "\r\n") {
		t.Errorf("expected the input to be drawn where the cursor was, got %q", got)
	}

	screenOutput.Reset()

	screen.Line("thinking")

	got := screenOutput.String()

	if want := "\rthinking" + eraseRow; !strings.Contains(got, want) {
		t.Errorf("expected what was said to take the row the input was on, got %q", got)
	}

	if want := "thinking" + eraseRow + "\r\n" + eraseRow + "\r\n> hi" + eraseRow; !strings.Contains(got, want) {
		t.Errorf("expected the input to move under what was said with a blank row, got %q", got)
	}
}

func TestFinishingTheConversationKeepsTheFooterInPlace(t *testing.T) {
	screenOutput := &strings.Builder{}
	screen := &Screen{writer: screenOutput, isTerminal: true, canRepaint: true}
	screen.Footer([]string{"> hi"}, 0, 3)
	screen.Line("thinking")
	screenOutput.Reset()

	screen.End()

	if got := screenOutput.String(); strings.Contains(got, "> hi") {
		t.Errorf("expected the existing blank row to keep the footer in place, got %q", got)
	}
}

func TestWritingToTheConversationPutsTheInputBackWithTheCursorInIt(t *testing.T) {
	screen, screenOutput := screenWithInput()

	screen.Line("thinking")

	want := "> hi" + eraseRow + "\r" + ansi.Right(3) + showCursor + endFrame

	if got := screenOutput.String(); !strings.HasSuffix(got, want) {
		t.Errorf("expected the input to be put back under it, got %q", got)
	}
}

func TestTheInputIsTakenOffTheScreenBeforeTheConversationIsWrittenTo(t *testing.T) {
	screen, screenOutput := screenWithInput()

	screen.Line("thinking")

	want := ansi.Up(1) + "\rthinking" + eraseRow + ansi.Down(1) + "\r" + eraseRow

	got := screenOutput.String()

	eraseIndex := strings.Index(got, want)
	if eraseIndex < 0 {
		t.Fatalf("expected the input's row to be taken over and cleared, got %q", got)
	}

	if drawIndex := strings.LastIndex(got, "> hi"); drawIndex < eraseIndex {
		t.Errorf("expected the input to come back below its cleared row, got %q", got)
	}
}

func TestNothingIsDrawnAtAPlaceTheScreenCanScrollAwayFrom(t *testing.T) {
	screen, screenOutput := screenWithInput()

	screen.Line("thinking")
	screen.Footer([]string{"> hi there"}, 0, 9)

	absolute := regexp.MustCompile(`\x1b\[[0-9;]*[Hfr]|\x1b7|\x1b8`)

	if got := screenOutput.String(); absolute.MatchString(got) {
		t.Errorf("expected every move to be relative, got %q", got)
	}
}

func TestAnInputOfSeveralRowsIsTakenOffAndPutBackWhole(t *testing.T) {
	screen, screenOutput := screenWithInput()

	screen.Footer([]string{"> one", "two", "three"}, 1, 2)
	screenOutput.Reset()

	screen.Line("thinking")

	got := screenOutput.String()

	if want := ansi.Up(2) + "\rthinking" + eraseRow; !strings.Contains(got, want) {
		t.Errorf("expected the erase to start above the top row of the input, got %q", got)
	}

	if want := "three" + eraseRow + ansi.Up(1); !strings.Contains(got, want) || !strings.Contains(got, "> one"+eraseRow) || !strings.Contains(got, "two"+eraseRow) {
		t.Errorf("expected every row back, cursor on the second, got %q", got)
	}
}

func TestResettingClearsTheScreenWithoutErasingFromAStaleRecord(t *testing.T) {
	screen, screenOutput := screenWithInput()
	screen.openedRows = 2

	screen.Reset()

	got := screenOutput.String()

	if !strings.Contains(got, clearScreen) || !strings.Contains(got, clearScrollback) {
		t.Errorf("expected the screen and the scrollback to be cleared, got %q", got)
	}

	if strings.Contains(got, clearBelow) {
		t.Errorf("expected nothing to be erased where the input used to be, got %q", got)
	}

	if screen.canvas.rows != nil || screen.input.rows != nil {
		t.Errorf("expected both footers to be forgotten, got %v and %v", screen.canvas, screen.input)
	}

	if screen.column != 0 || screen.openedRows != 0 || screen.isMidLine || screen.hasPrinted || screen.lastGroup != NoticeGroup || screen.isWrapping {
		t.Errorf("expected the screen to be forgotten, got %+v", screen)
	}
}

func TestWritingWithNoInputShownIsLeftAlone(t *testing.T) {
	screen, screenOutput := screenWithInput()
	screen.input = footer{}
	screen.canvas = canvas{}

	screen.Line("thinking")

	if got := screenOutput.String(); got != "thinking" {
		t.Errorf("expected the text and nothing else, got %q", got)
	}
}

func TestAnInertFooterLeavesTheCursorHidden(t *testing.T) {
	screenOutput := &strings.Builder{}
	screen := &Screen{writer: screenOutput, isTerminal: true, canRepaint: true}

	screen.InertFooter([]string{"> hi"}, 0, Pins{}, 0)

	got := screenOutput.String()
	if strings.Contains(got, showCursor) {
		t.Errorf("expected the cursor to stay hidden while the input is inert, got %q", got)
	}
	if !strings.HasPrefix(got, beginFrame+hideCursor) || !strings.HasSuffix(got, endFrame) {
		t.Errorf("expected a complete frame around the inert footer, got %q", got)
	}
}

func TestTheCursorComesBackWhenTheInputIsTakenAgain(t *testing.T) {
	screenOutput := &strings.Builder{}
	screen := &Screen{writer: screenOutput, isTerminal: true, canRepaint: true}

	screen.InertFooter([]string{"> hi"}, 0, Pins{}, 0)
	screenOutput.Reset()

	screen.Footer([]string{"> hi"}, 0, 3)

	if got := screenOutput.String(); !strings.HasSuffix(got, showCursor+endFrame) {
		t.Errorf("expected the cursor back once the input is taken again, got %q", got)
	}
}

func TestAHiddenRowsNoticeNeitherWrapsNorMovesTheDrawing(t *testing.T) {
	screen, _ := region()

	screen.DrawAnswer(width.HardRows([]string{strings.Repeat("x", screen.columns)}))
	before := screen.sealedState()

	notice := screen.hiddenRowsNotice(3)

	if strings.Contains(notice, "\n") {
		t.Errorf("a hidden-rows notice wrapped where the answer left the cursor: %q", notice)
	}
	if after := screen.sealedState(); after != before {
		t.Errorf("measuring a hidden-rows notice left the drawing at %+v, want %+v", after, before)
	}
}
