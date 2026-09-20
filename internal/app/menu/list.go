package menu

import (
	"errors"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/term"

	"crdx.org/oh/internal/app/ansi"
	"crdx.org/oh/internal/app/interaction"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/spinner"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/tty"
	"crdx.org/oh/internal/app/width"
	"crdx.org/oh/internal/util/strutil"
)

const (
	enterScreen = ansi.EnterAltScreen + hideCursor
	leaveScreen = showCursor + ansi.LeaveAltScreen
	homeCursor  = ansi.Home
	eraseLine   = ansi.EraseLine
	eraseBelow  = ansi.EraseBelow
)

const EffortColumn = 8

const (
	minimumGap     = 2
	defaultColumns = 80
	defaultRows    = 24
)

const filterPrompt = "Filter: "

var ErrCancelled = errors.New("cancelled")

type List interface {
	Len() int
	IsChoosable(index int) bool
	Text(index int) string
	ColumnHeader(room int) string
	Row(index int, isChosen bool, room int) string
	Adjust(index int, direction int)
}

type Switchable interface {
	Switch(direction int) bool
}

type Reachable interface {
	IsReachable(index int) bool
}

type Removable interface {
	Removal(index int, keypress key.Key) (Removal, bool)
}

type Removal struct {
	Prompt   string
	Progress string
	Perform  func() error
	Apply    func()
}

type Previewable interface {
	Preview(index int, keypress key.Key) (Preview, bool)
}

type Preview struct {
	Title string
	Read  func(room int) ([]string, error)
}

func Choose(rows List, terminal *os.File, screen io.Writer) (int, error) {
	if rows.Len() == 0 {
		return 0, ErrCancelled
	}

	restore, err := tty.Raw(terminal, screen)
	if err != nil {
		return 0, err
	}

	defer restore()

	keys, stopReading := interaction.Keypresses(terminal)
	defer stopReading()

	return choose(rows, keys, measuring(terminal), screen, nil)
}

func choose(
	rows List,
	keys <-chan key.Key,
	measure func() (int, int),
	screen io.Writer,
	startWork func(func()),
) (int, error) {
	write(screen, enterScreen)
	defer write(screen, leaveScreen)

	self := newState(rows)
	self.keys = keys
	self.measure = measure
	self.screen = screen
	self.startWork = startWork

	return self.run()
}

func newState(rows List) *state {
	self := &state{list: rows, removalState: removalState{index: -1}}
	self.refilter()

	return self
}

func measuring(terminal *os.File) func() (int, int) {
	return func() (int, int) {
		room, height, err := term.GetSize(int(terminal.Fd()))
		if err != nil {
			return defaultColumns, defaultRows
		}

		return room, height
	}
}

type state struct {
	list    List
	keys    <-chan key.Key
	measure func() (int, int)
	screen  io.Writer

	startWork func(func())

	query   string
	matches []int
	cursor  int
	offset  int
	window  int

	removalState removalState
	preview      previewState
}

type removalState struct {
	index     int
	keypress  key.Key
	work      Removal
	failure   string
	isWorking bool
	spinnerAt int
	done      chan error
}

func (self *state) run() (int, error) {
	resizeSignals := interaction.Resizes()
	defer signal.Stop(resizeSignals)

	return self.pick(resizeSignals)
}

func (self *state) pick(resizeSignals <-chan os.Signal) (int, error) {
	for {
		self.draw()

		select {
		case err := <-self.removalState.done:
			self.finishRemoval(err)
		case <-self.spinnerTicks():
			self.removalState.spinnerAt++
		case keypress, isOpen := <-self.keys:
			if !isOpen {
				return 0, ErrCancelled
			}

			switch self.apply(keypress) {
			case rowChosen:
				return self.chosen(), nil
			case choiceCancelled:
				return 0, ErrCancelled
			case continuePicking:
			}
		case <-resizeSignals:
			interaction.Settle(resizeSignals)
		}
	}
}

type action int

const (
	continuePicking action = iota
	rowChosen
	choiceCancelled
)

