package edit

import (
	"strings"
	"time"
	"unicode"

	"crdx.org/oh/internal/app/key"
)

type Action int

const (
	DrawInput Action = iota
	AcceptInput
	ForceAcceptInput
	ContinueTurn
	CancelTurn
	QuitSession
	ToggleWrite
	ToggleShell
	ToggleGit
	ToggleLookup
	ToggleNetwork
	ToggleToolGroup
	EditDraft
)

type Input struct {
	buffer     *Buffer
	history    *History
	recall     *Recall
	search     *historySearch
	frameWidth int

	isPasting       bool
	pasteStart      int
	isPrefixPending bool
	isEnterPending  bool
	isClearPending  bool
	acceptAfter     time.Time
	continueAfter   time.Time
	continueBefore  time.Time
	currentTime     func() time.Time
	toolGroup       string
	wasRunning      bool
}

func NewInput(history *History) *Input {
	self := &Input{
		history:     history,
		currentTime: time.Now,
	}
	self.Reset()

	return self
}

func (self *Input) TakeTimeFrom(clock func() time.Time) {
	self.currentTime = clock
}

func (self *Input) Reset() {
	self.buffer = &Buffer{}
	self.search = nil
	self.isPasting = false
	self.isPrefixPending = false
	self.isEnterPending = false
	self.isClearPending = false
	self.acceptAfter = time.Time{}
	self.continueBefore = time.Time{}
	self.wasRunning = false

	if self.history != nil {
		self.recall = self.history.recall()
	}
}

func (self *Input) Text() string {
	return self.buffer.String()
}

func (self *Input) SetText(text string) {
	self.finishSearch()
	self.buffer.Set(text)
}

func (self *Input) Insert(text string) {
	self.finishSearch()
	self.buffer.Insert([]rune(text))
}

func (self *Input) Runes() []rune {
	return self.buffer.Runes()
}

func (self *Input) Cursor() int {
	return self.buffer.Cursor()
}

func (self *Input) Replace(start int, end int, text string) {
	self.finishSearch()
	self.buffer.remove(start, end)
	self.buffer.cursor = start
	self.buffer.Insert([]rune(text))
}

func (self *Input) IsSearching() bool {
	return self.search != nil
}

func (self *Input) IsPasting() bool {
	return self.isPasting
}

func (self *Input) IsPrefixPending() bool {
	return self.isPrefixPending
}

func (self *Input) ToolGroup() string {
	return self.toolGroup
}

type Frame struct {
	Rows             []string
	Row              int
	Column           int
	IsSearching      bool
	SearchQuery      string
	HiddenLinesAbove int
	HiddenLinesBelow int
}

func (self *Input) Frame(width int) Frame {
	self.frameWidth = width
	rows, cursorRow, cursorColumn := layout(self.buffer, width)

	framedRows := window(rows, cursorRow)
	framedRows.Column = cursorColumn
	if self.search != nil {
		framedRows.IsSearching = true
		framedRows.SearchQuery = self.search.getQuery()
	}

	return framedRows
}

func (self *Input) InsertPasted(text string) {
	position := self.buffer.Cursor()
	runes := self.buffer.Runes()
	isAtLineEnd := position == len(runes) || runes[position] == '\n'

	self.buffer.Insert([]rune(preparePastedText(text, string(runes[:position]), isAtLineEnd)))
}

func isPastable(character rune) bool {
	return character == '\t' || character == '\n' || !unicode.IsControl(character)
}

