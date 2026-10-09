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

	for at := 0; at < len(text); {
		switch text[at] {
		case '\x1b':
			sequence := escape.GetSequence(text, at)
			if self.column+sequence.Cells > self.columns && self.column > 0 {
				out.WriteString("\r\n")
				self.column = 0
				self.openedRows++
			}
			self.column = min(self.column+sequence.Cells, self.columns)
			out.WriteString(text[at:sequence.End])
			at = sequence.End

		case '\n':
			if last != '\r' {
				out.WriteRune('\r')
			}

			out.WriteByte(text[at])
			self.column = 0
			self.openedRows++
			last = rune(text[at])
			at++

		case '\r':
			out.WriteByte(text[at])
			self.column = 0
			last = rune(text[at])
			at++

		default:
			end := at + 1
			for end < len(text) && text[end] != '\x1b' && text[end] != '\n' && text[end] != '\r' {
				end++
			}

			for grapheme, cells := range width.Graphemes(text[at:end]) {
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
			at = end
		}
	}

	return out.String()
}

func lastRune(grapheme string) rune {
	value, _ := utf8.DecodeLastRuneInString(grapheme)
	return value
}
