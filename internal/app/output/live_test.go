package output

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/ansi"
	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/width"
)

func region() (*Screen, *strings.Builder) {
	screenOutput := &strings.Builder{}

	return &Screen{writer: screenOutput, isTerminal: true, canRepaint: true, columns: 40, lines: 24}, screenOutput
}

func TestOnlyTheAnswerIsLinked(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "cmd", "oh", "draw.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("prepare directory: %v", err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("prepare file: %v", err)
	}

	screen, screenOutput := region()
	screen.LinkPathsUnder(link.Roots{Workspace: workspace})
	screen.OpenTool(textBlock{text: style.Subtle("cmd/oh/") + style.Subject("draw.go")})
	screen.DrawAnswer(width.HardRows([]string{"see cmd/oh/draw.go"}))

	if count := strings.Count(screenOutput.String(), "\x1b]8;;file://"); count != 1 {
		t.Errorf("expected only the answer linked, got %d links in %q", count, screenOutput)
	}
}

func TestTerminalNoticePathsAreLinkedAsHostPaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notice.txt")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("prepare file: %v", err)
	}

	screen, screenOutput := region()
	screen.LinkPathsUnder(link.Roots{Scratch: t.TempDir()})
	screen.Line("notice names " + path)

	wantTarget := "file://" + filepath.ToSlash(path)
	if !strings.Contains(screenOutput.String(), "\x1b]8;;"+wantTarget+"\x1b\\") {
		t.Errorf("got drawing %q, want link to %q", screenOutput.String(), wantTarget)
	}
}

func TestPathsStayPlainWhenScrollbackIsRedirected(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "one.go"), nil, 0o600); err != nil {
		t.Fatalf("prepare file: %v", err)
	}

	var screenOutput strings.Builder
	screen := New(&screenOutput).LinkPathsUnder(link.Roots{Workspace: workspace})
	screen.Line("one.go")

	if got := screenOutput.String(); got != "one.go" {
		t.Errorf("got %q, want plain redirected output", got)
	}
}

func TestOnlyTheRowsThatChangedArePaintedAgain(t *testing.T) {
	screen, screenOutput := region()

	screen.DrawAnswer(width.HardRows([]string{"one", "two", "three"}))
	screenOutput.Reset()

	screen.DrawAnswer(width.HardRows([]string{"one", "two", "three!"}))

	got := screenOutput.String()

	if strings.Contains(got, "one") || strings.Contains(got, "two") {
		t.Errorf("expected the rows above the difference to be left alone, got %q", got)
	}

	if !strings.Contains(got, "three!") {
		t.Errorf("expected the row that changed to be drawn, got %q", got)
	}
}

func TestARowAddedBelowTheRestOpensARowOfItsOwn(t *testing.T) {
	screen, screenOutput := region()

	screen.DrawAnswer(width.HardRows([]string{"one", "two"}))
	screenOutput.Reset()

	screen.DrawAnswer(width.HardRows([]string{"one", "two", "three"}))

	if want := "\r\nthree" + eraseRow; !strings.Contains(screenOutput.String(), want) {
		t.Errorf("expected the new row on a row of its own, got %q", screenOutput.String())
	}
}

func TestARowRewrittenHigherUpIsReachedByMovingBackToIt(t *testing.T) {
	screen, screenOutput := region()

	screen.DrawAnswer(width.HardRows([]string{"one", "two", "three"}))
	screenOutput.Reset()

	screen.DrawAnswer(width.HardRows([]string{"one", "TWO", "three"}))

	got := screenOutput.String()

	if want := ansi.Up(1); !strings.Contains(got, want) {
		t.Errorf("expected the cursor to move back a row, got %q", got)
	}

	if !strings.Contains(got, "TWO") || strings.Contains(got, "three") {
		t.Errorf("expected only the row that changed to be drawn again, got %q", got)
	}
}

func TestADifferenceAboveTheScreenIsReportedRatherThanRepaired(t *testing.T) {
	screen, _ := region()

	screen.lines = 4

	rows := []string{"one", "two", "three", "four", "five", "six"}

	if !screen.DrawAnswer(width.HardRows(rows)) {
		t.Fatal("expected the first drawing to be made")
	}

	if len(screen.live.committedRows) != len(rows)-4 {
		t.Fatalf("expected %d rows committed to scrollback, got %q", len(rows)-4, width.Texts(screen.live.committedRows))
	}

	if screen.DrawAnswer(width.HardRows([]string{"ONE", "two", "three", "four", "five", "six"})) {
		t.Error("expected a difference above the screen to be reported")
	}

	if !screen.DrawAnswer(width.HardRows([]string{"one", "two", "three", "four", "five", "SIX"})) {
		t.Error("expected a difference on the screen to be repaired")
	}
}

