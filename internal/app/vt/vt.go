package vt

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"crdx.org/oh/internal/app/width"
)

type Screen struct {
	err             error
	rows            [][]Cell
	marks           map[int]bool
	wraps           map[int]bool
	sizedCharacters []sizedCharacter
	row, column     int
	columns         int
	height          int
	lowestRow       int

	wasDrawnAboveTheScreen bool
	wasScreenErasedWhole   bool
	isWrapping             bool
	style                  graphicStyle

	main *screenContents
}

type screenContents struct {
	rows            [][]Cell
	marks           map[int]bool
	wraps           map[int]bool
	sizedCharacters []sizedCharacter
	row, column     int
	lowestRow       int
}

type Cell struct {
	Grapheme string
	Styles   string
}

type sizedCharacter struct {
	row, column int
	cells, rows int
}

type graphicStyle struct {
	foreground  string
	background  string
	underline   string
	decorations []string
}

const picturePlaceholder = "\U0010EEEE"

var decorationsOff = map[string][]string{
	"21": {"1"},
	"22": {"1", "2"},
	"23": {"3"},
	"24": {"4"},
	"25": {"5", "6"},
	"27": {"7"},
	"28": {"8"},
	"29": {"9"},
	"55": {"53"},
}

const messageMarkPayload = "133;A"

const regionCommand = "7700"

const RegionEnd = "\x1b]" + regionCommand + ";\a"

func New(columns int, lines int) *Screen {
	return &Screen{columns: columns, height: lines, isWrapping: true}
}

func (self *Screen) Play(stream string) error {
	self.play(stream)
	return self.err
}

func (self *Screen) TopRow() int {
	if self.height == 0 {
		return 0
	}

	return max(0, self.lowestRow-self.height+1)
}

func (self *Screen) OnScreen() []string {
	text := self.Text()
	rows := make([]string, self.height)
	for i := range rows {
		if at := self.TopRow() + i; at < len(text) {
			rows[i] = text[at]
		}
	}

	return rows
}

func (self *Screen) Text() []string {
	return self.lines(func(row []Cell) string {
		var line strings.Builder
		for _, one := range row {
			line.WriteString(one.Grapheme)
		}

		return line.String()
	})
}

func (self *Screen) Styled() []string {
	return self.lines(func(row []Cell) string {
		var line strings.Builder
		styles := ""
		for _, one := range row {
			if one.Styles != styles {
				if styles != "" {
					line.WriteString("\x1b[0m")
				}
				line.WriteString(one.Styles)
				styles = one.Styles
			}
			line.WriteString(one.Grapheme)
		}
		if styles != "" {
			line.WriteString("\x1b[0m")
		}

		return line.String()
	})
}

func (self *Screen) Marked() []int {
	return slices.Sorted(maps.Keys(self.marks))
}

func (self *Screen) SoftWrapped() []int {
	return slices.Sorted(maps.Keys(self.wraps))
}

func (self *Screen) Copied() []string {
	var lines []string

	for at, row := range self.rows {
		var line strings.Builder
		for _, one := range row {
			line.WriteString(one.Grapheme)
		}

		text := line.String()
		if !self.wraps[at] {
			text = strings.TrimRight(text, " ")
		}

		if at > 0 && self.wraps[at-1] {
			lines[len(lines)-1] += text
			continue
		}

		lines = append(lines, text)
	}

	return lines
}

func (self *Screen) BackgroundAt(row int) string {
	if row >= len(self.rows) || len(self.rows[row]) == 0 {
		return ""
	}

	return self.rows[row][0].Styles
}

func (self *Screen) Rows() [][]Cell {
	return self.rows
}

func (self *Screen) WasDrawnAboveTheScreen() bool {
	return self.wasDrawnAboveTheScreen
}

func (self *Screen) WasScreenErasedWhole() bool {
	return self.wasScreenErasedWhole
}

func (self *Screen) CursorRow() int {
	return self.row
}

func (self *Screen) CursorColumn() int {
	return self.column
}

func (self *Screen) StyledOnScreen() []string {
	rows := self.Styled()
	top := self.TopRow()
	if top >= len(rows) {
		return nil
	}

	return rows[top:]
}

