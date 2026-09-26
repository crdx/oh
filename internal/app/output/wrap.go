package output

import (
	"strings"
	"unicode/utf8"

	"crdx.org/oh/internal/app/ansi"
	"crdx.org/oh/internal/app/escape"
	"crdx.org/oh/internal/app/width"
)

const (
	noAutoWrap = ansi.NoAutoWrap
	autoWrap   = ansi.AutoWrap
)

func (self *Screen) fit(text string) string {
	return self.fitFrom(text, false)
}

func (self *Screen) fitRunningOn(text string) string {
	return self.fitFrom(text, true)
}

func (self *Screen) fitFrom(text string, isRunningOn bool) string {
	if self.columns <= 0 {
		self.openedRows += strings.Count(text, "\n")
		return text
	}

	var out strings.Builder

	last := rune(0)
	runes := []rune(text)

	for i := 0; i < len(runes); {
		switch runes[i] {
		case '\x1b':
			sequence := escape.GetSequence(runes, i)
			if self.column+sequence.Cells > self.columns && self.column > 0 {
				out.WriteString("\r\n")
				self.column = 0
				self.openedRows++
			}
			self.column = min(self.column+sequence.Cells, self.columns)
			out.WriteString(string(runes[i:sequence.End]))
			i = sequence.End

		case '\n':
			if last != '\r' {
				out.WriteRune('\r')
			}

			out.WriteRune(runes[i])
			self.column = 0
			self.openedRows++
			last = runes[i]
			i++

		case '\r':
			out.WriteRune(runes[i])
			self.column = 0
			last = runes[i]
			i++

		default:
			end := i + 1
			for end < len(runes) && runes[end] != '\x1b' && runes[end] != '\n' && runes[end] != '\r' {
				end++
			}

			for grapheme, cells := range width.Graphemes(string(runes[i:end])) {
				isSoftBreak := false

				if self.column+cells > self.columns && self.column > 0 {
					if grapheme == " " && !isRunningOn {
						continue
					}

					isSoftBreak = isRunningOn && self.isTerminal
					switch {
					case !isSoftBreak:
						out.WriteString("\r\n")
					case self.isWrapping:
						out.WriteString(autoWrap)
					}

					self.column = 0
					self.openedRows++
				}

				isRunningOn = false

				self.column = min(self.column+cells, self.columns)
				out.WriteString(grapheme)
				last = lastRune(grapheme)

				if isSoftBreak && self.isWrapping {
					out.WriteString(noAutoWrap)
				}
			}
			i = end
		}
	}

	return out.String()
}

func lastRune(grapheme string) rune {
	value, _ := utf8.DecodeLastRuneInString(grapheme)
	return value
}
