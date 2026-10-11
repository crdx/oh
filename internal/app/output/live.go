package output

import (
	"slices"
	"strings"

	"crdx.org/oh/internal/app/ansi"
	"crdx.org/oh/internal/app/graphics"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/width"
)

const eraseRow = ansi.EraseLine

type liveRegion struct {
	rows            []width.ScreenRow
	firstGroup      Group
	lastGroup       Group
	height          int
	committedRows   []width.ScreenRow
	settledRows     int
	isStarted       bool
	shouldLinkPaths bool
}

func spansOneGroup(firstGroup Group, lastGroup Group, group Group) bool {
	return firstGroup == group && lastGroup == group
}

func (self *Screen) DrawAnswer(rows []width.ScreenRow) bool {
	return self.draw(rows, len(rows), AnswerGroup, true)
}

func (self *Screen) DrawLinkedAnswer(rows []width.ScreenRow) bool {
	return self.draw(rows, len(rows), AnswerGroup, false)
}

func (self *Screen) DrawArrivingAnswer(rows []width.ScreenRow, settledRows int) bool {
	return self.draw(rows, settledRows, AnswerGroup, false)
}

func (self *Screen) DrawReasoning(rows []string) bool {
	return self.draw(width.HardRows(rows), len(rows), ReasoningGroup, false)
}

func (self *Screen) DrawArrivingReasoning(rows []string, settledRows int) bool {
	return self.draw(width.HardRows(rows), settledRows, ReasoningGroup, false)
}

func (self *Screen) DiscardLive() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.blocks = nil

	if self.live.isStarted {
		return false
	}

	self.live = liveRegion{}
	self.changed()

	return true
}

func (self *Screen) draw(rows []width.ScreenRow, settledRows int, group Group, shouldLinkPaths bool) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.blocks) > 0 {
		self.seal()
	}

	if len(rows) == 0 {
		if len(self.live.rows) == 0 {
			return true
		}

		if !self.live.isStarted {
			self.live = liveRegion{}
			self.changed()
			return true
		}

		rows = []width.ScreenRow{{}}
	}

	committedRows := self.live.committedRows
	if len(rows) < len(committedRows) || !slices.Equal(rows[:len(committedRows)], committedRows) {
		return false
	}

	if len(self.live.rows) == 0 {
		self.live.firstGroup = group
	}

	self.live.rows = rows
	self.live.settledRows = min(max(settledRows, 0), len(rows))
	self.live.lastGroup = group
	self.live.shouldLinkPaths = shouldLinkPaths
	self.changed()

	return true
}

func (self *Screen) hasLiveRegion() bool {
	return len(self.blocks) > 0 || len(self.live.rows) > 0
}

func (self *Screen) liveContent() ([]width.ScreenRow, Group, Group, []blockSpan) {
	if len(self.blocks) > 0 {
		rows, firstGroup, lastGroup, spans := renderGroupedBlocksWithSpans(self.blocks, self.columns, self.grouping)
		return width.HardRows(rows), firstGroup, lastGroup, spans
	}

	return self.live.rows, self.live.firstGroup, self.live.lastGroup, nil
}

func (self *Screen) seal() {
	if !self.hasLiveRegion() {
		self.blocks = nil
		self.live = liveRegion{}
		return
	}

	rows, firstGroup, lastGroup, _ := self.liveContent()
	shouldLinkPaths := spansOneGroup(firstGroup, lastGroup, AnswerGroup) && self.live.shouldLinkPaths
	rest := rows[min(len(self.live.committedRows), len(rows)):]

	switch {
	case !self.live.isStarted:
		self.begin(firstGroup)
	case len(self.live.committedRows) > 0 && len(rest) > 0:
		self.newline()
	}

	self.writeRows(rest, shouldLinkPaths)

	self.lastGroup = lastGroup
	self.blocks = nil
	self.live = liveRegion{}
}

