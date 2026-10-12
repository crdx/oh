package output

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/width"
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

type looseTextBlock struct {
	textBlock
}

func (looseTextBlock) IsLoose() bool {
	return true
}

func TestOnlyANoticeAfterALooseOneStandsApart(t *testing.T) {
	screen, _ := region()

	screen.Panel(textBlock{text: "first notice"}, framed)
	screen.Panel(textBlock{text: "second notice"}, framed)
	screen.Panel(looseTextBlock{textBlock{text: "third notice\nwith more"}}, framed)
	screen.Panel(textBlock{text: "fourth notice"}, framed)

	want := []string{
		"top",
		"first notice",
		"second notice",
		"third notice",
		"with more",
		"",
		"fourth notice",
		"bottom",
	}
	if got := panelRows(screen); !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStackSpacesOnlyAfterALoosePart(t *testing.T) {
	tight := []StackPart{{Rows: []string{"first"}}, {Rows: []string{"second", "third"}}, {Rows: []string{"fourth"}}}
	if got, want := Stack(tight), []string{"first", "second", "third", "fourth"}; !slices.Equal(got, want) {
		t.Errorf("tight: got %q, want %q", got, want)
	}

	loose := []StackPart{{Rows: []string{"first"}}, {Rows: []string{"second", "third"}, IsLoose: true}, {Rows: []string{"fourth"}}}
	if got, want := Stack(loose), []string{"first", "second", "third", "", "fourth"}; !slices.Equal(got, want) {
		t.Errorf("loose: got %q, want %q", got, want)
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
	if !strings.Contains(screen.canvas.rows[0].Text, "more lines") {
		t.Errorf("the window opened with %q, want it to say what it hid", screen.canvas.rows[0].Text)
	}
	if !slices.Contains(drawnTexts(screen), "row 19") {
		t.Errorf("the window lost the newest row: %q", drawnTexts(screen))
	}
}

func TestAShortRunningCallStaysVisibleBesideATallReport(t *testing.T) {
	screenOutput := &strings.Builder{}
	screen := &Screen{writer: screenOutput, isTerminal: true, canRepaint: true, columns: 40, lines: 8}
	screen.OpenTool(textBlock{text: "lookup RFC 8620 2m 4s"})
	report := &rowsBlock{rows: make([]string, 80)}
	for index := range report.rows {
		report.rows[index] = "subagent report line"
	}
	screen.Panel(report, framed)
	screen.Footer([]string{"> "}, 0, 2)

	rows := drawnTexts(screen)
	if !slices.Contains(rows, "lookup RFC 8620 2m 4s") {
		t.Errorf("short running call was windowed away: %q", rows)
	}
	if !strings.Contains(strings.Join(rows, "\n"), "report") {
		t.Errorf("long report disappeared: %q", rows)
	}
	if !strings.Contains(strings.Join(rows, "\n"), "more lines") {
		t.Errorf("long report did not name its hidden rows: %q", rows)
	}
}

type priorityBlock struct {
	rows     []string
	priority []int
}

func (self priorityBlock) Rows(_ int) []string { return self.rows }

func (self priorityBlock) RowsWithPriority(_ int) ([]string, []int) {
	return self.rows, self.priority
}

func TestPictureRowsCannotDisplaceOtherRunningCalls(t *testing.T) {
	screenOutput := &strings.Builder{}
	screen := &Screen{writer: screenOutput, isTerminal: true, canRepaint: true, columns: 40, lines: 8}
	screen.OpenTool(priorityBlock{
		rows:     []string{"call one", "picture one", "picture two", "call two"},
		priority: []int{0, 3},
	})
	report := &rowsBlock{rows: make([]string, 80)}
	for index := range report.rows {
		report.rows[index] = "report line"
	}
	screen.Panel(report, framed)
	screen.Footer([]string{"> "}, 0, 2)

	rows := drawnTexts(screen)
	for _, call := range []string{"call one", "call two"} {
		if !slices.Contains(rows, call) {
			t.Errorf("lost %q behind a picture or report: %q", call, rows)
		}
	}
	callOne := slices.Index(rows, "call one")
	callTwo := slices.Index(rows, "call two")
	if callOne >= 0 && callTwo >= 0 && (callOne >= callTwo || !strings.Contains(rows[callOne+1], "more lines")) {
		t.Errorf("hidden picture did not stand between its calls: %q", rows)
	}
}

func TestABlankFinalCallCannotPointOutsideTheWindow(t *testing.T) {
	screen := &Screen{columns: 40}
	rows := width.HardRows([]string{"call one", "picture row", ""})
	visible := screen.windowBlock(rows, 1, blockSpan{group: ToolGroup, priority: []int{0, 2}})
	if len(visible) != 1 || visible[0].Text != "call one" {
		t.Errorf("got %v, want the nonblank call row", visible)
	}
}

func TestPriorityCallRowsSurviveEveryPictureAndBlankBoundary(t *testing.T) {
	screen := &Screen{columns: 40}
	for callCount := 1; callCount <= 5; callCount++ {
		for pictureRows := range 7 {
			for trailingBlanks := range 4 {
				rows := make([]string, 0, callCount+pictureRows+trailingBlanks)
				priority := make([]int, 0, callCount)
				for index := range callCount {
					priority = append(priority, len(rows))
					rows = append(rows, "call "+strconv.Itoa(index))
					if index == 0 {
						for range pictureRows {
							rows = append(rows, "picture")
						}
					}
				}
				for range trailingBlanks {
					rows = append(rows, "")
				}
				for room := 1; room <= 8; room++ {
					visible := screen.windowBlock(width.HardRows(rows), room, blockSpan{group: ToolGroup, priority: priority})
					if len(visible) > room {
						t.Errorf("%d calls, %d picture rows, %d blanks, %d-room window drew %d rows", callCount, pictureRows, trailingBlanks, room, len(visible))
					}
					if room > callCount {
						shown := strings.Join(width.Texts(visible), "\n")
						for index := range callCount {
							if !strings.Contains(shown, "call "+strconv.Itoa(index)) {
								t.Errorf("%d calls, %d picture rows, %d blanks, %d-room window hid call %d: %q", callCount, pictureRows, trailingBlanks, room, index, shown)
							}
						}
					}
				}
			}
		}
	}
}

func TestPriorityCallsUseSpareRoomAroundPictures(t *testing.T) {
	screen := &Screen{columns: 40}
	rows := width.HardRows([]string{"call one", "picture one", "picture two", "picture three", "call two"})
	visible := screen.windowPriorityRows(rows, 4, []int{0, 4})
	texts := width.Texts(visible)
	for index, text := range texts {
		texts[index] = style.Plain(text)
	}
	if !slices.Equal(texts, []string{"call one", "⋮ 2 more lines", "picture three", "call two"}) {
		t.Errorf("window drew %q, want the spare row around both calls", texts)
	}
}

func TestSeveralShortCallsStayVisibleBesideATallReport(t *testing.T) {
	screenOutput := &strings.Builder{}
	screen := &Screen{writer: screenOutput, isTerminal: true, canRepaint: true, columns: 40, lines: 10}
	screen.OpenTool(&rowsBlock{rows: []string{"call one", "call two", "call three"}})
	report := &rowsBlock{rows: make([]string, 80)}
	for index := range report.rows {
		report.rows[index] = "report line"
	}
	screen.Panel(report, framed)
	screen.Footer([]string{"> "}, 0, 2)

	rows := drawnTexts(screen)
	for _, call := range []string{"call one", "call two", "call three"} {
		if !slices.Contains(rows, call) {
			t.Errorf("lost %q among the report rows: %q", call, rows)
		}
	}
	if len(rows) > screen.lines {
		t.Errorf("live viewport grew to %d rows on %d-line terminal", len(rows), screen.lines)
	}
}

func TestATinyViewportPrefersTheCallOverTheReport(t *testing.T) {
	screenOutput := &strings.Builder{}
	screen := &Screen{writer: screenOutput, isTerminal: true, canRepaint: true, columns: 40, lines: 3}
	screen.OpenTool(textBlock{text: "lookup still running"})
	screen.Panel(&rowsBlock{rows: []string{"report one", "report two", "report three"}}, framed)
	screen.Footer([]string{"> "}, 0, 2)

	if !slices.Contains(drawnTexts(screen), "lookup still running") {
		t.Errorf("running lookup vanished in a tiny window: %q", drawnTexts(screen))
	}
}

func TestMoreChildrenThanRowsStillNameTheRunningCall(t *testing.T) {
	screenOutput := &strings.Builder{}
	screen := &Screen{writer: screenOutput, isTerminal: true, canRepaint: true, columns: 40, lines: 4}
	screen.OpenTool(textBlock{text: "lookup still running"})
	screen.Panel(textBlock{text: "first report"}, framed)
	screen.Line("another harness notice")
	screen.Footer([]string{"> "}, 0, 2)

	rows := drawnTexts(screen)
	if !slices.Contains(rows, "lookup still running") {
		t.Errorf("lost running call among %d children: %q", len(screen.blocks), rows)
	}
	if !strings.Contains(strings.Join(rows, "\n"), "more blocks") {
		t.Errorf("did not count omitted children: %q", rows)
	}
	if len(rows) > screen.lines {
		t.Errorf("drew %d rows in %d-line terminal", len(rows), screen.lines)
	}
}

func TestManyLiveChildrenAlwaysFitTheTerminal(t *testing.T) {
	for lines := 1; lines <= 12; lines++ {
		for calls := 1; calls <= 6; calls++ {
			for reportLines := 1; reportLines <= 12; reportLines++ {
				screen := &Screen{writer: &strings.Builder{}, isTerminal: true, canRepaint: true, columns: 40, lines: lines}
				toolRows := make([]string, calls)
				for index := range toolRows {
					toolRows[index] = "running call " + strconv.Itoa(index)
				}
				screen.OpenTool(&rowsBlock{rows: toolRows})
				report := &rowsBlock{rows: make([]string, reportLines)}
				for index := range report.rows {
					report.rows[index] = "report line"
				}
				screen.Panel(report, framed)
				screen.Line("other notice")
				screen.Footer([]string{"─", "> ", "─"}, 1, 2)
				if got := len(screen.canvas.rows); got > lines {
					t.Errorf("height %d, %d calls, %d report rows painted %d rows", lines, calls, reportLines, got)
				}
			}
		}
	}
}

func TestAWindowedPanelKeepsItsBottomPadding(t *testing.T) {
	for _, columns := range []int{20, 40} {
		for _, lines := range []int{7, 9} {
			screen := &Screen{writer: &strings.Builder{}, isTerminal: true, canRepaint: true, columns: columns, lines: lines}
			block := &rowsBlock{}
			for i := range 20 {
				block.rows = append(block.rows, "line "+strconv.Itoa(i))
			}
			screen.Panel(block, func(rows []string, width int) []string {
				return slices.Concat([]string{style.Harness("top")}, rows, []string{style.Harness(strings.Repeat(" ", width))})
			})
			screen.Footer([]string{"─", "> ", "─"}, 1, 2)

			shown := drawnTexts(screen)
			if len(shown) > lines {
				t.Errorf("painted %d rows in %d lines: %q", len(shown), lines, shown)
			}
			if len(shown) < 6 || !strings.Contains(shown[0], "more lines") || shown[len(shown)-6] != "line 19" ||
				shown[len(shown)-5] != style.Harness(strings.Repeat(" ", columns)) {
				t.Errorf("width %d height %d windowed to %q, want the last content line followed by the blue padding", columns, lines, shown)
			}
		}
	}
}

func TestATwoRowPanelWindowPrefersContentOverPadding(t *testing.T) {
	screen := &Screen{writer: &strings.Builder{}, isTerminal: true, canRepaint: true, columns: 40, lines: 6}
	block := &rowsBlock{rows: []string{"first", "second", "last word"}}
	screen.Panel(block, func(rows []string, columns int) []string {
		return slices.Concat([]string{style.Harness("top")}, rows, []string{style.Harness(strings.Repeat(" ", columns))})
	})
	screen.Footer([]string{"─", "> ", "─"}, 1, 2)

	shown := drawnTexts(screen)
	if !strings.Contains(shown[0], "more lines") || shown[1] != "last word" {
		t.Errorf("two-row window showed %q, want the notice and the last content line", shown)
	}
}

func TestAPanelBesideAToolKeepsItsBottomPadding(t *testing.T) {
	screen := &Screen{writer: &strings.Builder{}, isTerminal: true, canRepaint: true, columns: 40, lines: 10}
	screen.OpenTool(textBlock{text: "lookup docs"})
	block := &rowsBlock{}
	for i := range 20 {
		block.rows = append(block.rows, "line "+strconv.Itoa(i))
	}
	screen.Panel(block, func(rows []string, columns int) []string {
		return slices.Concat([]string{style.Harness("top")}, rows, []string{style.Harness(strings.Repeat(" ", columns))})
	})
	screen.Footer([]string{"─", "> ", "─"}, 1, 2)

	shown := drawnTexts(screen)
	padding := style.Harness(strings.Repeat(" ", 40))
	if !slices.Contains(shown, padding) || shown[len(shown)-6] != "line 19" {
		t.Errorf("tool and panel windowed to %q, want the last content line and its blue bottom padding", shown)
	}
}

func TestAWindowedRegionKeepsItsGroupingGap(t *testing.T) {
	screenOutput := &strings.Builder{}
	screen := &Screen{writer: screenOutput, isTerminal: true, canRepaint: true, columns: 40, lines: 8}
	screen.DrawAnswer(width.HardRows([]string{"answer"}))
	screen.Seal()
	screen.Footer([]string{"> "}, 0, 2)

	block := &rowsBlock{}
	for i := range 20 {
		block.rows = append(block.rows, "row "+strconv.Itoa(i))
	}
	screen.OpenPanel(block)
	screen.Footer([]string{"> "}, 0, 2)

	if strings.TrimSpace(style.Plain(screen.canvas.rows[0].Text)) != "" {
		t.Errorf("window opened with %q, want its grouping gap", screen.canvas.rows[0].Text)
	}
	if !strings.Contains(screen.canvas.rows[1].Text, "more lines") {
		t.Errorf("window continued with %q, want it to say what it hid", screen.canvas.rows[1].Text)
	}
	if got := len(screen.canvas.rows); got > screen.lines {
		t.Errorf("painted %d rows on a terminal of %d", got, screen.lines)
	}
}

func TestAWindowedRegionDropsItsGroupingGapBeforeHidingEveryContentRow(t *testing.T) {
	screenOutput := &strings.Builder{}
	screen := &Screen{writer: screenOutput, isTerminal: true, canRepaint: true, columns: 40, lines: 4}
	screen.DrawAnswer(width.HardRows([]string{"answer"}))
	screen.Seal()
	screen.Footer([]string{"> "}, 0, 2)

	block := &rowsBlock{}
	for i := range 20 {
		block.rows = append(block.rows, "row "+strconv.Itoa(i))
	}
	screen.OpenPanel(block)
	screen.Footer([]string{"> "}, 0, 2)

	if !strings.Contains(screen.canvas.rows[0].Text, "more lines") {
		t.Errorf("window opened with %q, want it to say what it hid", screen.canvas.rows[0].Text)
	}
	if !slices.Contains(drawnTexts(screen), "row 19") {
		t.Errorf("window lost its newest content row: %q", drawnTexts(screen))
	}
	if got := len(screen.canvas.rows); got > screen.lines {
		t.Errorf("painted %d rows on a terminal of %d", got, screen.lines)
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

	if !slices.Contains(drawnTexts(screen), "first changed") || !slices.Contains(drawnTexts(screen), "second") {
		t.Errorf("a region with room to draw lost a row: %q", drawnTexts(screen))
	}
}
