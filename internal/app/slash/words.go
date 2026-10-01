package slash

import (
	"slices"

	"crdx.org/oh/internal/app/trigger"
)

const wordQuote = '"'

type quotedWord struct {
	start    int
	end      int
	text     string
	isQuoted bool
	isClosed bool
}

func QuotedFields(text string) ([]string, bool) {
	words := scanQuotedWords([]rune(text), 0)
	fields := make([]string, 0, len(words))
	for _, word := range words {
		if word.isQuoted && !word.isClosed {
			return nil, false
		}
		fields = append(fields, word.text)
	}
	return fields, true
}

func scanQuotedWords(runes []rune, start int) []quotedWord {
	var words []quotedWord
	for i := skipSpaces(runes, start); i < len(runes); i = skipSpaces(runes, words[len(words)-1].end) {
		words = append(words, scanQuotedWord(runes, i))
	}
	return words
}

func scanQuotedWord(runes []rune, start int) quotedWord {
	if runes[start] != wordQuote {
		end := indexSpace(runes, start)
		return quotedWord{start: start, end: end, text: string(runes[start:end])}
	}

	closingQuote := slices.Index(runes[start+1:], wordQuote)
	if closingQuote < 0 {
		return quotedWord{start: start, end: len(runes), text: string(runes[start+1:]), isQuoted: true}
	}

	closingQuote += start + 1
	return quotedWord{
		start:    start,
		end:      closingQuote + 1,
		text:     string(runes[start+1 : closingQuote]),
		isQuoted: true,
		isClosed: true,
	}
}

func quotedWordAt(runes []rune, start int, cursor int) trigger.Word {
	for _, word := range scanQuotedWords(runes, start) {
		if cursor < word.start {
			break
		}
		if cursor > word.end {
			continue
		}

		queryStart, queryEnd := word.start, cursor
		if word.isQuoted {
			queryStart = min(word.start+1, cursor)
		}
		if word.isClosed {
			queryEnd = min(cursor, word.end-1)
		}
		return trigger.Word{
			Start:    word.start,
			End:      word.end,
			Query:    string(runes[queryStart:queryEnd]),
			IsQuoted: word.isQuoted,
		}
	}

	return trigger.Word{Start: cursor, End: cursor}
}
