package output

import (
	"io"
	"os"
	"strings"
	"sync"

	"crdx.org/oh/internal/app/escape"
	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/tty"
	"crdx.org/oh/internal/app/width"

	"golang.org/x/term"
)

type Screen struct {
	writer io.Writer

	mutex sync.Mutex

	isMidLine        bool
	hasPendingText   bool
	isBlankOwed      bool
	trailingNewlines int
	lastGroup        Group
	hasPrinted       bool

	isTerminal            bool
	canRepaint            bool
	marksMessages         bool
	isTextSizingSupported bool
	linkRoots             link.Roots
	isProgressReported    bool
	grouping              Grouping

	columns    int
	lines      int
	column     int
	openedRows int

	isWrapping        bool
	nestedUpdates     int
	synchronisedBytes strings.Builder

	input       footer
	shownFooter footer

	liveRegion       liveRegion
	isLiveDirty      bool
	isShrinkOwed     bool
	isRepaintRefused bool
	blocks           []groupedBlock
}

func New(writer io.Writer) *Screen {
	isTerminal := tty.Is(writer)
	self := &Screen{
		writer:        writer,
		isTerminal:    isTerminal,
		canRepaint:    isTerminal,
		marksMessages: isTerminal,
	}

	self.measureTerminal()

	return self
}

func NewTerminalOfSize(writer io.Writer, columns int, lines int) *Screen {
	return &Screen{
		writer:        writer,
		isTerminal:    true,
		canRepaint:    true,
		marksMessages: true,
		columns:       columns,
		lines:         lines,
	}
}

func (self *Screen) AppendOnly() *Screen {
	self.canRepaint = false
	return self
}

func (self *Screen) WithoutMessageMarks() *Screen {
	self.marksMessages = false
	return self
}

func (self *Screen) SetGrouping(grouping Grouping) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.grouping = grouping
}

func (self *Screen) SetTextSizingSupported(isSupported bool) {
	self.isTextSizingSupported = isSupported
}

func (self *Screen) IsTextSizingSupported() bool {
	return self.isTextSizingSupported
}

func (self *Screen) IsTerminal() bool {
	return self.isTerminal
}

func (self *Screen) LinkRoots() link.Roots {
	return self.linkRoots
}

func (self *Screen) LinkPathsUnder(roots link.Roots) *Screen {
	self.linkRoots = roots
	return self
}

func (self *Screen) Line(text string) {
	self.line(text, false, NoticeGroup)
}

func (self *Screen) PanelLine(text string) {
	self.line(text, false, PanelGroup)
}

func (self *Screen) MarkedPanelLine(text string) {
	self.line(text, true, PanelGroup)
}

func (self *Screen) Blank() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.blocks) > 0 {
		return
	}

	self.isBlankOwed = self.hasPrinted
}

func (self *Screen) End() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.seal()

	if self.isMidLine {
		self.newline()
		self.isMidLine = false
		self.hasPendingText = false
	}
}

func (self *Screen) line(text string, isMarked bool, group Group) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	text = self.linkifyScrollback(text)
	if isMarked && self.marksMessages {
		text = escape.MessageMark + text
	}

	if len(self.blocks) > 0 {
		self.blocks = append(self.blocks, groupedBlock{Block: textBlock{text: text}, group: group})
		self.refresh()

		return
	}

	self.seal()
	self.makeRoomFor(group)

	if self.isMidLine {
		self.newline()
	}

	self.write(self.wrapToWidth(text))
}

func (self *Screen) wrapToWidth(text string) string {
	if self.columns <= 0 {
		return text
	}

	return strings.Join(width.Wrap(text, self.columns), "\n")
}

func (self *Screen) drawRow(text string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.makeRoomFor(NoticeGroup)

	self.write(text)
}

func (self *Screen) measureTerminal() {
	file, ok := self.writer.(*os.File)
	if !ok {
		return
	}

	if columns, lines, err := term.GetSize(int(file.Fd())); err == nil {
		self.columns = columns
		self.lines = lines
	}
}

func (self *Screen) newline() {
	self.emit("\n")
}

func (self *Screen) write(text string) {
	if text == "" {
		return
	}

	self.openPendingLine()
	self.emit(text)
}

func (self *Screen) emit(text string) {
	self.hasPrinted = true
	fittedText := self.fit(text)
	self.advance(text)
	self.count(text)
	self.at(fittedText)
}

const apart = 2

func (self *Screen) makeRoomFor(next Group) {
	if !self.grouping.runsOn(self.lastGroup, next) {
		self.isBlankOwed = self.hasPrinted
	}

	self.lastGroup = next
}

func (self *Screen) openPendingLine() {
	if self.hasPendingText {
		self.hasPendingText = false
		self.newline()
	}

	if self.isBlankOwed {
		self.isBlankOwed = false

		for self.trailingNewlines < apart {
			self.newline()
		}
	}
}

func (self *Screen) advance(text string) {
	if index := strings.LastIndex(text, "\n"); index >= 0 {
		self.isMidLine = false
		text = text[index+1:]
	}

	if text != "" {
		self.isMidLine = true
	}
}

func (self *Screen) count(styledText string) {
	text := style.Plain(styledText)
	trailingNewlines := len(text) - len(strings.TrimRight(text, "\n"))

	if trailingNewlines == len(text) {
		self.trailingNewlines += trailingNewlines
		return
	}

	self.trailingNewlines = trailingNewlines
}

func (self *Screen) at(text string) {
	if len(self.shownFooter.rows) == 0 {
		self.raw(text)
		return
	}

	self.redraw(text)
}

func (self *Screen) linkifyScrollback(text string) string {
	if !self.isTerminal || self.linkRoots.IsEmpty() {
		return text
	}

	return link.Render(text, self.linkRoots.WithoutScratch())
}

func (self *Screen) raw(text string) {
	if self.nestedUpdates > 0 {
		self.synchronisedBytes.WriteString(text)
		return
	}

	self.writeRaw(text)
}

func (self *Screen) writeRaw(text string) {
	_, _ = io.WriteString(self.writer, text)
}
