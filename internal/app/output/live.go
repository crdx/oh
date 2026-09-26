package output

import (
	"slices"
	"strings"

	"crdx.org/oh/internal/app/ansi"
	"crdx.org/oh/internal/app/style"
)

const eraseRow = ansi.EraseLine

type liveRegion struct {
	rows          []string
	firstGroup    Group
	lastGroup     Group
	height        int
	committedRows []string
	isStarted     bool
}

func spansOneGroup(firstGroup Group, lastGroup Group, group Group) bool {
	return firstGroup == group && lastGroup == group
}

func (self *Screen) DrawAnswer(rows []string) bool {
	return self.draw(rows, AnswerGroup)
}

func (self *Screen) DrawReasoning(rows []string) bool {
	return self.draw(rows, ReasoningGroup)
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

func (self *Screen) draw(rows []string, group Group) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.blocks) > 0 {
		self.seal()
	}

	if len(rows) == 0 {
		if len(self.live.rows) == 0 {
			return true
		}

		rows = []string{""}
	}

	committedRows := self.live.committedRows
	if len(rows) < len(committedRows) || !slices.Equal(rows[:len(committedRows)], committedRows) {
		return false
	}

	if len(self.live.rows) == 0 {
		self.live.firstGroup = group
	}

	self.live.rows = rows
	self.live.lastGroup = group
	self.changed()

	return true
}

func (self *Screen) hasLiveRegion() bool {
	return len(self.blocks) > 0 || len(self.live.rows) > 0
}

func (self *Screen) liveContent() ([]string, Group, Group) {
	if len(self.blocks) > 0 {
		return renderGroupedBlocks(self.blocks, self.columns, self.grouping)
	}

	return self.live.rows, self.live.firstGroup, self.live.lastGroup
}

func (self *Screen) seal() {
	if !self.hasLiveRegion() {
		self.blocks = nil
		self.live = liveRegion{}
		return
	}

	rows, firstGroup, lastGroup := self.liveContent()
	isAnswer := spansOneGroup(firstGroup, lastGroup, AnswerGroup)
	rest := rows[min(len(self.live.committedRows), len(rows)):]

	switch {
	case !self.live.isStarted:
		self.begin(firstGroup)
	case len(self.live.committedRows) > 0 && len(rest) > 0:
		self.newline()
	}

	if len(rest) > 0 {
		self.write(self.linkifyRows(strings.Join(rest, "\n"), isAnswer))
	}

	self.lastGroup = lastGroup
	self.blocks = nil
	self.live = liveRegion{}
}

func (self *Screen) commit(rows []string, firstGroup Group, isAnswer bool) {
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

	self.write(self.linkifyRows(strings.Join(rows, "\n"), isAnswer))
	self.live.committedRows = append(self.live.committedRows, rows...)
}