func (self *Screen) enterAlternateScreen() {
	if self.main != nil {
		return
	}

	self.main = &screenContents{
		rows: self.rows, marks: self.marks, wraps: self.wraps, sizedCharacters: self.sizedCharacters,
		row: self.row, column: self.column, lowestRow: self.lowestRow,
	}
	self.rows, self.marks, self.wraps, self.sizedCharacters = nil, nil, nil, nil
	self.row, self.column, self.lowestRow = 0, 0, 0
}

func (self *Screen) leaveAlternateScreen() {
	if self.main == nil {
		return
	}

	self.rows, self.marks, self.wraps, self.sizedCharacters = self.main.rows, self.main.marks, self.main.wraps, self.main.sizedCharacters
	self.row, self.column, self.lowestRow = self.main.row, self.main.column, self.main.lowestRow
	self.main = nil
}

func (self *Screen) failf(format string, arguments ...any) {
	if self.err == nil {
		self.err = fmt.Errorf(format, arguments...)
	}
}

func (self *Screen) moveDown(rows int) {
	self.row += rows
	self.lowestRow = max(self.lowestRow, self.row)
}

func (self *Screen) play(stream string) {
	for at := 0; at < len(stream) && self.err == nil; {
		switch stream[at] {
		case '\x1b':
			at = self.escape(stream, at)
		case '\r':
			self.column = 0
			at++
		case '\n':
			self.moveDown(1)
			self.column = 0
			at++
		default:
			end := at + 1
			for end < len(stream) && stream[end] != '\x1b' && stream[end] != '\r' && stream[end] != '\n' {
				end++
			}

			for grapheme, cells := range width.Graphemes(stream[at:end]) {
				self.put(grapheme, cells)
			}
			at = end
		}
	}
}

func (self *Screen) escape(stream string, at int) int {
	if at+1 >= len(stream) {
		return len(stream)
	}

	switch stream[at+1] {
	case '[':
		return self.control(stream, at)
	case ']':
		return self.operatingSystemCommand(stream, at)
	case '_':
		return self.applicationCommand(stream, at)
	default:
		self.failf("the screen was sent an escape it does not know: %q", stream[at:min(at+8, len(stream))])
		return len(stream)
	}
}

func (self *Screen) applicationCommand(stream string, at int) int {
	return skipUntilStringTerminator(stream, at)
}

func (self *Screen) operatingSystemCommand(stream string, at int) int {
	end := skipUntilStringTerminator(stream, at)
	terminatorWidth := 1
	if end >= 2 && stream[end-2:end] == "\x1b\\" {
		terminatorWidth = 2
	}
	payload := stream[at+2 : end-terminatorWidth]
	if strings.HasPrefix(payload, "133;") {
		self.semanticPrompt(payload)
		return end
	}

	if !strings.HasPrefix(payload, "66;") {
		return end
	}

	parts := strings.SplitN(payload, ";", 3)
	if len(parts) != 3 {
		return end
	}

	scale := 1
	declaredWidth := width.Of(parts[2])
	for option := range strings.SplitSeq(parts[1], ":") {
		scale = prefixedNumber(option, "s=", scale)
		declaredWidth = prefixedNumber(option, "w=", declaredWidth)
	}

	self.putSized(parts[2], scale*declaredWidth, scale)

	return end
}

func (self *Screen) semanticPrompt(payload string) {
	if payload != messageMarkPayload {
		self.failf("the screen was sent a semantic prompt it does not know: OSC %s", payload)
		return
	}

	if self.marks == nil {
		self.marks = map[int]bool{}
	}

	self.marks[self.row] = true
}

func (self *Screen) putSized(grapheme string, cells int, rows int) {
	drawnCells := min(width.Of(grapheme), cells)
	self.put(grapheme, drawnCells)
	character := sizedCharacter{row: self.row, column: self.column - drawnCells, cells: cells, rows: rows}

	for range cells - drawnCells {
		self.put(" ", 1)
	}

	if rows > 1 {
		self.sizedCharacters = append(self.sizedCharacters, character)
	}
}

