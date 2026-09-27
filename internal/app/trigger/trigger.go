package trigger

import (
	"strings"
	"unicode"

	"crdx.org/oh/internal/app/dropdown"
)

const quote = '"'

type Result struct {
	Label       string
	Detail      string
	Text        string
	IsOpenEnded bool
}

type Results struct {
	Items       []Result
	Total       int
	Placeholder string
}

type Word struct {
	Start    int
	End      int
	Query    string
	IsQuoted bool
}

type Source interface {
	Symbol() rune
	Elision() dropdown.Elision
	Find(runes []rune, cursor int) (Word, bool)
	Open(announceChange func())
	Results(word Word, limit int) Results
}

func FindWord(runes []rune, cursor int, symbol rune) (Word, bool) {
	if cursor < 0 || cursor > len(runes) {
		return Word{}, false
	}

	if word, isFound := findQuotedWord(runes, cursor, symbol); isFound {
		return word, true
	}

	start := cursor
	for start > 0 && !unicode.IsSpace(runes[start-1]) {
		start--
	}
	if start == cursor || runes[start] != symbol {
		return Word{}, false
	}

	end := cursor
	for end < len(runes) && !unicode.IsSpace(runes[end]) {
		end++
	}

	return Word{Start: start, End: end, Query: string(runes[start+1 : cursor])}, true
}

func findQuotedWord(runes []rune, cursor int, symbol rune) (Word, bool) {
	openingQuote := cursor - 1
	for openingQuote > 0 && runes[openingQuote] != quote && runes[openingQuote] != '\n' {
		openingQuote--
	}
	if openingQuote <= 0 || runes[openingQuote] != quote || runes[openingQuote-1] != symbol {
		return Word{}, false
	}

	start := openingQuote - 1
	if start > 0 && !unicode.IsSpace(runes[start-1]) {
		return Word{}, false
	}

	end := cursor
	for end < len(runes) && runes[end] != quote && runes[end] != '\n' {
		end++
	}
	if end < len(runes) && runes[end] == quote {
		end++
	} else {
		end = cursor
		for end < len(runes) && !unicode.IsSpace(runes[end]) {
			end++
		}
	}

	return Word{Start: start, End: end, Query: string(runes[openingQuote+1 : cursor]), IsQuoted: true}, true
}

func WordText(symbol rune, text string, isQuoted bool, isOpenEnded bool) string {
	if !isQuoted && !strings.ContainsFunc(text, unicode.IsSpace) {
		return string(symbol) + text
	}

	quotedText := string(symbol) + string(quote) + text
	if isOpenEnded {
		return quotedText
	}

	return quotedText + string(quote)
}

func Replacement(word Word, result Result, runes []rune) string {
	if result.IsOpenEnded || word.End < len(runes) && unicode.IsSpace(runes[word.End]) {
		return result.Text
	}

	return result.Text + " "
}