func (self *Input) Apply(keypress key.Key, isRunning bool) Action {
	if self.wasRunning != isRunning {
		self.isEnterPending = false
	}
	self.wasRunning = isRunning

	if self.applyClearKey(keypress) {
		return DrawInput
	}

	self.isClearPending = false

	if self.isPasting {
		self.isEnterPending = false
		self.paste(keypress)
		return DrawInput
	}

	if self.applySearchKey(keypress) {
		return DrawInput
	}

	if self.applyReadlineKey(keypress) {
		return DrawInput
	}

	if keypress.Code != key.Enter || keypress.Mod.Has(key.Shift) {
		self.isEnterPending = false
	}

	if self.isPrefixPending {
		return self.toggleMode(keypress)
	}

	switch keypress.Code {
	case key.Escape:
		return CancelTurn

	case key.Enter:
		return self.enter(keypress)

	case key.Left:
		if keypress.Mod.Has(key.Ctrl) {
			self.buffer.MoveWordLeft()
		} else {
			self.buffer.MoveLeft()
		}

	case key.Right:
		if keypress.Mod.Has(key.Ctrl) {
			self.buffer.MoveWordRight()
		} else {
			self.buffer.MoveRight()
		}

	case key.Up:
		if !moveCursorVertically(self.buffer, self.frameWidth, -1) {
			self.walk(-1)
		}

	case key.Down:
		if !moveCursorVertically(self.buffer, self.frameWidth, 1) {
			self.walk(1)
		}

	case key.Home:
		self.buffer.MoveHome()

	case key.End:
		self.buffer.MoveEnd()

	case key.Delete:
		if keypress.Mod.Has(key.Ctrl) {
			self.buffer.DeleteWordForward()
		} else {
			self.buffer.DeleteForward()
		}

	case key.Backspace:
		if keypress.Mod.Has(key.Ctrl) {
			self.buffer.DeleteWordBackward()
		} else {
			self.buffer.DeleteBackward()
		}

	case key.PasteStart:
		self.isPasting = true
		self.pasteStart = self.buffer.Cursor()

	case key.Rune:
		return self.rune(keypress, isRunning)

	case key.PageUp, key.PageDown, key.PasteEnd, key.Clipboard, key.FocusIn, key.FocusOut, key.Unknown:
	}

	return DrawInput
}

func (self *Input) applyClearKey(keypress key.Key) bool {
	if keypress.Code != key.Rune || !keypress.Mod.Has(key.Ctrl) {
		return false
	}

	switch keypress.Value {
	case 'u':
		self.Reset()
		return true
	case 'c':
		if self.buffer.Len() > 0 && !self.isClearPending {
			self.isClearPending = true
			return true
		}

		self.Reset()
		return true
	}

	return false
}

func (self *Input) applyReadlineKey(keypress key.Key) bool {
	if keypress.Code != key.Rune || !keypress.Mod.Has(key.Ctrl) {
		return false
	}

	switch keypress.Value {
	case 'a':
		self.buffer.MoveHome()
	case 'b':
		self.buffer.MoveLeft()
	case 'e':
		self.buffer.MoveEnd()
	case 'f':
		self.buffer.MoveRight()
	case 'k':
		self.buffer.DeleteToEnd()
	case 'r':
		self.startSearch()
	case 'w':
		self.buffer.DeleteWhitespaceWordBackward()
	default:
		return false
	}

	return true
}

func (self *Input) applySearchKey(keypress key.Key) bool {
	if self.search == nil {
		return false
	}

	switch {
	case keypress.Code == key.Rune && keypress.Value == 'r' && keypress.Mod.Has(key.Ctrl):
		self.search.previous()
		self.buffer.Set(self.search.getText())
		return true
	case keypress.Code == key.Rune && keypress.Mod == 0:
		self.search.add(keypress.Value)
		self.buffer.Set(self.search.getText())
		return true
	case keypress.Code == key.Backspace && keypress.Mod == 0:
		self.search.deleteBackward()
		self.buffer.Set(self.search.getText())
		return true
	case keypress.Code == key.Escape:
		self.finishSearch()
		return true
	default:
		self.finishSearch()
		return false
	}
}

func (self *Input) startSearch() {
	if self.history == nil {
		return
	}

	self.search = self.history.search(self.buffer.String())
}

func (self *Input) finishSearch() {
	if self.search == nil {
		return
	}

	self.recall = self.search.recall()
	self.search = nil
}

const (
	acceptCoolOff   = 250 * time.Millisecond
	continueWindow  = 250 * time.Millisecond
	continueCoolOff = time.Second
)