func (self *Screen) pastLowerHalves(column int) int {
	for _, one := range self.sizedCharacters {
		if one.row < self.row && self.row < one.row+one.rows && one.covers(self.row, self.row, column, column) {
			return self.pastLowerHalves(one.column + one.cells)
		}
	}

	return column
}

func (self *Screen) destroySized(firstRow int, lastRow int, firstColumn int, lastColumn int) {
	self.sizedCharacters = slices.DeleteFunc(self.sizedCharacters, func(one sizedCharacter) bool {
		if !one.covers(firstRow, lastRow, firstColumn, lastColumn) {
			return false
		}

		if one.row < len(self.rows) {
			row := self.rows[one.row]
			for at := one.column; at < min(one.column+one.cells, len(row)); at++ {
				row[at] = Cell{Grapheme: " "}
			}
		}

		return true
	})
}

func (self *Screen) control(stream string, at int) int {
	end := at + 2
	for end < len(stream) && (stream[end] < 0x40 || stream[end] > 0x7e) {
		end++
	}

	if end >= len(stream) {
		return len(stream)
	}

	parameters := stream[at+2 : end]
	self.apply(stream[end], parameters)

	return end + 1
}

func (self *Screen) apply(command byte, parameters string) {
	if strings.HasPrefix(parameters, "?") {
		self.privateMode(command, parameters)
		return
	}

	count := max(1, numberOr(parameters, 1))

	switch command {
	case 'm':
		self.restyle(parameters)
	case 'A':
		if self.row-count < self.TopRow() {
			self.wasDrawnAboveTheScreen = true
		}
		self.row = max(self.TopRow(), self.row-count)
	case 'B':
		self.row += count
		if self.height > 0 {
			self.row = min(self.row, self.TopRow()+self.height-1)
		}
	case 'C':
		self.column += count
	case 'D':
		self.column = max(0, self.column-count)
	case 'H':
		row, column, _ := strings.Cut(parameters, ";")
		self.row = self.TopRow() + max(1, numberOr(row, 1)) - 1
		self.column = max(1, numberOr(column, 1)) - 1
		self.lowestRow = max(self.lowestRow, self.row)
	case 'c', 'n', 'q', 't', 'u':
	case 'K':
		self.eraseInRow(numberOr(parameters, 0))
	case 'J':
		self.erase(numberOr(parameters, 0))
	default:
		self.failf("the screen was sent a control it does not know: ESC [ %s%c", parameters, command)
	}
}

func (self *Screen) privateMode(command byte, parameters string) {
	if command == 'u' || command == 'n' || command == 'c' {
		return
	}
	if command != 'h' && command != 'l' {
		self.failf("the screen was sent a private mode it does not know: ESC [ %s%c", parameters, command)
	}

	switch parameters {
	case "?7":
		self.isWrapping = command == 'h'
	case "?1049":
		if command == 'h' {
			self.enterAlternateScreen()
		} else {
			self.leaveAlternateScreen()
		}
	case "?25", "?2026", "?1004", "?2004", "?5522", "?2031":
	default:
		self.failf("the screen was sent a private mode it does not know: ESC [ %s%c", parameters, command)
	}
}

func (self *Screen) eraseInRow(mode int) {
	switch mode {
	case 0:
		self.destroySized(self.row, self.row, self.column, math.MaxInt)
	case 1:
		self.destroySized(self.row, self.row, 0, self.column)
	case 2:
		self.destroySized(self.row, self.row, 0, math.MaxInt)
	}

	if self.row >= len(self.rows) {
		return
	}

	row := self.rows[self.row]

	switch mode {
	case 0:
		if self.column < len(row) {
			self.rows[self.row] = row[:self.column]
		}
		if self.column < self.columns {
			delete(self.wraps, self.row)
		}
	case 1:
		for at := range min(self.column+1, len(row)) {
			row[at] = Cell{Grapheme: " "}
		}
	case 2:
		self.rows[self.row] = nil
		delete(self.wraps, self.row)
	}
}