func TestWritingOutsideTheRegionEndsIt(t *testing.T) {
	screen, _ := region()

	screen.DrawAnswer(width.HardRows([]string{"one", "two"}))
	screen.Line("a line")

	if screen.live.rows != nil {
		t.Errorf("expected the region to be forgotten, got %q", width.Texts(screen.live.rows))
	}

	screen.DrawAnswer(width.HardRows([]string{"three"}))

	if len(screen.live.rows) != 1 {
		t.Errorf("expected a region of its own, got %q", width.Texts(screen.live.rows))
	}
}

func TestWithoutATerminalTheAnswerIsWrittenOnceItIsWhole(t *testing.T) {
	screenOutput := &strings.Builder{}

	screen := New(screenOutput)

	screen.DrawAnswer(width.HardRows([]string{"one"}))
	screen.DrawAnswer(width.HardRows([]string{"one", "two"}))

	if got := screenOutput.String(); got != "" {
		t.Errorf("expected the answer to be held back until it was whole, got %q", got)
	}

	screen.End()

	if got := screenOutput.String(); got != "one\ntwo\n" {
		t.Errorf("expected the whole answer on the way out, got %q", got)
	}
}

func TestAFrameThatShrinksKeepsItsPaintedHeightUntilItIsSealed(t *testing.T) {
	screen, screenOutput := region()

	screen.DrawAnswer(width.HardRows([]string{"one", "two", "three"}))
	screenOutput.Reset()

	if !screen.DrawAnswer(width.HardRows([]string{"one", "two"})) {
		t.Fatal("expected a shorter set of rows to be repaired")
	}

	if len(screen.canvas.rows) != 3 || screen.canvas.rows[2].Text != "" || len(screen.live.rows) != 2 {
		t.Fatalf("expected two content rows held at three painted rows, got %q", drawnTexts(screen))
	}
	if got := screenOutput.String(); strings.Contains(got, moveUp(1)) {
		t.Errorf("expected the cursor not to move up while streaming, got %q", got)
	}

	screenOutput.Reset()
	screen.End()

	got := screenOutput.String()
	if !strings.Contains(got, eraseRow) || strings.Contains(got, "three") {
		t.Errorf("expected sealing to clear below the new last row, got %q", got)
	}
}

func TestRowsThatScrolledOffDoNotReturnWhenTheRegionShrinks(t *testing.T) {
	screen, _ := region()
	screen.lines = 4

	if !screen.DrawAnswer(width.HardRows([]string{"one", "two", "three", "four", "five", "six"})) {
		t.Fatal("expected the first drawing to be made")
	}
	if !screen.DrawAnswer(width.HardRows([]string{"one", "two", "three", "four", "five"})) {
		t.Fatal("expected the visible end of the region to be shortened")
	}
	if len(screen.live.committedRows) != 2 {
		t.Fatalf("expected the first two rows to remain committed, got %q", width.Texts(screen.live.committedRows))
	}
	if screen.DrawAnswer(width.HardRows([]string{"one", "TWO", "three", "four", "five"})) {
		t.Error("expected a change to a row that remains offscreen to require a replay")
	}
}

func TestAFrameWithNoRowsErasesWhatWasDrawn(t *testing.T) {
	screen, screenOutput := region()

	if !screen.DrawAnswer(nil) {
		t.Error("expected nothing drawn against nothing to be no trouble")
	}

	screen.DrawAnswer(width.HardRows([]string{"``"}))
	screenOutput.Reset()

	if !screen.DrawAnswer(nil) {
		t.Fatal("expected an empty frame to be repaired")
	}

	if !strings.Contains(screenOutput.String(), eraseRow) {
		t.Errorf("expected the old row to be cleared, got %q", screenOutput.String())
	}
}

func TestDiscardingLiveReasoningLeavesNoScrollback(t *testing.T) {
	var screenOutput strings.Builder
	screen := New(&screenOutput)

	screen.DrawReasoning([]string{"half a thought"})
	if !screen.DiscardLive() {
		t.Fatal("expected the live region to remain erasable")
	}
	screen.Seal()
	screen.End()

	if screenOutput.String() != "" {
		t.Errorf("discarded reasoning reached scrollback: %q", screenOutput.String())
	}
}