func (self *Screen) commit(rows []width.ScreenRow, firstGroup Group, shouldLinkPaths bool) {
	if !self.live.isStarted {
		self.begin(firstGroup)
		self.live.isStarted = true
	}

	if len(rows) == 0 {
		return
	}

	if len(self.live.committedRows) > 0 {
		self.newline()
	}

	self.writeRows(rows, shouldLinkPaths)
	self.live.committedRows = append(self.live.committedRows, rows...)
}

func (self *Screen) linkifyRows(text string, shouldLinkPaths bool) string {
	if !shouldLinkPaths {
		return text
	}

	return self.linkifyScrollback(text)
}

func (self *Screen) begin(next Group) {
	self.makeRoomFor(next)

	if self.isMidLine {
		self.newline()
	}

	self.openPendingLine()
}

func (self *Screen) blankRowsBefore(isBlankOwed bool) int {
	if !isBlankOwed || !self.hasPrinted {
		return 0
	}

	newlines := self.trailingNewlines
	if self.isMidLine {
		newlines = 1
	}

	return max(0, apart-newlines)
}

func (self *Screen) liveGap(firstGroup Group) int {
	return self.blankRowsBefore(self.isBlankOwed || !self.grouping.runsOn(self.lastGroup, firstGroup))
}

func (self *Screen) liveFrameRows(room int) []width.ScreenRow {
	if !self.hasLiveRegion() {
		return nil
	}

	rows, firstGroup, lastGroup, spans := self.liveContent()
	shouldLinkPaths := spansOneGroup(firstGroup, lastGroup, AnswerGroup) && self.live.shouldLinkPaths

	if padding := self.live.height - len(rows); padding > 0 {
		rows = append(slices.Clone(rows), make([]width.ScreenRow, padding)...)
	}
	self.live.height = len(rows)

	if len(self.blocks) > 0 {
		return self.windowedBlocks(rows, spans, self.liveGap(firstGroup), room)
	}

	visible := rows[min(len(self.live.committedRows), len(rows)):]
	gap := 0
	if !self.live.isStarted {
		gap = self.liveGap(firstGroup)
	}

	if room >= 0 && gap+len(visible) > room {
		visibleSettledRows := visible[:max(0, self.live.settledRows-len(self.live.committedRows))]
		owedRows := max(0, len(visible)-room)
		overflow := endingOnARow(visibleSettledRows, min(len(visibleSettledRows), owedRows))
		if overflow > 0 || owedRows <= len(visibleSettledRows) {
			self.commit(visible[:overflow], firstGroup, shouldLinkPaths)
			visible = visible[overflow:]
			gap = 0
		}

		if gap+len(visible) > room {
			visible = self.windowed(visible, gap, room)
			gap = 0
		}
	}

	frameRows := make([]width.ScreenRow, gap, gap+len(visible))
	for _, row := range visible {
		frameRows = append(frameRows, width.ScreenRow{
			Text:         self.linkifyRows(row.Text, shouldLinkPaths),
			HasSoftBreak: row.HasSoftBreak,
		})
	}

	return frameRows
}

func endingOnARow(rows []width.ScreenRow, count int) int {
	for at := count; at <= len(rows); at++ {
		if at == 0 || rows[at-1].Text != "" {
			return at
		}
	}

	for at := count; at > 0; at-- {
		if rows[at-1].Text != "" {
			return at
		}
	}

	return 0
}