func (self *state) apply(keypress key.Key) action {
	if self.removalState.isWorking {
		return continuePicking
	}

	if self.preview.isOpen {
		return self.readPreview(keypress)
	}

	if self.removalState.index >= 0 {
		self.answerRemoval(keypress)
		return continuePicking
	}

	self.removalState.failure = ""

	if self.askToRemove(keypress) {
		return continuePicking
	}

	if self.askToPreview(keypress) {
		return continuePicking
	}

	switch keypress.Code {
	case key.Up:
		self.move(-1)
	case key.Down:
		self.move(1)
	case key.Left:
		self.adjust(-1)
	case key.Right:
		self.adjust(1)
	case key.PageUp:
		self.page(-1)
	case key.PageDown:
		self.page(1)
	case key.Home:
		self.cursor = self.firstSelectable()
	case key.End:
		self.cursor = self.lastSelectable()
	case key.Enter:
		if self.cursor >= 0 && self.list.IsChoosable(self.chosen()) {
			return rowChosen
		}
	case key.Escape:
		return choiceCancelled
	case key.Backspace:
		self.narrow(trimLastRune(self.query))
	case key.Rune:
		if keypress.Value == 'c' && keypress.Mod.Has(key.Ctrl) {
			return choiceCancelled
		}
		if isTypeable(keypress) {
			self.narrow(self.query + string(keypress.Value))
		}
	case key.Delete, key.PasteStart, key.PasteEnd, key.Clipboard, key.FocusIn, key.FocusOut, key.Unknown:
	}

	return continuePicking
}

func (self *state) spinnerTicks() <-chan time.Time {
	if !self.removalState.isWorking {
		return nil
	}

	return time.After(spinner.Activity.RefreshInterval())
}

func (self *state) start(work func()) {
	if self.startWork != nil {
		self.startWork(work)
		return
	}

	go work()
}

func (self *state) removableList() (Removable, bool) {
	rows, isRemovable := self.list.(Removable)
	return rows, isRemovable
}

func (self *state) askToRemove(keypress key.Key) bool {
	rows, isRemovable := self.removableList()
	if !isRemovable || self.cursor < 0 {
		return false
	}

	index := self.chosen()
	work, isBound := rows.Removal(index, keypress)
	if !isBound {
		return false
	}

	self.removalState = removalState{index: index, keypress: keypress, work: work}

	return true
}

func (self *state) answerRemoval(keypress key.Key) {
	confirmation, work := self.removalState.keypress, self.removalState.work
	self.removalState = removalState{index: -1}

	if keypress != confirmation {
		return
	}

	self.beginRemoval(work)
}

func (self *state) beginRemoval(work Removal) {
	self.removalState = removalState{index: -1, work: work, isWorking: true, done: make(chan error, 1)}
	self.draw()

	self.start(func() { self.removalState.done <- work.Perform() })

	select {
	case err := <-self.removalState.done:
		self.finishRemoval(err)
	default:
	}
}

func (self *state) finishRemoval(err error) {
	work := self.removalState.work
	self.removalState = removalState{index: -1}

	if err != nil {
		self.removalState.failure = err.Error()
		return
	}

	work.Apply()

	at, offset := self.cursor, self.offset
	self.refilter()
	self.cursor = self.selectableFrom(at)
	self.offset = min(offset, max(len(self.matches)-self.window, 0))
}

func (self *state) selectableFrom(at int) int {
	for index := max(at, 0); index < len(self.matches); index++ {
		if self.isReachable(self.matches[index]) {
			return index
		}
	}

	for index := min(at, len(self.matches)) - 1; index >= 0; index-- {
		if self.isReachable(self.matches[index]) {
			return index
		}
	}

	return -1
}

func isTypeable(keypress key.Key) bool {
	return keypress.Value >= ' ' && keypress.Value != 0x7f && !keypress.Mod.Has(key.Ctrl) &&
		!keypress.Mod.Has(key.Alt)
}

func trimLastRune(text string) string {
	if text == "" {
		return ""
	}

	_, size := utf8.DecodeLastRuneInString(text)

	return text[:len(text)-size]
}

func (self *state) chosen() int {
	return self.matches[self.cursor]
}

func (self *state) narrow(query string) {
	if query == self.query {
		return
	}

	wantedIndex := -1
	if self.cursor >= 0 {
		wantedIndex = self.chosen()
	}

	self.query = query
	self.refilter()

	if at := slices.Index(self.matches, wantedIndex); at >= 0 && self.isReachable(wantedIndex) {
		self.cursor = at
	}
}

