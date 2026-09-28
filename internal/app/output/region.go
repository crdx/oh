package output

import (
	"fmt"
	"slices"
	"strings"

	"crdx.org/oh/internal/app/ansi"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/width"
)

const (
	clearBelow            = ansi.EraseBelow
	beginFrame            = ansi.BeginFrame
	endFrame              = ansi.EndFrame
	hideCursor            = ansi.HideCursor
	showCursor            = ansi.ShowCursor
	barCursor             = ansi.BarCursor
	defaultCursor         = ansi.DefaultCursor
	clearScreen           = ansi.EraseScreen
	clearScrollback       = ansi.EraseScrollback
	progressIndeterminate = "\x1b]9;4;3\x1b\\"
	progressClear         = "\x1b]9;4;0\x1b\\"
)

func (self *Screen) BeginEditing() func() {
	if !self.canRepaint {
		return func() {}
	}

	self.writeRaw(barCursor)
	return func() { self.writeRaw(defaultCursor) }
}

func (self *Screen) Sync(draw func()) {
	if !self.canRepaint {
		draw()
		return
	}

	self.mutex.Lock()
	self.nestedUpdates++
	self.mutex.Unlock()

	defer func() {
		self.mutex.Lock()
		defer self.mutex.Unlock()

		if self.nestedUpdates == 1 && self.isFrameOwed {
			self.paint()
		}

		self.nestedUpdates--
		if self.nestedUpdates == 0 {
			text := self.synchronisedBytes.String()
			self.synchronisedBytes.Reset()

			if text != "" {
				self.writeRaw(self.openFrame() + text + self.closeFrame())
			}
		}
	}()

	draw()
}

func (self *Screen) openFrame() string {
	if self.nestedUpdates > 0 {
		return ""
	}

	return beginFrame + hideCursor
}

func (self *Screen) closeFrame() string {
	if self.nestedUpdates > 0 {
		return ""
	}

	if self.input.isCursorHidden {
		return endFrame
	}

	return showCursor + endFrame
}

type footer struct {
	rows           []string
	cursorRow      int
	cursorColumn   int
	pinnedRows     int
	isCursorHidden bool
}

func (self *Screen) Footer(rows []string, cursorRow int, cursorColumn int) {
	self.showFooter(footer{rows: rows, cursorRow: cursorRow, cursorColumn: cursorColumn})
}

func (self *Screen) InertFooter(rows []string, focusRow int, pinnedRows int) {
	self.showFooter(footer{rows: rows, cursorRow: focusRow, pinnedRows: pinnedRows, isCursorHidden: true})
}

func (self *Screen) showFooter(input footer) {
	if !self.canRepaint {
		return
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	if slices.Equal(self.input.rows, input.rows) &&
		self.input.cursorRow == input.cursorRow &&
		self.input.cursorColumn == input.cursorColumn &&
		self.input.pinnedRows == input.pinnedRows &&
		self.input.isCursorHidden == input.isCursorHidden {
		return
	}

	self.input = input
	self.changed()
}

const leastWindowedRows = 1

func (self *Screen) FooterRoom() (int, bool) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.lines <= 0 {
		return 0, false
	}

	return self.footerRoom(), true
}

func (self *Screen) footerRoom() int {
	return max(1, self.lines-1)
}

func (self *Screen) fitFooter(rows []string, cursorRow int, pinnedRows int, room int) ([]string, int) {
	room = max(1, room)
	if len(rows) <= room {
		return rows, cursorRow
	}

	if pinnedRows > 0 && pinnedRows <= cursorRow && room-pinnedRows >= leastWindowedRows {
		windowRows, windowCursorRow := self.windowFooter(rows[pinnedRows:], cursorRow-pinnedRows, room-pinnedRows)

		return slices.Concat(rows[:pinnedRows], windowRows), pinnedRows + windowCursorRow
	}

	return self.windowFooter(rows, cursorRow, room)
}

func (self *Screen) windowFooter(rows []string, cursorRow int, room int) ([]string, int) {
	visibleRows := width.WindowRows(rows, room, cursorRow)
	notices := noticesFor(visibleRows)

	for notices > 0 && notices < room {
		shrunkRows := width.WindowRows(rows, room-notices, cursorRow)
		if cutEnds := noticesFor(shrunkRows); cutEnds > notices {
			notices = cutEnds
			continue
		}

		return self.withHiddenRowNotices(shrunkRows)
	}

	if room > 1 && visibleRows.HiddenLinesAbove > 0 {
		windowRows := width.WindowRows(rows, room-1, cursorRow)
		return slices.Concat([]string{self.hiddenRowsNotice(windowRows.HiddenLinesAbove)}, windowRows.Rows), windowRows.Focus + 1
	}

	return visibleRows.Rows, visibleRows.Focus
}