func (self *Screen) windowedBlocks(rows []width.ScreenRow, spans []blockSpan, gap int, room int) []width.ScreenRow {
	if room < 0 || gap+len(rows) <= room {
		return append(make([]width.ScreenRow, gap), rows...)
	}

	active := make([]int, 0, len(spans))
	for index, span := range spans {
		if span.end > span.start {
			active = append(active, index)
		}
	}
	if len(active) < 2 {
		return self.windowed(rows, gap, room)
	}
	if room <= len(active) {
		return self.summariseCrowdedBlocks(rows, spans, active, room)
	}

	if gap > 0 && room-gap >= len(active)+1 {
		room -= gap
	} else {
		gap = 0
	}

	separators := 0
	for _, index := range active[1:] {
		separators += spans[index].gap
	}
	if room < len(active)+separators {
		separators = 0
	}

	budgets := make([]int, len(spans))
	remainingRows := room - separators
	for _, index := range active {
		if remainingRows == 0 {
			break
		}
		budgets[index] = 1
		remainingRows--
	}
	for _, index := range active {
		for len(spans[index].priority) > budgets[index] && remainingRows > 0 {
			budgets[index]++
			remainingRows--
		}
	}
	for _, index := range active {
		if remainingRows == 0 {
			break
		}
		if spans[index].end-spans[index].start > budgets[index] {
			budgets[index]++
			remainingRows--
		}
	}
	for remainingRows > 0 {
		best := -1
		for _, index := range active {
			rowsNeeded := spans[index].end - spans[index].start - budgets[index]
			if rowsNeeded <= 0 {
				continue
			}
			if best < 0 || rowsNeeded < spans[best].end-spans[best].start-budgets[best] {
				best = index
			}
		}
		if best < 0 {
			break
		}
		budgets[best]++
		remainingRows--
	}

	visible := make([]width.ScreenRow, gap, gap+room)
	previous := -1
	for _, index := range active {
		span := spans[index]
		if previous >= 0 && separators > 0 && span.gap > 0 {
			visible = append(visible, width.ScreenRow{})
		}
		visible = append(visible, self.windowBlock(rows[span.start:span.end], budgets[index], span)...)
		previous = index
	}

	return visible
}

func (self *Screen) summariseCrowdedBlocks(rows []width.ScreenRow, spans []blockSpan, active []int, room int) []width.ScreenRow {
	if room <= 0 {
		return nil
	}
	isSelected := make([]bool, len(spans))
	chosenBlocks := 0
	for _, index := range slices.Backward(active) {
		if spans[index].group == ToolGroup && chosenBlocks < room {
			isSelected[index] = true
			chosenBlocks++
		}
	}
	for _, index := range active {
		if chosenBlocks >= room-1 {
			break
		}
		if !isSelected[index] {
			isSelected[index] = true
			chosenBlocks++
		}
	}
	if chosenBlocks == room && room > 1 {
		for _, index := range active {
			if isSelected[index] {
				isSelected[index] = false
				break
			}
		}
	}

	visible := make([]width.ScreenRow, 0, room)
	hiddenRows := 0
	hiddenBlocks := 0
	for _, index := range active {
		span := spans[index]
		if !isSelected[index] {
			hiddenBlocks++
			hiddenRows += span.end - span.start + span.gap
			continue
		}
		visible = append(visible, self.windowBlock(rows[span.start:span.end], 1, span)...)
		if span.group == ToolGroup {
			hiddenRows += span.end - span.start - 1
		}
	}
	if hiddenBlocks > 0 && len(visible) < room {
		visible = append(visible, width.ScreenRow{Text: self.hiddenBlocksNotice(hiddenBlocks, hiddenRows)})
	}

	return visible
}

func (self *Screen) childHiddenRowsNotice(hiddenRows int, span blockSpan) string {
	notice := self.hiddenRowsNotice(hiddenRows)
	if span.framer != nil {
		return span.framer.FrameHiddenNotice(notice, self.columns)
	}

	return notice
}

func (self *Screen) windowBlock(rows []width.ScreenRow, room int, span blockSpan) []width.ScreenRow {
	if room <= 0 {
		return nil
	}
	rows = withoutTrailingBlankRows(rows)
	if len(rows) <= room {
		return rows
	}
	priority := slices.DeleteFunc(slices.Clone(span.priority), func(index int) bool {
		return index < 0 || index >= len(rows)
	})
	if len(priority) > 0 {
		return self.windowPriorityRows(rows, room, priority)
	}
	if room == 1 {
		if span.group == ToolGroup {
			return rows[:1]
		}
		return []width.ScreenRow{{Text: self.childHiddenRowsNotice(len(rows), span)}}
	}

	head := min(2, max(0, room-2))
	tail := room - head - 1
	shownTail := withVisiblePictureTransmissions(rows[head:], rows[len(rows)-tail:])
	visible := append([]width.ScreenRow{}, rows[:head]...)
	visible = append(visible, width.ScreenRow{Text: self.childHiddenRowsNotice(len(rows)-head-tail, span)})

	return append(visible, shownTail...)
}