func (self *Screen) erase(mode int) {
	switch mode {
	case 0:
		if self.height > 0 && self.row == self.TopRow() && self.column == 0 {
			self.wasScreenErasedWhole = true
		}

		self.eraseInRow(0)

		if self.row+1 < len(self.rows) {
			self.rows = self.rows[:self.row+1]
		}

		maps.DeleteFunc(self.marks, func(row int, _ bool) bool { return row > self.row })
		maps.DeleteFunc(self.wraps, func(row int, _ bool) bool { return row > self.row })
		self.destroySized(self.row+1, math.MaxInt, 0, math.MaxInt)
	case 2, 3:
		if self.height > 0 {
			self.eraseScreenOrScrollback(mode)
			return
		}

		self.rows = nil
		self.marks = nil
		self.wraps = nil
		self.sizedCharacters = nil
	default:
		self.failf("the screen was asked to erase in a way it does not know: ESC [ %dJ", mode)
	}
}

func (self *Screen) eraseScreenOrScrollback(mode int) {
	top := self.TopRow()

	if mode == 2 {
		for at := top; at < len(self.rows); at++ {
			self.rows[at] = nil
		}
		maps.DeleteFunc(self.marks, func(row int, _ bool) bool { return row >= top })
		maps.DeleteFunc(self.wraps, func(row int, _ bool) bool { return row >= top })
		self.destroySized(top, math.MaxInt, 0, math.MaxInt)

		return
	}

	self.rows = self.rows[min(top, len(self.rows)):]
	self.marks = rowsFrom(self.marks, top)
	self.wraps = rowsFrom(self.wraps, top)
	self.sizedCharacters = slices.DeleteFunc(self.sizedCharacters, func(one sizedCharacter) bool { return one.row < top })
	for at := range self.sizedCharacters {
		self.sizedCharacters[at].row -= top
	}
	self.row -= top
	self.lowestRow -= top
}

func (self *Screen) put(grapheme string, cells int) {
	if cells == 0 {
		for len(self.rows) <= self.row {
			self.rows = append(self.rows, nil)
		}
		if self.column > 0 {
			self.rows[self.row][self.column-1].Grapheme += grapheme
		}
		return
	}

	if self.column+cells > self.columns && self.column > 0 {
		if !self.isWrapping {
			return
		}

		if self.wraps == nil {
			self.wraps = map[int]bool{}
		}

		self.wraps[self.row] = true
		self.moveDown(1)
		self.column = 0
	}

	for len(self.rows) <= self.row {
		self.rows = append(self.rows, nil)
	}

	self.column = self.pastLowerHalves(self.column)
	drawnCells := min(cells, max(0, self.columns-self.column))
	if drawnCells == 0 {
		return
	}
	self.destroySized(self.row, self.row, self.column, self.column+drawnCells-1)
	row := self.rows[self.row]
	for len(row) < self.column+drawnCells {
		row = append(row, Cell{Grapheme: " "})
	}

	styles := self.style.String()
	if strings.HasPrefix(grapheme, picturePlaceholder) {
		styles = ""
	}

	row[self.column] = Cell{Grapheme: grapheme, Styles: styles}
	for i := 1; i < drawnCells; i++ {
		row[self.column+i] = Cell{Styles: styles}
	}
	self.rows[self.row] = row
	if self.column+drawnCells == self.columns {
		delete(self.wraps, self.row)
	}
	self.column = min(self.column+cells, self.columns)
}

func (self *Screen) restyle(parameters string) {
	tokens := strings.Split(parameters, ";")

	for at := 0; at < len(tokens); at++ {
		switch token := tokens[at]; {
		case token == "" || token == "0":
			self.style = graphicStyle{}
		case token == "38" || token == "48" || token == "58":
			colour, next := self.readColour(tokens, at)
			switch token {
			case "38":
				self.style.foreground = colour
			case "48":
				self.style.background = colour
			default:
				self.style.underline = colour
			}
			at = next
		case token == "39":
			self.style.foreground = ""
		case token == "49":
			self.style.background = ""
		case token == "59":
			self.style.underline = ""
		case decorationsOff[token] != nil:
			self.style.decorations = slices.DeleteFunc(self.style.decorations, func(one string) bool {
				name, _, _ := strings.Cut(one, ":")
				return slices.Contains(decorationsOff[token], name)
			})
		case isDecoration(token):
			if !slices.Contains(self.style.decorations, token) {
				self.style.decorations = append(self.style.decorations, token)
			}
		default:
			self.failf("the screen was sent a graphic rendition it does not know: ESC [ %sm", parameters)
		}
	}
}

