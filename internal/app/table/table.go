package table

import (
	"strings"

	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/app/width"
)

const DefaultGap = 2

type Alignment int

const (
	Left Alignment = iota
	Right
)

type Column struct {
	Title   string
	Width   int
	Align   Alignment
	Style   style.Style
	MinRoom int
	IsFlex  bool
}

type Table struct {
	columns []Column
	gap     int
}

func New(columns ...Column) *Table {
	return &Table{columns: columns, gap: DefaultGap}
}

func (self *Table) Gap(cells int) *Table {
	self.gap = cells
	return self
}

func (self *Table) Fit(rows [][]string) *Table {
	for index := range self.columns {
		if self.columns[index].Width > 0 || self.columns[index].IsFlex {
			continue
		}

		widest := width.Of(self.columns[index].Title)
		for _, cells := range rows {
			if index < len(cells) {
				widest = max(widest, width.Of(cells[index]))
			}
		}
		self.columns[index].Width = widest
	}

	return self
}

func (self *Table) Header(room int) string {
	titles := make([]string, len(self.columns))
	for index, column := range self.columns {
		titles[index] = column.Title
	}

	return self.line(titles, room, false)
}

func (self *Table) Row(cells []string, room int) string {
	return self.line(cells, room, true)
}

func (self *Table) line(cells []string, room int, isPainted bool) string {
	shownIndexes := self.shownColumns(room)

	flexAt := -1
	for at, index := range shownIndexes {
		if self.columns[index].IsFlex {
			flexAt = at
		}
	}

	if flexAt < 0 {
		return clip(self.join(shownIndexes, cells, isPainted, -1), room)
	}

	trailingText := self.join(shownIndexes[flexAt+1:], cells, isPainted, -1)
	trailingRoom := width.Of(trailingText)

	leadingRoom := room - trailingRoom - self.gap
	if leadingRoom <= 0 {
		return clip(self.join(shownIndexes[:flexAt+1], cells, isPainted, -1), room)
	}

	leadingText := clip(self.join(shownIndexes[:flexAt+1], cells, isPainted, -1), leadingRoom)
	padding := max(room-width.Of(leadingText)-trailingRoom, 0)

	return strings.TrimRight(leadingText+strings.Repeat(" ", padding)+trailingText, " ")
}

func (self *Table) shownColumns(room int) []int {
	shownIndexes := make([]int, 0, len(self.columns))
	for index, column := range self.columns {
		if room <= 0 || column.MinRoom <= room {
			shownIndexes = append(shownIndexes, index)
		}
	}

	return shownIndexes
}

func (self *Table) join(shownIndexes []int, cells []string, isPainted bool, _ int) string {
	parts := make([]string, 0, len(shownIndexes))
	for at, index := range shownIndexes {
		isLast := at == len(shownIndexes)-1
		parts = append(parts, self.cell(index, cellAt(cells, index), isPainted, isLast))
	}

	return strings.Join(parts, strings.Repeat(" ", self.gap))
}

func (self *Table) cell(index int, text string, isPainted bool, isLast bool) string {
	column := self.columns[index]

	if column.Width > 0 {
		text = clip(text, column.Width)

		switch {
		case column.Align == Right:
			text = strings.Repeat(" ", column.Width-width.Of(text)) + text
		case !isLast:
			text += strings.Repeat(" ", column.Width-width.Of(text))
		}
	}

	if isPainted && column.Style != nil {
		return column.Style.Over(text)
	}

	return text
}

func clip(text string, room int) string {
	if room <= 0 {
		return text
	}

	return width.Elide(text, room)
}

func cellAt(cells []string, index int) string {
	if index < len(cells) {
		return cells[index]
	}

	return ""
}