func (self *Screen) windowPriorityRows(rows []width.ScreenRow, room int, priority []int) []width.ScreenRow {
	if room == 1 {
		return rows[priority[len(priority)-1] : priority[len(priority)-1]+1]
	}

	first := max(0, len(priority)-(room-1))
	isSelected := make([]bool, len(rows))
	for _, index := range priority[first:] {
		isSelected[index] = true
	}
	spareRows := room - len(priority[first:]) - 1
	for index := len(rows) - 1; index >= 0 && spareRows > 0; index-- {
		if !isSelected[index] {
			isSelected[index] = true
			spareRows--
		}
	}

	hiddenRows := len(rows) - room + 1
	visible := make([]width.ScreenRow, 0, room)
	hasNotice := false
	for index := 0; index < len(rows); {
		if !isSelected[index] {
			if !hasNotice {
				visible = append(visible, width.ScreenRow{Text: self.hiddenRowsNotice(hiddenRows)})
				hasNotice = true
			}
			index++
			continue
		}
		end := index + 1
		for end < len(rows) && isSelected[end] {
			end++
		}
		visible = append(visible, withVisiblePictureTransmissions(rows[:end], rows[index:end])...)
		index = end
	}

	return visible
}

func (self *Screen) windowed(rows []width.ScreenRow, gap int, room int) []width.ScreenRow {
	withGap := func(visible []width.ScreenRow) []width.ScreenRow {
		return append(make([]width.ScreenRow, gap), visible...)
	}
	if room < 0 || gap+len(rows) <= room {
		return withGap(rows)
	}

	if gap > 0 && room-gap >= 2 {
		room -= gap
	} else {
		gap = 0
	}
	if len(rows) <= room {
		return withGap(rows)
	}

	rows = withoutTrailingBlankRows(rows)
	if len(rows) <= room {
		return withGap(rows)
	}

	if room < 2 {
		return withGap(withVisiblePictureTransmissions(rows, rows[len(rows)-room:]))
	}

	hiddenRows := rows[:len(rows)-room+1]
	shownRows := withVisiblePictureTransmissions(rows, rows[len(hiddenRows):])
	visible := append([]width.ScreenRow{{Text: self.hiddenRowsNotice(len(hiddenRows))}}, shownRows...)

	return withGap(visible)
}

func withVisiblePictureTransmissions(rows []width.ScreenRow, shownRows []width.ScreenRow) []width.ScreenRow {
	if len(shownRows) == 0 {
		return shownRows
	}

	prefix := graphics.HiddenPictureTransmissions(width.Texts(rows[:len(rows)-len(shownRows)]), width.Texts(shownRows))
	if prefix == "" {
		return shownRows
	}

	shownRows = slices.Clone(shownRows)
	shownRows[0].Text = prefix + shownRows[0].Text

	return shownRows
}

func withoutTrailingBlankRows(rows []width.ScreenRow) []width.ScreenRow {
	end := len(rows)
	for end > 0 && strings.TrimSpace(style.Plain(rows[end-1].Text)) == "" {
		end--
	}

	if end == 0 {
		return rows
	}

	return rows[:end]
}

type canvas struct {
	rows         []width.ScreenRow
	cursorRow    int
	cursorColumn int
	isMidRow     bool
}

type frameLayout struct {
	rows         []width.ScreenRow
	cursorRow    int
	cursorColumn int
}

func (self *Screen) changed() {
	if !self.canRepaint {
		return
	}

	if self.nestedUpdates > 0 {
		self.isFrameOwed = true
		return
	}

	self.paint()
}

