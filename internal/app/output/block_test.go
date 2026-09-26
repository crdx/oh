package output

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/style"
)

type mutableBlock struct {
	text string
}

func (self *mutableBlock) Rows(_ int) []string {
	return []string{self.text}
}

func TestARefreshInsideASynchronisedUpdateIsDrawnOnceAtItsClose(t *testing.T) {
	screen, screenOutput := region()
	block := &mutableBlock{text: "first"}

	screen.Sync(func() {
		screen.OpenTool(block)
		block.text = "second"
		screen.Refresh()
		block.text = "third"
		screen.Refresh()
	})

	drawn := screenOutput.String()

	for _, unseen := range []string{"first", "second"} {
		if strings.Contains(drawn, unseen) {
			t.Errorf("an unseen state of the block was drawn: %q in %q", unseen, drawn)
		}
	}
	if count := strings.Count(drawn, "third"); count != 1 {
		t.Errorf("the last state of the block was drawn %d times, want once: %q", count, drawn)
	}
}

func TestSealingInsideASynchronisedUpdateDrawsTheRefreshItOwes(t *testing.T) {
	screen, screenOutput := region()
	block := &mutableBlock{text: "running"}

	screen.Sync(func() {
		screen.OpenTool(block)
		block.text = "finished"
		screen.Refresh()
		screen.Seal()
	})

	drawn := screenOutput.String()

	if !strings.Contains(drawn, "finished") {
		t.Errorf("the sealed block lost the refresh it was owed: %q", drawn)
	}
	if strings.Contains(drawn, "running") {
		t.Errorf("a superseded state of the block was drawn: %q", drawn)
	}
}

func TestDiscardingANoticeBlockRestoresTheDrawingOrigin(t *testing.T) {
	screen, _ := screenWithInput()
	before := screen.sealedState()
	beforeFrame := screen.canvas

	handle := screen.OpenPanel(textBlock{text: "temporary notice"})
	if !screen.DiscardBlock(handle) {
		t.Fatal("expected the notice block to remain retractable")
	}

	if got := screen.sealedState(); got != before {
		t.Errorf("drawing state was not restored: got %+v, want %+v", got, before)
	}
	if !slices.Equal(screen.canvas.rows, beforeFrame.rows) || screen.canvas.cursorRow != beforeFrame.cursorRow {
		t.Errorf("input was not restored: got %+v, want %+v", screen.canvas, beforeFrame)
	}
}

func TestAnOldBlockHandleCannotDiscardANewerBlock(t *testing.T) {
	screen, _ := region()

	oldHandle := screen.OpenPanel(textBlock{text: "old"})
	screen.Seal()
	newHandle := screen.OpenPanel(textBlock{text: "new"})

	if screen.DiscardBlock(oldHandle) {
		t.Error("an old handle discarded a newer block")
	}
	if !screen.DiscardBlock(newHandle) {
		t.Error("the current block could not be discarded")
	}
}

func TestANoticeBlockStaysItsOwnBesideALaterLine(t *testing.T) {
	screen, screenOutput := region()
	block := &mutableBlock{text: "unsent"}

	handle := screen.OpenPanel(block)
	screen.Line("the harness said something")

	block.text = "submitted"
	if !screen.RefreshBlock(handle) {
		t.Fatal("a line drawn beside the notice took its handle away")
	}
	if !screen.SealBlock(handle) {
		t.Fatal("a line drawn beside the notice left the notice unsealable")
	}

	drawn := screenOutput.String()
	if !strings.Contains(drawn, "submitted") {
		t.Errorf("the notice kept its superseded state: %q", drawn)
	}
	if !strings.Contains(drawn, "the harness said something") {
		t.Errorf("the line beside the notice was lost: %q", drawn)
	}
}

func framed(rows []string, _ int) []string {
	return slices.Concat([]string{"top"}, rows, []string{"bottom"})
}

func panelRows(screen *Screen) []string {
	rows, _, _ := renderGroupedBlocks(screen.blocks, screen.columns, screen.grouping)

	return rows
}

