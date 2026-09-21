package painter

import (
	"strings"

	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/width"
)

type paragraphReflow struct {
	columns     int
	settledText string
	settledRows []string
}

func (self *paragraphReflow) Reset() {
	self.settledText = ""
	self.settledRows = nil
}

func (self *paragraphReflow) Wrap(text string, settledLength int, columns int) []string {
	if columns != self.columns || !strings.HasPrefix(text, self.settledText) {
		self.Reset()
		self.columns = columns
	}

	tail := text[len(self.settledText):]
	rows := width.Wrap(style.Reasoning(tail), columns)

	if len(rows) > 1 {
		if advance := self.reach(tail, len(rows)-1, columns); advance > 0 &&
			len(self.settledText)+advance <= settledLength {
			self.settledRows = append(self.settledRows, rows[:len(rows)-1]...)
			self.settledText = text[:len(self.settledText)+advance]
			tail = text[len(self.settledText):]
			rows = width.Wrap(style.Reasoning(tail), columns)
		}
	}

	output := make([]string, 0, len(self.settledRows)+len(rows))
	output = append(output, self.settledRows...)

	return append(output, rows...)
}

func (self *paragraphReflow) reach(tail string, rowCount int, columns int) int {
	rows := width.Rows(tail, columns)
	if len(rows) < rowCount {
		return 0
	}

	return runeOffset(tail, rows[rowCount-1].Next)
}

func runeOffset(text string, runes int) int {
	count := 0
	for at := range text {
		if count == runes {
			return at
		}
		count++
	}

	if count == runes {
		return len(text)
	}

	return -1
}
