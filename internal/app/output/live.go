package output

import (
	"slices"
	"strings"

	"crdx.org/io/internal/app/ansi"
	"crdx.org/io/internal/app/style"
)

const clearRow = ansi.EraseLine

type drawingState struct {
	column           int
	openedRows       int
	isMidLine        bool
	hasPendingText   bool
	isBlankOwed      bool
	trailingNewlines int
	lastGroup        Group
	hasPrinted       bool
}

type liveRegion struct {
	rows                   []string
	currentContentRowCount int
	firstGroup             Group
	lastGroup              Group
	topRowIndex            int
	origin                 drawingState
	originRowOffset        int
	hasOrigin              bool
}

func spansOneGroup(firstGroup Group, lastGroup Group, group Group) bool {
	return firstGroup == group && lastGroup == group
}

func (self liveRegion) spans(group Group) bool {
	return spansOneGroup(self.firstGroup, self.lastGroup, group)
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
	if len(self.liveRegion.rows) == 0 {
		return true
	}
	if self.liveRegion.topRowIndex > 0 {
		return false
	}
	if self.canRepaint {
		self.repaint(0, []string{""}, false)
		self.liveRegion = liveRegion{}
		return false
	}
	self.liveRegion = liveRegion{}

	return true
}

func (self *Screen) discardBlock() bool {
	if len(self.liveRegion.rows) == 0 {
		return true
	}
	if self.liveRegion.topRowIndex > 0 {
		return false
	}
	if !self.canRepaint {
		self.restoreDrawingState(self.liveRegion.origin)
		self.liveRegion = liveRegion{}
		return true
	}

	var out strings.Builder

	out.WriteString(self.openFrame())
	if !self.isWrapping {
		self.isWrapping = true
		out.WriteString(noAutoWrap)
	}
	out.WriteString(self.eraseInput())
	out.WriteString(moveUp(len(self.liveRegion.rows) - 1))
	out.WriteString("\r")
	out.WriteString(clearBelow)
	out.WriteString(moveUp(self.liveRegion.originRowOffset))
	out.WriteString("\r")
	out.WriteString(moveRight(self.liveRegion.origin.column))

	self.restoreDrawingState(self.liveRegion.origin)
	self.liveRegion = liveRegion{}

	out.WriteString(self.drawInput())
	out.WriteString(self.closeFrame())
	self.raw(out.String())

	return true
}

func (self *Screen) drawingState() drawingState {
	return drawingState{
		column:           self.column,
		openedRows:       self.openedRows,
		isMidLine:        self.isMidLine,
		hasPendingText:   self.hasPendingText,
		isBlankOwed:      self.isBlankOwed,
		trailingNewlines: self.trailingNewlines,
		lastGroup:        self.lastGroup,
		hasPrinted:       self.hasPrinted,
	}
}

func (self *Screen) restoreDrawingState(state drawingState) {
	self.column = state.column
	self.openedRows = state.openedRows
	self.isMidLine = state.isMidLine
	self.hasPendingText = state.hasPendingText
	self.isBlankOwed = state.isBlankOwed
	self.trailingNewlines = state.trailingNewlines
	self.lastGroup = state.lastGroup
	self.hasPrinted = state.hasPrinted
}

func (self *Screen) draw(newRows []string, next Group) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.blocks) > 0 {
		self.seal()
	}

	return self.paint(newRows, next)
}

func (self *Screen) paint(newRows []string, group Group) bool {
	return self.paintGroups(newRows, group, group)
}