func (self *Screen) linkifyRows(text string, isAnswer bool) string {
	if !isAnswer {
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

func (self *Screen) liveFrameRows(room int) []string {
	if !self.hasLiveRegion() {
		return nil
	}

	rows, firstGroup, lastGroup := self.liveContent()
	isAnswer := spansOneGroup(firstGroup, lastGroup, AnswerGroup)

	if padding := self.live.height - len(rows); padding > 0 {
		rows = append(slices.Clone(rows), make([]string, padding)...)
	}
	self.live.height = len(rows)

	if len(self.blocks) > 0 {
		return self.windowed(rows, self.liveGap(firstGroup), room)
	}

	visible := rows[min(len(self.live.committedRows), len(rows)):]
	gap := 0
	if !self.live.isStarted {
		gap = self.liveGap(firstGroup)
	}

	if room >= 0 && gap+len(visible) > room {
		overflow := endingOnARow(visible, max(0, len(visible)-room))
		self.commit(visible[:overflow], firstGroup, isAnswer)
		visible = visible[overflow:]
		gap = 0
	}

	frameRows := make([]string, gap, gap+len(visible))
	for _, row := range visible {
		frameRows = append(frameRows, self.linkifyRows(row, isAnswer))
	}

	return frameRows
}

func endingOnARow(rows []string, count int) int {
	for at := count; at <= len(rows); at++ {
		if at == 0 || rows[at-1] != "" {
			return at
		}
	}

	for at := count; at > 0; at-- {
		if rows[at-1] != "" {
			return at
		}
	}

	return 0
}

func (self *Screen) windowed(rows []string, gap int, room int) []string {
	if room < 0 || gap+len(rows) <= room {
		return append(make([]string, gap), rows...)
	}

	if len(rows) <= room {
		return rows
	}

	rows = withoutTrailingBlankRows(rows)
	if len(rows) <= room {
		return rows
	}

	if room < 2 {
		return rows[len(rows)-room:]
	}

	shownRows := rows[len(rows)-room+1:]

	return append([]string{self.hiddenRowsNotice(len(rows) - len(shownRows))}, shownRows...)
}

func withoutTrailingBlankRows(rows []string) []string {
	end := len(rows)
	for end > 0 && strings.TrimSpace(style.Plain(rows[end-1])) == "" {
		end--
	}

	if end == 0 {
		return rows
	}

	return rows[:end]
}

type canvas struct {
	rows         []string
	cursorRow    int
	cursorColumn int
	isMidRow     bool
}

type frameLayout struct {
	rows         []string
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
			room := max(1, self.lines-1)
			if len(footerRows)+gap > room {
				gap = 0
			}
			footerRows, footerCursorRow = self.fitFooter(footerRows, footerCursorRow, self.input.pinnedRows, room-gap)
		}

		liveRoom = max(0, self.lines-len(footerRows)-gap)
	}

	liveRows := self.liveFrameRows(liveRoom)
	if len(liveRows) == 0 && self.hasLiveRegion() {
		gap = min(gap, self.blankRowsBefore(true))
	}

	rows := make([]string, 0, len(liveRows)+gap+len(footerRows))
	rows = append(rows, liveRows...)

	if len(footerRows) == 0 {
		if len(rows) == 0 {
			return frameLayout{}
		}

		last := len(rows) - 1

		return frameLayout{rows: rows, cursorRow: last, cursorColumn: style.Width(rows[last])}
	}

	rows = append(rows, make([]string, gap)...)
	cursorRow := len(rows) + footerCursorRow
	rows = append(rows, footerRows...)

	return frameLayout{rows: rows, cursorRow: cursorRow, cursorColumn: self.input.cursorColumn}
}

func (self *Screen) paint() {
	self.isFrameOwed = false
	self.measureTerminal()

	layout := self.compose()

	sealedText := strings.ReplaceAll(self.owedText.String(), "\r\n", "\n")
	isFromMidLine := self.isOwedTextFromMidLine
	self.owedText.Reset()

	if len(self.canvas.rows) == 0 && len(layout.rows) == 0 {
		self.append(sealedText, isFromMidLine)
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

	if isFromMidLine {
		sealedText = strings.TrimPrefix(sealedText, "\n")
	}

	committedRows, hasTrailingNewline := rowsOf(sealedText)
	rows := slices.Concat(committedRows, layout.rows)

	row := 0
	first := 0
	if len(self.canvas.rows) > 0 {
		row = self.canvas.cursorRow
		first = getFirstDifference(self.canvas.rows, rows)
	}

	end := max(len(rows), len(self.canvas.rows))
	lastRowOnScreen := max(0, len(self.canvas.rows)-1)

	for at := first; at < end; at++ {
		if at < len(self.canvas.rows) && at < len(rows) && self.canvas.rows[at] == rows[at] {
			continue
		}

		if at <= lastRowOnScreen {
			out.WriteString(moveBetween(row, at))
			out.WriteString("\r")
		} else {
			out.WriteString(moveBetween(row, lastRowOnScreen))
			out.WriteString(strings.Repeat("\r\n", at-lastRowOnScreen))
			lastRowOnScreen = at
		}

		row = at
		out.WriteString(eraseRow)
		if at < len(rows) {
			out.WriteString(rows[at])
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

func (self *Screen) landAfter(committedRows []string, row int, rowsOnScreen int, hasTrailingNewline bool) string {
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
		out.WriteString(moveRight(style.Width(committedRows[last])))
		self.canvas = canvas{isMidRow: true}
	}

	return out.String()
}

func (self *Screen) append(text string, isFromMidLine bool) {
	if !self.canvas.isMidRow && isFromMidLine {
		text = strings.TrimPrefix(text, "\n")
	}

	if text == "" {
		return
	}

	self.raw(strings.ReplaceAll(text, "\n", "\r\n"))
	self.canvas = canvas{isMidRow: !strings.HasSuffix(text, "\n")}
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

func getFirstDifference(before []string, after []string) int {
	for at := range min(len(before), len(after)) {
		if before[at] != after[at] {
			return at
		}
	}

	return min(len(before), len(after))
}