func (self *state) refilter() {
	self.matches = self.matches[:0]

	for index := range self.list.Len() {
		if strutil.MatchesQuery(self.list.Text(index), self.query) {
			self.matches = append(self.matches, index)
		}
	}

	self.cursor = self.firstSelectable()
	self.offset = 0
}

func (self *state) move(direction int) {
	if self.cursor < 0 {
		return
	}

	for at := self.cursor + direction; at >= 0 && at < len(self.matches); at += direction {
		if self.isReachable(self.matches[at]) {
			self.cursor = at
			return
		}
	}
}

func (self *state) page(direction int) {
	for range max(self.window, 1) {
		self.move(direction)
	}
}

func (self *state) adjust(direction int) {
	if rows, isSwitchable := self.list.(Switchable); isSwitchable {
		if rows.Switch(direction) {
			self.refilter()
			return
		}
	}

	if self.cursor >= 0 {
		self.list.Adjust(self.chosen(), direction)
	}
}

func (self *state) firstSelectable() int {
	for at, index := range self.matches {
		if self.isReachable(index) {
			return at
		}
	}

	return -1
}

func (self *state) lastSelectable() int {
	for at, index := range slices.Backward(self.matches) {
		if self.isReachable(index) {
			return at
		}
	}

	return -1
}

func (self *state) isReachable(index int) bool {
	if rows, isReachable := self.list.(Reachable); isReachable {
		return rows.IsReachable(index)
	}

	return self.list.IsChoosable(index)
}

func Paint(rows List, room int, height int, cursor int, query string) string {
	return paint(rows, room, height, cursor, query, nil)
}

func PaintAfterKey(rows List, room int, height int, cursor int, query string, keypress key.Key) string {
	return paint(rows, room, height, cursor, query, &keypress)
}

func paint(rows List, room int, height int, cursor int, query string, keypress *key.Key) string {
	var screen strings.Builder

	self := newState(rows)
	self.narrow(query)
	self.cursor = cursor
	self.screen = &screen
	self.measure = func() (int, int) { return room, height }
	if keypress != nil && !self.askToRemove(*keypress) {
		self.askToPreview(*keypress)
	}

	self.draw()

	return screen.String()
}

func (self *state) draw() {
	room, height := self.size()

	if self.preview.isOpen {
		write(self.screen, self.drawPreview(room, height))
		return
	}

	rows := max(height-2, 1)
	self.window = rows

	self.scroll(rows)

	var output strings.Builder

	output.WriteString(homeCursor)
	output.WriteString(self.promptLine(room))
	output.WriteString(eraseLine + "\r\n")
	output.WriteString(style.Column(Clip(self.list.ColumnHeader(room), room)))
	output.WriteString(eraseLine + "\r\n")

	for at := self.offset; at < self.offset+rows; at++ {
		if at < len(self.matches) {
			output.WriteString(self.list.Row(self.matches[at], at == self.cursor, room))
		}
		output.WriteString(eraseLine)
		if at+1 < self.offset+rows {
			output.WriteString("\r\n")
		}
	}

	output.WriteString(eraseBelow)

	write(self.screen, output.String())
}

func (self *state) scroll(rows int) {
	if self.cursor < 0 {
		return
	}

	switch {
	case self.cursor < self.offset:
		self.offset = self.cursor
	case self.cursor >= self.offset+rows:
		self.offset = self.cursor - rows + 1
	}
}

func (self *state) promptLine(room int) string {
	if self.removalState.isWorking {
		frame := spinner.Activity.Frame(self.removalState.spinnerAt)
		return style.Change(Clip(frame+" "+self.removalState.work.Progress, room))
	}
	if self.removalState.index >= 0 {
		return style.Change(Clip(self.removalState.work.Prompt, room))
	}
	if self.removalState.failure != "" {
		return style.Failure(Clip(self.removalState.failure, room))
	}

	return self.filterLine(room)
}

func (self *state) filterLine(room int) string {
	return style.Subtle(Clip(filterPrompt, room)) + style.Answer(Clip(self.query, room-len(filterPrompt)))
}

func Mark(isChosen bool) string {
	if isChosen {
		return "›"
	}

	return " "
}

func Clip(text string, room int) string {
	return width.Elide(text, room)
}

func (self *state) size() (int, int) {
	if self.measure == nil {
		return defaultColumns, defaultRows
	}

	return self.measure()
}

func write(screen io.Writer, text string) {
	if screen == nil {
		return
	}

	_, _ = io.WriteString(screen, text)
}