func (self *Screen) compose() frameLayout {
	footerRows := self.input.rows
	footerCursorRow := self.input.cursorRow

	gap := 0
	if len(footerRows) > 0 {
		if self.hasLiveRegion() {
			gap = apart - 1
		} else {
			gap = self.blankRowsBefore(true)
		}
	}

	liveRoom := -1
	if self.lines > 0 {
		if len(footerRows) > 0 {
			room := self.footerRoom()
			if len(footerRows)+gap > room {
				gap = 0
			}
			footerRows, footerCursorRow = self.fitInertFooter(self.input, room-gap)
		}

		liveRoom = max(0, self.lines-len(footerRows)-gap)
	}

	liveRows := self.liveFrameRows(liveRoom)
	if len(liveRows) == 0 && self.hasLiveRegion() {
		gap = min(gap, self.blankRowsBefore(true))
	}

	rows := make([]width.ScreenRow, 0, len(liveRows)+gap+len(footerRows))
	rows = append(rows, liveRows...)

	if len(footerRows) == 0 {
		if len(rows) == 0 {
			return frameLayout{}
		}

		last := len(rows) - 1

		return frameLayout{rows: rows, cursorRow: last, cursorColumn: style.Width(rows[last].Text)}
	}

	rows = append(rows, make([]width.ScreenRow, gap)...)
	cursorRow := len(rows) + footerCursorRow
	rows = append(rows, width.HardRows(footerRows)...)

	return frameLayout{rows: rows, cursorRow: cursorRow, cursorColumn: self.input.cursorColumn}
}

func (self *Screen) paint() {
	self.isFrameOwed = false
	self.measureTerminal()

	layout := self.compose()

	sealedText := strings.ReplaceAll(self.owedText.String(), "\r\n", "\n")
	isFromMidLine := self.isOwedTextFromMidLine
	softBreaks := self.owedSoftBreaks
	self.owedText.Reset()
	self.owedNewlines = 0
	self.owedSoftBreaks = nil

	if len(self.canvas.rows) == 0 && len(layout.rows) == 0 {
		self.append(owedRows(sealedText, softBreaks, !self.canvas.isMidRow && isFromMidLine))
		return
	}

	if sealedText == "" && slices.Equal(self.canvas.rows, layout.rows) &&
		self.canvas.cursorRow == layout.cursorRow && self.canvas.cursorColumn == layout.cursorColumn {
		return
	}

	var out strings.Builder

	out.WriteString(self.openFrame())
	if !self.isWrapping {
		self.isWrapping = true
		out.WriteString(noAutoWrap)
	}

	if len(self.canvas.rows) == 0 && self.canvas.isMidRow {
		out.WriteString("\r\n")
		isFromMidLine = true
	}

	committedRows, hasTrailingNewline := owedRows(sealedText, softBreaks, isFromMidLine)
	rows := withSoftBreaksThatRunOn(slices.Concat(committedRows, layout.rows))
	committedRows = rows[:len(committedRows)]
	layout.rows = rows[len(committedRows):]

	row := 0
	first := 0
	if len(self.canvas.rows) > 0 {
		row = self.canvas.cursorRow
		first = getFirstDifference(self.canvas.rows, rows)
	}

	end := max(len(rows), len(self.canvas.rows))
	lastRowOnScreen := max(0, len(self.canvas.rows)-1)
	wasWritten := false
	isAutoWrapping := false

	for at := first; at < end; at++ {
		isRunningOn := wasWritten && at < len(rows) && rows[at-1].HasSoftBreak
		if !isRunningOn && at < len(self.canvas.rows) && at < len(rows) && self.canvas.rows[at] == rows[at] {
			wasWritten = false
			continue
		}

		switch {
		case isRunningOn:
			lastRowOnScreen = max(lastRowOnScreen, at)
		case at <= lastRowOnScreen:
			out.WriteString(moveBetween(row, at))
			out.WriteString("\r")
		default:
			out.WriteString(moveBetween(row, lastRowOnScreen))
			out.WriteString(strings.Repeat("\r\n", at-lastRowOnScreen))
			lastRowOnScreen = at
		}

		row = at
		wasWritten = true

		if at >= len(rows) {
			out.WriteString(eraseRow)
			continue
		}

		if rows[at].HasSoftBreak && !isAutoWrapping {
			isAutoWrapping = true
			out.WriteString(autoWrap)
		}

		out.WriteString(rows[at].Text)
		if self.columns <= 0 || style.Width(rows[at].Text) < self.columns {
			out.WriteString(eraseRow)
		}

		if !rows[at].HasSoftBreak && isAutoWrapping {
			isAutoWrapping = false
			out.WriteString(noAutoWrap)
		}
	}

	if len(layout.rows) == 0 {
		out.WriteString(self.landAfter(committedRows, row, lastRowOnScreen+1, hasTrailingNewline))
	} else {
		cursorRow := len(committedRows) + layout.cursorRow
		out.WriteString(moveBetween(row, cursorRow))
		out.WriteString("\r")
		out.WriteString(moveRight(layout.cursorColumn))
		self.canvas = canvas{rows: layout.rows, cursorRow: layout.cursorRow, cursorColumn: layout.cursorColumn}
	}

	out.WriteString(self.closeFrame())
	self.raw(out.String())
}

