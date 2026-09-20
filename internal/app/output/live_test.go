package output

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/ansi"
	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/style"
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
	screen.drawRow(style.Subtle("cmd/oh/") + style.Subject("draw.go"))
	screen.DrawAnswer([]string{"see cmd/oh/draw.go"})

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

	screen.DrawAnswer([]string{"one", "two", "three"})
	screenOutput.Reset()

	screen.DrawAnswer([]string{"one", "two", "three!"})

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

	screen.DrawAnswer([]string{"one", "two"})
	screenOutput.Reset()

	screen.DrawAnswer([]string{"one", "two", "three"})

	if want := "\r\n\r" + clearRow + "three"; !strings.Contains(screenOutput.String(), want) {
		t.Errorf("expected the new row on a row of its own, got %q", screenOutput.String())
	}
}

func TestARowRewrittenHigherUpIsReachedByMovingBackToIt(t *testing.T) {
	screen, screenOutput := region()

	screen.DrawAnswer([]string{"one", "two", "three"})
	screenOutput.Reset()

	screen.DrawAnswer([]string{"one", "TWO", "three"})

	got := screenOutput.String()

	if want := ansi.Up(1); !strings.Contains(got, want) {
		t.Errorf("expected the cursor to move back a row, got %q", got)
	}

	if !strings.Contains(got, "TWO") || !strings.Contains(got, "three") {
		t.Errorf("expected the row and everything under it to be drawn again, got %q", got)
	}
}

func TestADifferenceAboveTheScreenIsReportedRatherThanRepaired(t *testing.T) {
	screen, _ := region()

	screen.lines = 4

	rows := []string{"one", "two", "three", "four", "five", "six"}

	if !screen.DrawAnswer(rows) {
		t.Fatal("expected the first drawing to be made")
	}

	if screen.liveRegion.topRowIndex != len(rows)-4 {
		t.Fatalf("expected %d rows to have scrolled off, got %d", len(rows)-4, screen.liveRegion.topRowIndex)
	}

	if screen.DrawAnswer([]string{"ONE", "two", "three", "four", "five", "six"}) {
		t.Error("expected a difference above the screen to be reported")
	}

	if !screen.DrawAnswer([]string{"one", "two", "three", "four", "five", "SIX"}) {
		t.Error("expected a difference on the screen to be repaired")
	}
}

func TestWritingOutsideTheRegionEndsIt(t *testing.T) {
	screen, _ := region()

	screen.DrawAnswer([]string{"one", "two"})
	screen.Line("a line")

	if screen.liveRegion.rows != nil {
		t.Errorf("expected the region to be forgotten, got %q", screen.liveRegion.rows)
	}

	screen.DrawAnswer([]string{"three"})

	if len(screen.liveRegion.rows) != 1 {
		t.Errorf("expected a region of its own, got %q", screen.liveRegion.rows)
	}
}

func TestWithoutATerminalTheAnswerIsWrittenOnceItIsWhole(t *testing.T) {
	screenOutput := &strings.Builder{}

	screen := New(screenOutput)

	screen.DrawAnswer([]string{"one"})
	screen.DrawAnswer([]string{"one", "two"})

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

	screen.DrawAnswer([]string{"one", "two", "three"})
	screenOutput.Reset()

	if !screen.DrawAnswer([]string{"one", "two"}) {
		t.Fatal("expected a shorter set of rows to be repaired")
	}

	if len(screen.liveRegion.rows) != 3 || screen.liveRegion.rows[2] != "" || screen.liveRegion.currentContentRowCount != 2 {
		t.Fatalf("expected two content rows held at three painted rows, got %q", screen.liveRegion.rows)
	}
	if got := screenOutput.String(); strings.Contains(got, moveUp(1)) {
		t.Errorf("expected the cursor not to move up while streaming, got %q", got)
	}

	screenOutput.Reset()
	screen.End()

	got := screenOutput.String()
	if !strings.Contains(got, clearBelow) || !strings.Contains(got, "two") {
		t.Errorf("expected sealing to repaint the new last row and clear below it, got %q", got)
	}
}

func TestRowsThatScrolledOffDoNotReturnWhenTheRegionShrinks(t *testing.T) {
	screen, _ := region()
	screen.lines = 4

	if !screen.DrawAnswer([]string{"one", "two", "three", "four", "five", "six"}) {
		t.Fatal("expected the first drawing to be made")
	}
	if !screen.DrawAnswer([]string{"one", "two", "three", "four", "five"}) {
		t.Fatal("expected the visible end of the region to be shortened")
	}
	if screen.liveRegion.topRowIndex != 2 {
		t.Fatalf("expected the first two rows to remain offscreen, got top row %d", screen.liveRegion.topRowIndex)
	}
	if screen.DrawAnswer([]string{"one", "TWO", "three", "four", "five"}) {
		t.Error("expected a change to a row that remains offscreen to require a replay")
	}
}

func TestAFrameWithNoRowsErasesWhatWasDrawn(t *testing.T) {
	screen, screenOutput := region()

	if !screen.DrawAnswer(nil) {
		t.Error("expected nothing drawn against nothing to be no trouble")
	}

	screen.DrawAnswer([]string{"``"})
	screenOutput.Reset()

	if !screen.DrawAnswer(nil) {
		t.Fatal("expected an empty frame to be repaired")
	}

	if !strings.Contains(screenOutput.String(), clearRow) {
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

func TestAnAnswerAfterReasoningIsLinkedAsItIsPaintedButNotAsARegion(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "one.go"), nil, 0o600); err != nil {
		t.Fatalf("prepare file: %v", err)
	}

	screen, screenOutput := region()
	screen.LinkPathsUnder(link.Roots{Workspace: workspace})
	screen.DrawReasoning([]string{"looking at one.go"})
	screen.DrawAnswer([]string{"looking at one.go", "the answer names one.go"})
	screen.Seal()

	if count := strings.Count(screenOutput.String(), "\x1b]8;;file://"); count != 1 {
		t.Errorf("expected the painted answer row linked once, got %d links in %q", count, screenOutput)
	}
}