func (self *Screen) paintGroups(newRows []string, firstGroup Group, lastGroup Group) bool {
	if len(newRows) == 0 {
		if len(self.liveRegion.rows) == 0 {
			return true
		}

		newRows = []string{""}
	}

	if len(self.liveRegion.rows) == 0 {
		self.liveRegion.firstGroup = firstGroup
		if !self.liveRegion.hasOrigin {
			self.liveRegion.origin = self.drawingState()
			self.liveRegion.hasOrigin = true
		}
	}
	self.liveRegion.lastGroup = lastGroup

	if !self.canRepaint {
		self.liveRegion.rows = newRows
		self.liveRegion.currentContentRowCount = len(newRows)
		return true
	}

	self.measureTerminal()

	contentRowCount := len(newRows)
	if len(newRows) < len(self.liveRegion.rows) {
		newRows = append(slices.Clone(newRows), make([]string, len(self.liveRegion.rows)-len(newRows))...)
	}

	firstDifference := getFirstDifference(self.liveRegion.rows, newRows)

	if firstDifference == len(newRows) && firstDifference == len(self.liveRegion.rows) {
		self.liveRegion.currentContentRowCount = contentRowCount
		return true
	}

	if firstDifference < self.liveRegion.topRowIndex {
		self.isRepaintRefused = true

		return false
	}

	if len(self.liveRegion.rows) == 0 {
		openedRows := self.openedRows
		self.begin(firstGroup)
		self.liveRegion.originRowOffset += self.openedRows - openedRows
	}

	self.repaint(firstDifference, newRows, spansOneGroup(firstGroup, lastGroup, AnswerGroup))
	self.liveRegion.currentContentRowCount = contentRowCount
	self.lastGroup = lastGroup

	return true
}

func (self *Screen) seal() {
	self.flushLiveRegion()
	self.blocks = nil

	if len(self.liveRegion.rows) == 0 {
		return
	}

	if !self.canRepaint {
		self.begin(self.liveRegion.firstGroup)

		text := strings.Join(self.liveRegion.rows, "\n")
		if self.liveRegion.spans(AnswerGroup) {
			text = self.linkifyScrollback(text)
		}

		self.write(text)
	} else {
		self.shrinkLiveRegion()
	}

	self.lastGroup = self.liveRegion.lastGroup
	self.liveRegion = liveRegion{}
}

func (self *Screen) shrinkLiveRegion() {
	if !self.canRepaint {
		return
	}

	if self.liveRegion.currentContentRowCount >= len(self.liveRegion.rows) ||
		self.liveRegion.currentContentRowCount <= self.liveRegion.topRowIndex {
		return
	}

	rows := slices.Clone(self.liveRegion.rows[:self.liveRegion.currentContentRowCount])
	self.repaint(len(rows)-1, rows, self.liveRegion.spans(AnswerGroup))
}

func (self *Screen) begin(next Group) {
	self.makeRoomFor(next)

	if self.isMidLine {
		self.newline()
	}

	self.openPendingLine()
}

func (self *Screen) repaint(first int, rows []string, shouldLinkPaths bool) {
	var out strings.Builder

	out.WriteString(self.openFrame())

	if !self.isWrapping {
		self.isWrapping = true

		out.WriteString(noAutoWrap)
	}

	out.WriteString(self.eraseInput())

	if back := len(self.liveRegion.rows) - 1 - first; back >= 0 {
		out.WriteString(moveUp(back))
	} else if len(self.liveRegion.rows) > 0 {
		out.WriteString("\r\n")
	}

	if len(rows) < len(self.liveRegion.rows) {
		out.WriteString("\r" + clearBelow)
	}

	for at := first; at < len(rows); at++ {
		if at > first {
			out.WriteString("\r\n")
		}

		row := rows[at]
		if shouldLinkPaths {
			row = self.linkifyScrollback(row)
		}

		out.WriteString("\r" + clearRow)
		out.WriteString(row)
	}

	self.settle(rows)

	out.WriteString(self.drawInput())
	out.WriteString(self.closeFrame())

	self.raw(out.String())
}

func (self *Screen) settle(rows []string) {
	self.liveRegion.rows = rows
	self.hasPrinted = true
	self.isMidLine = true
	self.hasPendingText = false
	self.trailingNewlines = 0
	self.column = style.Width(rows[len(rows)-1])

	if self.columns > 0 {
		self.column = min(self.column, self.columns)
	}

	if room := self.lines - len(self.input.rows); self.lines > 0 && len(rows) > room {
		self.liveRegion.topRowIndex = max(self.liveRegion.topRowIndex, len(rows)-room)
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