func (self *Screen) withHiddenRowNotices(visibleRows width.Window) ([]string, int) {
	footerRows := make([]string, 0, len(visibleRows.Rows)+2)
	focus := visibleRows.Focus

	if visibleRows.HiddenLinesAbove > 0 {
		footerRows = append(footerRows, self.hiddenRowsNotice(visibleRows.HiddenLinesAbove))
		focus++
	}

	footerRows = append(footerRows, visibleRows.Rows...)

	if visibleRows.HiddenLinesBelow > 0 {
		footerRows = append(footerRows, self.hiddenRowsNotice(visibleRows.HiddenLinesBelow))
	}

	return footerRows, focus
}

func noticesFor(visibleRows width.Window) int {
	notices := 0
	if visibleRows.HiddenLinesAbove > 0 {
		notices++
	}
	if visibleRows.HiddenLinesBelow > 0 {
		notices++
	}

	return notices
}

func (self *Screen) hiddenRowsNotice(hiddenLines int) string {
	notice := fmt.Sprintf("%s %d more %s", width.VerticalEllipsis, hiddenLines, plural(hiddenLines))
	if self.columns > 0 {
		notice = width.Elide(notice, self.columns)
	}

	return style.Rule(notice)
}

func plural(count int) string {
	if count == 1 {
		return "line"
	}

	return "lines"
}

func (self *Screen) WriteEscape(escape string) bool {
	if !self.canRepaint {
		return false
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.raw(escape)

	return true
}

func (self *Screen) ReportProgress(isRunning bool) {
	if !self.canRepaint {
		return
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.setProgress(isRunning)
}

func (self *Screen) RefreshProgress() {
	if !self.canRepaint {
		return
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.isProgressReported {
		self.raw(progressIndeterminate)
	}
}

func (self *Screen) setProgress(isRunning bool) {
	if self.isProgressReported == isRunning {
		return
	}

	sequence := progressClear
	if isRunning {
		sequence = progressIndeterminate
	}

	self.raw(sequence)
	self.isProgressReported = isRunning
}

func (self *Screen) Release(shouldKeep bool) {
	if !self.canRepaint {
		return
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.setProgress(false)

	if shouldKeep {
		self.seal()
	} else {
		self.blocks = nil
		self.live = liveRegion{}
	}

	self.input = footer{}
	self.paint()

	var out strings.Builder

	switch {
	case self.canvas.isMidRow:
		out.WriteString("\r\n")
	default:
		out.WriteString("\r")
	}

	if !shouldKeep && self.hasPrinted {
		rows := self.openedRows
		if self.isMidLine {
			rows++
		}
		if self.lines > 0 {
			rows = min(rows, self.lines-1)
		}
		out.WriteString(eraseRowsAbove(rows))
	}

	out.WriteString(autoWrap + showCursor)
	self.raw(out.String())

	self.canvas = canvas{}
	self.openedRows = 0
	self.isWrapping = false
	self.isMidLine = false
}

func (self *Screen) Reset() {
	if !self.canRepaint {
		return
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.input = footer{}
	self.blocks = nil
	self.live = liveRegion{}
	self.owedText.Reset()
	self.owedNewlines = 0
	self.owedSoftBreaks = nil
	self.canvas = canvas{}
	self.isFrameOwed = false
	self.column = 0
	self.openedRows = 0
	self.isMidLine = false
	self.isBlankOwed = false
	self.trailingNewlines = 0
	self.lastGroup = NoticeGroup
	restoration := ""
	if self.isWrapping {
		restoration = autoWrap
	}
	self.isWrapping = false
	self.hasPrinted = false

	self.measureTerminal()

	self.raw(clearScreen + clearScrollback + restoration)
}

func eraseRowsAbove(rows int) string {
	var out strings.Builder

	out.WriteString(moveUp(rows))
	for row := range rows + 1 {
		if row > 0 {
			out.WriteString("\r\n")
		}
		out.WriteString(eraseRow)
	}
	out.WriteString(moveUp(rows))

	return out.String()
}

func moveUp(rows int) string {
	if rows <= 0 {
		return ""
	}

	return ansi.Up(rows)
}

func moveRight(columns int) string {
	if columns <= 0 {
		return ""
	}

	return ansi.Right(columns)
}

func (self *Screen) Columns() int {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.measureTerminal()

	return self.columns
}