func TestDiscardingLiveReasoningLeavesTheDrawingWhereItWas(t *testing.T) {
	screen, _ := region()

	screen.PanelLine("what the person sent")
	screen.End()
	screen.Footer([]string{"> "}, 0, 2)

	before := screen.sealedState()
	beforeFrame := screen.canvas.rows

	screen.DrawReasoning([]string{"half a thought", "and the rest of it"})
	if !screen.DiscardLive() {
		t.Fatal("expected the reasoning erased in place rather than left for a replay")
	}

	if after := screen.sealedState(); after != before {
		t.Errorf("discarded reasoning left the drawing at %+v, want %+v", after, before)
	}
	if !slices.Equal(screen.canvas.rows, beforeFrame) {
		t.Errorf("discarded reasoning left %q painted, want %q", drawnTexts(screen), width.Texts(beforeFrame))
	}
}

func TestAnAnswerDrawnOverReasoningIsNotLinkedAsAnAnswer(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "one.go"), nil, 0o600); err != nil {
		t.Fatalf("prepare file: %v", err)
	}

	screen, screenOutput := region()
	screen.LinkPathsUnder(link.Roots{Workspace: workspace})
	screen.DrawReasoning([]string{"looking at one.go"})
	screen.DrawAnswer(width.HardRows([]string{"looking at one.go", "the answer names one.go"}))
	screen.Seal()

	if count := strings.Count(screenOutput.String(), "\x1b]8;;file://"); count != 0 {
		t.Errorf("expected a region spanning reasoning and answer left unlinked, got %d links in %q", count, screenOutput)
	}
}

func TestAnAnswerTallerThanItsRoomCommitsItsTopOnce(t *testing.T) {
	screenOutput := &strings.Builder{}
	screen := &Screen{writer: screenOutput, isTerminal: true, canRepaint: true, columns: 40, lines: 6}
	screen.Footer([]string{"─", "> ", "─"}, 1, 2)

	var rows []string
	for i := range 10 {
		rows = append(rows, "row "+strconv.Itoa(i))
		if !screen.DrawAnswer(width.HardRows(slices.Clone(rows))) {
			t.Fatalf("an answer growing by row %d was refused", i)
		}
		if got := len(screen.canvas.rows); got > screen.lines {
			t.Fatalf("painted %d rows on a terminal of %d", got, screen.lines)
		}
	}

	if committedTexts := width.Texts(screen.live.committedRows); !slices.Contains(committedTexts, "row 0") {
		t.Errorf("committed %q, want the top of the answer in scrollback", committedTexts)
	}

	screen.Seal()

	for _, row := range rows {
		if count := strings.Count(screenOutput.String(), row+eraseRow); count != 1 {
			t.Errorf("%q was written %d times, want it written once", row, count)
		}
	}
}

func TestTheFrameNeverOutgrowsTheTerminal(t *testing.T) {
	for _, lines := range []int{1, 2, 3, 6, 12} {
		screen := &Screen{writer: &strings.Builder{}, isTerminal: true, canRepaint: true, columns: 40, lines: lines}
		screen.Line("said before")

		block := &rowsBlock{}
		for i := range 20 {
			block.rows = append(block.rows, "call "+strconv.Itoa(i))
		}
		screen.OpenTool(block)
		screen.InertFooter(footerRows(30), 29, 0)

		if got := len(screen.canvas.rows); got > lines {
			t.Errorf("height %d painted %d rows", lines, got)
		}
	}
}

func TestAWindowedRegionEndsOnARowThatSaysSomething(t *testing.T) {
	screen := &Screen{writer: &strings.Builder{}, isTerminal: true, canRepaint: true, columns: 40, lines: 6}

	block := &rowsBlock{}
	for i := range 10 {
		block.rows = append(block.rows, "call "+strconv.Itoa(i))
	}
	block.rows = append(block.rows, "", " notice", " last word", style.Reasoning.Over("   "), "")
	screen.OpenTool(block)
	screen.Footer([]string{"─", "> ", "─"}, 1, 2)

	if got := drawnTexts(screen)[:2]; !strings.Contains(got[0], "more lines") || got[1] != " last word" {
		t.Errorf("windowed to %q, want the hidden-rows notice over the last row with something in it", got)
	}
}

func drawnTexts(screen *Screen) []string {
	return width.Texts(screen.canvas.rows)
}