func TestConsecutivePanelRowsShareOneFrame(t *testing.T) {
	screen, _ := region()

	screen.Panel(textBlock{text: "first notice"}, framed)
	screen.Panel(textBlock{text: "second notice"}, framed)
	screen.Panel(textBlock{text: "third notice"}, framed)

	if len(screen.blocks) != 1 {
		t.Fatalf("the panel was drawn as %d blocks, want one", len(screen.blocks))
	}

	want := []string{"top", "first notice", "second notice", "third notice", "bottom"}
	if got := panelRows(screen); !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAPanelOpenedBesideAToolBlockLeavesItAlone(t *testing.T) {
	screen, _ := region()

	screen.OpenTool(textBlock{text: "read notes.txt"})
	screen.Panel(textBlock{text: "first notice"}, framed)
	screen.Panel(textBlock{text: "second notice"}, framed)

	if len(screen.blocks) != 2 {
		t.Fatalf("the panel was drawn as %d blocks beside the tool, want two", len(screen.blocks))
	}

	want := []string{"read notes.txt", "", "top", "first notice", "second notice", "bottom"}
	if got := panelRows(screen); !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAPanelAfterALineStartsItsOwnFrame(t *testing.T) {
	screen, _ := region()

	screen.Panel(textBlock{text: "first notice"}, framed)
	screen.Line("the harness said something")
	screen.Panel(textBlock{text: "second notice"}, framed)

	want := []string{
		"top", "first notice", "bottom",
		"",
		"the harness said something",
		"",
		"top", "second notice", "bottom",
	}
	if got := panelRows(screen); !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAPanelSealedIntoScrollbackFramesTheNextOneApart(t *testing.T) {
	screen, screenOutput := region()

	screen.Panel(textBlock{text: "first notice"}, framed)
	screen.Seal()
	screen.Panel(textBlock{text: "second notice"}, framed)
	screen.Seal()

	drawn := style.Plain(screenOutput.String())
	if count := strings.Count(drawn, "top"); count != 2 {
		t.Errorf("the sealed panel was reopened: %d frames in %q, want two", count, drawn)
	}
}

func TestDiscardingANoticeBesideALaterLineKeepsTheLine(t *testing.T) {
	screen, screenOutput := region()

	handle := screen.OpenPanel(textBlock{text: "temporary notice"})
	screen.Line("the harness said something")

	if !screen.DiscardBlock(handle) {
		t.Fatal("a line drawn beside the notice left the notice undiscardable")
	}

	screen.Seal()

	drawn := screenOutput.String()
	if !strings.Contains(drawn, "the harness said something") {
		t.Errorf("the line beside the discarded notice was lost: %q", drawn)
	}
}

type rowsBlock struct {
	rows []string
}

func (self *rowsBlock) Rows(_ int) []string {
	return self.rows
}

func TestARegionTallerThanTheTerminalIsWindowedToTheRoomItHas(t *testing.T) {
	screenOutput := &strings.Builder{}
	screen := &Screen{writer: screenOutput, isTerminal: true, canRepaint: true, columns: 40, lines: 8}

	block := &rowsBlock{}
	for i := range 20 {
		block.rows = append(block.rows, "row "+strconv.Itoa(i))
	}
	handle := screen.OpenPanel(block)
	screen.Footer([]string{"> "}, 0, 2)

	block.rows[0] = "the row that was windowed away changed"
	screen.RefreshBlock(handle)

	if got := len(screen.canvas.rows); got > screen.lines {
		t.Errorf("painted %d rows on a terminal of %d", got, screen.lines)
	}
	if !strings.Contains(screen.canvas.rows[0], "more lines") {
		t.Errorf("the window opened with %q, want it to say what it hid", screen.canvas.rows[0])
	}
	if !slices.Contains(screen.canvas.rows, "row 19") {
		t.Errorf("the window lost the newest row: %q", screen.canvas.rows)
	}
}

func TestARegionWithRoomToDrawShowsEveryRow(t *testing.T) {
	screenOutput := &strings.Builder{}
	screen := &Screen{writer: screenOutput, isTerminal: true, canRepaint: true, columns: 40, lines: 24}

	block := &rowsBlock{rows: []string{"first", "second"}}
	handle := screen.OpenPanel(block)
	screen.Footer([]string{"> "}, 0, 2)

	block.rows[0] = "first changed"
	screen.RefreshBlock(handle)

	if !slices.Contains(screen.canvas.rows, "first changed") || !slices.Contains(screen.canvas.rows, "second") {
		t.Errorf("a region with room to draw lost a row: %q", screen.canvas.rows)
	}
}
