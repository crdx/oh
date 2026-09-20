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

		if self.nestedUpdates == 1 {
			self.flushLiveRegion()
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
	rows            []string
	cursorRow       int
	cursorColumn    int
	column          int
	separators      int
	hasContentAbove bool
	isCursorHidden  bool
}

func (self *Screen) Footer(rows []string, cursorRow int, cursorColumn int) {
	self.showFooter(footer{rows: rows, cursorRow: cursorRow, cursorColumn: cursorColumn})
}

func (self *Screen) InertFooter(rows []string, focusRow int) {
	self.showFooter(footer{rows: rows, cursorRow: focusRow, isCursorHidden: true})
}

func (self *Screen) showFooter(input footer) {
	if !self.canRepaint {
		return
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	input.rows, input.cursorRow = self.fitFooter(input.rows, input.cursorRow)

	if slices.Equal(self.input.rows, input.rows) &&
		self.input.cursorRow == input.cursorRow &&
		self.input.cursorColumn == input.cursorColumn &&
		self.input.isCursorHidden == input.isCursorHidden {
		return
	}

	self.input = input

	self.redraw("")
}

func (self *Screen) fitFooter(rows []string, cursorRow int) ([]string, int) {
	if self.lines <= 0 {
		return rows, cursorRow
	}

	room := max(1, self.lines-self.leastSeparators())
	if len(rows) > room && len(self.input.rows) > room {
		room = min(len(self.input.rows), self.lines)
	}
	if len(rows) <= room {
		return rows, cursorRow
	}

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

func (self *Screen) wantedSeparators() int {
	if !self.hasPrinted {
		return 0
	}

	return max(0, apart-self.trailingNewlines)
}

func (self *Screen) separatorsAbove(footerRows int) int {
	separators := self.wantedSeparators()
	if self.lines <= 0 {
		return separators
	}

	return min(separators, max(0, self.lines-footerRows))
}

func (self *Screen) leastSeparators() int {
	if !self.hasPrinted || self.trailingNewlines > 0 {
		return 0
	}

	return 1
}

func (self *Screen) hiddenRowsNotice(hiddenLines int) string {
	notice := fmt.Sprintf("%s %d more %s", width.VerticalEllipsis, hiddenLines, plural(hiddenLines))

	return style.Rule(self.fit(notice))
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

	landing := ""
	if self.shownFooter.hasContentAbove {
		switch {
		case !shouldKeep:
			landing = "\r" + moveUp(self.openedRows) + clearBelow
		case self.column > 0:
			landing = "\r\n"
		}
	}

	self.raw(self.eraseInput() + landing + autoWrap + showCursor)

	self.input = footer{}
	self.openedRows = 0
	self.isWrapping = false
	self.isMidLine = false
	self.hasPendingText = false
}

func (self *Screen) Reset() {
	if !self.canRepaint {
		return
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.shownFooter = footer{}
	self.input = footer{}
	self.blocks = nil
	self.column = 0
	self.openedRows = 0
	self.isMidLine = false
	self.hasPendingText = false
	self.isBlankOwed = false
	self.trailingNewlines = 0
	self.lastGroup = NoticeGroup
	self.isWrapping = false
	self.hasPrinted = false
	self.liveRegion = liveRegion{}
	self.isLiveDirty = false
	self.isShrinkOwed = false
	self.isRepaintRefused = false

	self.measureTerminal()

	self.raw(clearScreen + clearScrollback)
}

func (self *Screen) redraw(text string) {
	var out strings.Builder

	out.WriteString(self.openFrame())

	if !self.isWrapping {
		self.isWrapping = true

		out.WriteString(noAutoWrap)
	}

	out.WriteString(self.eraseInput())
	out.WriteString(text)
	out.WriteString(self.drawInput())
	out.WriteString(self.closeFrame())

	self.raw(out.String())
}

func (self *Screen) eraseInput() string {
	shownFooter := self.shownFooter
	self.shownFooter = footer{}

	if len(shownFooter.rows) == 0 {
		return ""
	}

	if !shownFooter.hasContentAbove {
		return "\r" + moveUp(shownFooter.cursorRow) + clearBelow
	}

	return "\r" + moveUp(shownFooter.cursorRow) + clearBelow + moveUp(shownFooter.separators) + moveRight(shownFooter.column)
}

func (self *Screen) drawInput() string {
	if len(self.input.rows) == 0 {
		return ""
	}

	self.shownFooter = self.input
	self.shownFooter.column = self.column
	self.shownFooter.hasContentAbove = self.hasPrinted
	self.shownFooter.separators = self.separatorsAbove(len(self.input.rows))

	var out strings.Builder

	for i, row := range self.input.rows {
		switch {
		case i > 0:
			out.WriteString("\r\n")
		case self.shownFooter.separators > 0:
			out.WriteString(strings.Repeat("\r\n", self.shownFooter.separators))
		default:
			out.WriteString("\r")
		}

		out.WriteString(row)
	}

	out.WriteString(moveUp(len(self.input.rows) - 1 - self.input.cursorRow))
	out.WriteString("\r")
	out.WriteString(moveRight(self.input.cursorColumn))

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