func (self *Input) enter(keypress key.Key) Action {
	if keypress.Mod.Has(key.Shift) {
		self.buffer.Insert([]rune{'\n'})
		return DrawInput
	}

	if strings.TrimSpace(self.buffer.String()) != "" {
		now := self.currentTime()
		isCoolingOff := now.Before(self.acceptAfter)
		self.acceptAfter = now.Add(acceptCoolOff)

		switch {
		case isCoolingOff:
			return DrawInput
		case keypress.Mod.Has(key.Alt):
			return ForceAcceptInput
		default:
			return AcceptInput
		}
	}

	now := self.currentTime()
	if now.Before(self.continueAfter) {
		self.continueAfter = now.Add(continueCoolOff)
		return DrawInput
	}

	if self.isEnterPending && !now.After(self.continueBefore) {
		self.isEnterPending = false
		self.continueAfter = now.Add(continueCoolOff)
		return ContinueTurn
	}

	self.isEnterPending = true
	self.continueBefore = now.Add(continueWindow)
	return DrawInput
}

const tabStop = 4

func (self *Input) insert(value rune) {
	if value == '\t' {
		self.buffer.Insert([]rune(strings.Repeat(" ", tabStop)))
		return
	}

	self.buffer.Insert([]rune{value})
}

func (self *Input) rune(keypress key.Key, isRunning bool) Action {
	if !keypress.Mod.Has(key.Ctrl) {
		self.insert(keypress.Value)
		return DrawInput
	}

	switch keypress.Value {
	case 'd':
		if isRunning {
			return CancelTurn
		}

		if self.buffer.Len() == 0 {
			return QuitSession
		}

	case 'g':
		return EditDraft

	case 'j':
		self.insert('\n')

	case 'x':
		self.isPrefixPending = true
	}

	return DrawInput
}

func (self *Input) toggleMode(button key.Key) Action {
	self.isPrefixPending = false
	self.toolGroup = ""

	if button.Code != key.Rune || button.Mod != 0 {
		return DrawInput
	}

	switch button.Value {
	case 'w':
		return ToggleWrite

	case 'x':
		return ToggleShell

	case 'n':
		return ToggleNetwork

	case 'g':
		return ToggleGit

	case 'l':
		return ToggleLookup
	}

	self.toolGroup = string(button.Value)
	return ToggleToolGroup
}

func (self *Input) paste(keypress key.Key) {
	switch {
	case keypress.Code == key.PasteEnd:
		self.isPasting = false
		self.normalisePastedText()
	case keypress.Code == key.Enter || keypress.Code == key.Rune && keypress.Value == 'j' && keypress.Mod.Has(key.Ctrl):
		self.buffer.Insert([]rune{'\n'})
	case keypress.Code == key.Rune && keypress.Mod == 0 && isPastable(keypress.Value):
		self.insert(keypress.Value)
	}
}

func (self *Input) normalisePastedText() {
	end := self.buffer.Cursor()
	runes := self.buffer.Runes()
	pastedText := string(runes[self.pasteStart:end])
	isAtLineEnd := end == len(runes) || runes[end] == '\n'

	normalisedText := preparePastedText(pastedText, string(runes[:self.pasteStart]), isAtLineEnd)
	if normalisedText == pastedText {
		return
	}

	self.buffer.remove(self.pasteStart, end)
	self.buffer.Insert([]rune(normalisedText))
}

func normaliseIndentation(text string) string {
	lines := strings.Split(text, "\n")
	indentation := len(text)

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		indentation = min(indentation, len(line)-len(strings.TrimLeft(line, " ")))
	}

	if indentation == 0 || indentation == len(text) {
		return text
	}

	for i, line := range lines {
		leadingSpaces := len(line) - len(strings.TrimLeft(line, " "))
		lines[i] = line[min(indentation, leadingSpaces):]
	}

	return strings.Join(lines, "\n")
}

func (self *Input) walk(direction int) {
	if self.recall == nil {
		return
	}

	if line, found := self.recall.Walk(self.buffer.String(), direction); found {
		self.buffer.Set(line)
	}
}