func (self *Screen) readColour(tokens []string, at int) (string, int) {
	tokenCount := 0
	if at+1 < len(tokens) {
		switch tokens[at+1] {
		case "2":
			tokenCount = 4
		case "5":
			tokenCount = 2
		}
	}

	if tokenCount == 0 || at+tokenCount >= len(tokens) {
		self.failf("the screen was sent a colour it does not know: %q", strings.Join(tokens[at:], ";"))
		return "", len(tokens)
	}

	return strings.Join(tokens[at:at+tokenCount+1], ";"), at + tokenCount
}

func (self *Screen) lines(draw func(row []Cell) string) []string {
	lines := make([]string, 0, len(self.rows))

	for _, row := range self.rows {
		lines = append(lines, strings.TrimRight(draw(row), " "))
	}

	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}

func (self sizedCharacter) covers(firstRow int, lastRow int, firstColumn int, lastColumn int) bool {
	return self.row <= lastRow && firstRow < self.row+self.rows &&
		self.column <= lastColumn && firstColumn < self.column+self.cells
}

func (self graphicStyle) String() string {
	parts := slices.Clone(self.decorations)
	slices.Sort(parts)

	for _, colour := range []string{self.foreground, self.background, self.underline} {
		if colour != "" {
			parts = append(parts, colour)
		}
	}

	if len(parts) == 0 {
		return ""
	}

	return "\x1b[" + strings.Join(parts, ";") + "m"
}

func rowsFrom(rows map[int]bool, top int) map[int]bool {
	shiftedRows := map[int]bool{}
	for row := range rows {
		if row >= top {
			shiftedRows[row-top] = true
		}
	}

	return shiftedRows
}

func isDecoration(token string) bool {
	name, _, _ := strings.Cut(token, ":")
	number, err := strconv.Atoi(name)

	return err == nil && (number >= 1 && number <= 9 || number == 53)
}

func skipUntilStringTerminator(stream string, at int) int {
	for end := at + 2; end < len(stream); end++ {
		switch {
		case stream[end] == '\a':
			return end + 1
		case stream[end] == '\x1b' && end+1 < len(stream) && stream[end+1] == '\\':
			return end + 2
		}
	}

	return len(stream)
}

func prefixedNumber(parameter string, prefix string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimPrefix(parameter, prefix))
	if err != nil || !strings.HasPrefix(parameter, prefix) {
		return fallback
	}

	return value
}

func numberOr(parameters string, fallback int) int {
	if parameters == "" {
		return fallback
	}

	value, err := strconv.Atoi(strings.Split(parameters, ";")[0])
	if err != nil {
		return fallback
	}

	return value
}

func RegionStart(name string) string {
	return "\x1b]" + regionCommand + ";region=" + name + "\a"
}

func WithRegions(rows []string, owners []int, name func(int) string) string {
	ends := make([]bool, len(rows))
	last := map[int]int{}
	for row, owner := range owners {
		if owner != 0 {
			last[owner] = row
		}
	}
	for _, row := range last {
		ends[row] = true
	}

	var drawing strings.Builder
	open := 0
	for row, text := range rows {
		owner := 0
		if row < len(owners) {
			owner = owners[row]
		}
		if owner != 0 && owner != open {
			if open != 0 {
				drawing.WriteString(RegionEnd)
			}
			drawing.WriteString(RegionStart(name(owner)))
			open = owner
		}
		drawing.WriteString(text)
		if open != 0 && ends[row] && last[open] == row {
			drawing.WriteString(RegionEnd)
			open = 0
		}
		drawing.WriteString("\n")
	}

	return drawing.String()
}
