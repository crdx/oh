package width

import (
	"iter"
	"strings"
	"unicode/utf8"

	"crdx.org/oh/internal/app/escape"
	"github.com/rivo/uniseg"
)

func Graphemes(text string) iter.Seq2[string, int] {
	return func(yield func(string, int) bool) {
		for one := range graphemes(text) {
			if !yield(one.text, one.cells) {
				return
			}
		}
	}
}

type grapheme struct {
	at    int
	text  string
	cells int
}

func graphemes(text string) iter.Seq[grapheme] {
	return func(yield func(grapheme) bool) {
		state := -1

		for at := 0; at < len(text); {
			if isLoneASCII(text, at) {
				if !yield(grapheme{at: at, text: text[at : at+1], cells: 1}) {
					return
				}

				at++
				state = -1

				continue
			}

			cluster, rest, measuredWidth, newState := uniseg.FirstGraphemeClusterInString(text[at:], state)
			if !yield(grapheme{at: at, text: cluster, cells: graphemeWidth(cluster, measuredWidth)}) {
				return
			}

			at = len(text) - len(rest)
			state = newState
		}
	}
}

func isLoneASCII(text string, at int) bool {
	if text[at] < ' ' || text[at] > '~' {
		return false
	}

	return at+1 == len(text) || text[at+1] < utf8.RuneSelf
}

func Of(text string) int {
	if plain, isPlain := plainWidth(text); isPlain {
		return plain
	}

	cells := 0
	runes := []rune(text)

	for i := 0; i < len(runes); {
		if runes[i] == '\x1b' {
			sequence := escape.GetSequence(runes, i)
			cells += sequence.Cells
			i = sequence.End
			continue
		}

		end := i + 1
		for end < len(runes) && runes[end] != '\x1b' {
			end++
		}
		for _, graphemeCells := range Graphemes(string(runes[i:end])) {
			cells += graphemeCells
		}
		i = end
	}
	return cells
}

func Cut(text string, cells int) (string, int) {
	if plain, isPlain := plainWidth(text); isPlain && plain <= cells {
		return text, plain
	}

	takenCells := 0
	runes := []rune(text)
	var keptText strings.Builder

	for i := 0; i < len(runes); {
		if runes[i] == '\x1b' {
			sequence := escape.GetSequence(runes, i)
			if takenCells+sequence.Cells > cells {
				return keptText.String(), takenCells
			}
			keptText.WriteString(string(runes[i:sequence.End]))
			takenCells += sequence.Cells
			i = sequence.End
			continue
		}

		end := i + 1
		for end < len(runes) && runes[end] != '\x1b' {
			end++
		}

		for one := range graphemes(string(runes[i:end])) {
			if takenCells+one.cells > cells {
				return keptText.String(), takenCells
			}
			keptText.WriteString(one.text)
			takenCells += one.cells
		}
		i = end
	}

	return keptText.String(), takenCells
}

func Cells(text string) []string {
	var cells []string

	for one := range graphemes(text) {
		if one.cells == 0 {
			continue
		}
		cells = append(cells, one.text)
		for range one.cells - 1 {
			cells = append(cells, "")
		}
	}

	return cells
}

func plainWidth(text string) (int, bool) {
	for at := range len(text) {
		if text[at] < ' ' || text[at] > '~' {
			return 0, false
		}
	}

	return len(text), true
}

func graphemeWidth(grapheme string, measuredWidth int) int {
	if strings.ContainsRune(grapheme, '\u20e3') {
		return 2
	}
	return measuredWidth
}
