package painter

import (
	"strings"
	"unicode/utf8"

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
	wrappedRows := width.Rows(style.Reasoning(tail), columns)
	rows := rowTexts(wrappedRows)

	if len(rows) > 1 {
		advanceRunes := wrappedRows[len(wrappedRows)-2].Next - reasoningPrefixRunes()
		advance := runeOffset(tail, advanceRunes)
		if advance > 0 && len(self.settledText)+advance <= settledLength {
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

func rowTexts(wrappedRows []width.Row) []string {
	rows := make([]string, len(wrappedRows))
	for index, row := range wrappedRows {
		rows[index] = row.Text
	}

	return rows
}

func reasoningPrefixRunes() int {
	const marker = '\x00'

	markedText := style.Reasoning(string(marker))
	return utf8.RuneCountInString(markedText[:strings.IndexByte(markedText, marker)])
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
