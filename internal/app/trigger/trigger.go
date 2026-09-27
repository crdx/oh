package trigger

import (
	"unicode"

	"crdx.org/oh/internal/app/dropdown"
)

type Result struct {
	Text        string
	IsOpenEnded bool
}

type Results struct {
	Items       []Result
	Total       int
	Placeholder string
}

type Source interface {
	Symbol() rune
	Elision() dropdown.Elision
	Open(announceChange func())
	Results(query string, limit int) Results
}

type Word struct {
	Symbol rune
	Start  int
	End    int
	Query  string
}

func Find(runes []rune, cursor int, isSymbol func(rune) bool) (Word, bool) {
	if cursor < 0 || cursor > len(runes) {
		return Word{}, false
	}

	start := cursor
	for start > 0 && !unicode.IsSpace(runes[start-1]) {
		start--
	}
	if start == cursor || !isSymbol(runes[start]) {
		return Word{}, false
	}

	end := cursor
	for end < len(runes) && !unicode.IsSpace(runes[end]) {
		end++
	}

	return Word{Symbol: runes[start], Start: start, End: end, Query: string(runes[start+1 : cursor])}, true
}

func Replacement(word Word, result Result, runes []rune) string {
	text := string(word.Symbol) + result.Text
	if result.IsOpenEnded || word.End < len(runes) && unicode.IsSpace(runes[word.End]) {
		return text
	}

	return text + " "
}