func withSoftBreaksThatRunOn(rows []width.ScreenRow) []width.ScreenRow {
	for at := range rows {
		rows[at].HasSoftBreak = at+1 < len(rows) && runsOn(rows[at], rows[at+1])
	}

	return rows
}

func runsOn(previous width.ScreenRow, row width.ScreenRow) bool {
	return previous.HasSoftBreak && style.Width(row.Text) > 0
}

func owedRows(sealedText string, softBreaks []int, isFromMidLine bool) ([]width.ScreenRow, bool) {
	shift := 0
	if isFromMidLine && strings.HasPrefix(sealedText, "\n") {
		sealedText = sealedText[1:]
		shift = 1
	}

	texts, hasTrailingNewline := rowsOf(sealedText)
	rows := width.HardRows(texts)

	for _, at := range softBreaks {
		if at -= shift; at >= 0 && at < len(rows) {
			rows[at].HasSoftBreak = true
		}
	}

	return rows, hasTrailingNewline
}

func (self *Screen) landAfter(committedRows []width.ScreenRow, row int, rowsOnScreen int, hasTrailingNewline bool) string {
	var out strings.Builder

	switch {
	case len(committedRows) == 0 || hasTrailingNewline:
		anchor := len(committedRows)
		if anchor < rowsOnScreen {
			out.WriteString(moveBetween(row, anchor))
			out.WriteString("\r")
		} else {
			out.WriteString(moveBetween(row, anchor-1))
			out.WriteString("\r\n")
		}

		self.canvas = canvas{}
	default:
		last := len(committedRows) - 1
		out.WriteString(moveBetween(row, last))
		out.WriteString("\r")
		out.WriteString(moveRight(style.Width(committedRows[last].Text)))
		self.canvas = canvas{isMidRow: true}
	}

	return out.String()
}

func (self *Screen) append(rows []width.ScreenRow, hasTrailingNewline bool) {
	if len(rows) == 0 && !hasTrailingNewline {
		return
	}

	rows = withSoftBreaksThatRunOn(rows)

	var out strings.Builder

	for at, row := range rows {
		if at > 0 && !rows[at-1].HasSoftBreak {
			out.WriteString("\r\n")
		}

		if row.HasSoftBreak && self.isWrapping {
			out.WriteString(autoWrap)
		}

		out.WriteString(row.Text)

		if at > 0 && rows[at-1].HasSoftBreak && !row.HasSoftBreak && self.isWrapping {
			out.WriteString(noAutoWrap)
		}
	}

	if hasTrailingNewline {
		out.WriteString("\r\n")
	}

	self.raw(out.String())
	self.canvas = canvas{isMidRow: !hasTrailingNewline}
}

func rowsOf(text string) ([]string, bool) {
	if text == "" {
		return nil, false
	}

	rows := strings.Split(text, "\n")
	if rows[len(rows)-1] == "" {
		return rows[:len(rows)-1], true
	}

	return rows, false
}

func moveBetween(from int, to int) string {
	switch {
	case to < from:
		return moveUp(from - to)
	case to > from:
		return ansi.Down(to - from)
	default:
		return ""
	}
}

func getFirstDifference(before []width.ScreenRow, after []width.ScreenRow) int {
	for at := range min(len(before), len(after)) {
		if before[at] != after[at] {
			return at
		}
	}

	return min(len(before), len(after))
}
