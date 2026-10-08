package strutil

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

func Capitalise(text string) string {
	characters := []rune(text)
	if len(characters) > 0 {
		characters[0] = unicode.ToUpper(characters[0])
	}
	return string(characters)
}

func Uncapitalise(text string) string {
	characters := []rune(text)
	if len(characters) > 0 {
		characters[0] = unicode.ToLower(characters[0])
	}
	return string(characters)
}

func CapitaliseSentence(text string) string {
	firstWord, _, _ := strings.Cut(text, " ")
	firstWord, _, _ = strings.Cut(firstWord, "\n")

	if firstWord == "" || isNameLike(firstWord) {
		return text
	}

	return Capitalise(text)
}

func isNameLike(word string) bool {
	return strings.ContainsAny(word, `./:\_`) || strings.ContainsFunc(word, unicode.IsDigit)
}

func OrDash(text string) string {
	if text == "" {
		return "—"
	}

	return text
}

func FirstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(line)
}

func MatchesQuery(text string, query string) bool {
	text = strings.ToLower(text)

	for word := range strings.FieldsSeq(strings.ToLower(query)) {
		if !strings.Contains(text, word) {
			return false
		}
	}

	return true
}

func Flatten(text string) string {
	return strings.Join(strings.Fields(stripped(text)), " ")
}

func PrintableLines(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = Printable(line)
	}

	return strings.Join(lines, "\n")
}

func Printable(text string) string {
	var out strings.Builder

	text = escapeSequence.ReplaceAllString(text, "")

	out.Grow(len(text))

	for _, character := range text {
		if !unicode.IsSpace(character) && !unicode.IsControl(character) {
			out.WriteRune(character)
			continue
		}

		out.WriteByte(' ')
	}

	return out.String()
}

const (
	controlSequence = `\[[0-?]*[ -/]*[@-~]?`
	commandString   = `\][^\a\x1b]*(?:\a|\x1b\\)?`
	otherSequence   = `[ -/]*[0-~]?`
)

var escapeSequence = regexp.MustCompile(
	`\x1b(?:` + controlSequence + `|` + commandString + `|` + otherSequence + `)`,
)

func StripControl(text string) string {
	return strings.Map(withoutControl, escapeSequence.ReplaceAllString(text, ""))
}

func ControlFreeLength(text string, from int) int {
	at := from
	for at < len(text) {
		character, size := utf8.DecodeRuneInString(text[at:])
		if character == utf8.RuneError && size <= 1 || withoutControl(character) != character {
			return at
		}
		at += size
	}

	return at
}

func withoutControl(character rune) rune {
	if character == '\n' || character == '\t' || !unicode.IsControl(character) {
		return character
	}

	return -1
}

func stripped(text string) string {
	return strings.Map(strippedRune, escapeSequence.ReplaceAllString(text, ""))
}

func strippedRune(character rune) rune {
	switch {
	case unicode.IsSpace(character):
		return ' '
	case unicode.IsControl(character):
		return -1
	default:
		return character
	}
}

func Lines(text string) []string {
	if text == "" {
		return nil
	}

	lines := strings.Split(text, "\n")
	if strings.HasSuffix(text, "\n") {
		lines = lines[:len(lines)-1]
	}

	return lines
}

func VisibleEscapes(stream string) string {
	var out strings.Builder

	for _, character := range stream {
		switch {
		case character == '\n':
			out.WriteByte('\n')
		case character == '\\':
			out.WriteString(`\\`)
		case character == '\x1b':
			out.WriteString(`\e`)
		case character == '\r':
			out.WriteString(`\r`)
		case character == '\t':
			out.WriteString(`\t`)
		case character < ' ' || character == 0x7f:
			fmt.Fprintf(&out, `\x%02X`, character)
		default:
			out.WriteRune(character)
		}
	}

	return out.String()
}
