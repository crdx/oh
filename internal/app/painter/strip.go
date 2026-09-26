package painter

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark/util"
)

type lineBoundary int

const (
	endOfThought lineBoundary = iota
	endOfLine
	endOfArrival
)

type plainThought struct {
	source        string
	consumedBytes int
	settledText   strings.Builder
	fenceMark     byte
	fenceLength   int
}

func (self *plainThought) Reset() {
	self.source = ""
	self.consumedBytes = 0
	self.settledText.Reset()
	self.fenceMark = 0
	self.fenceLength = 0
}

func (self *plainThought) Text(source string, isSettled bool) (string, string) {
	if !strings.HasPrefix(source, self.source[:min(self.consumedBytes, len(source))]) {
		self.Reset()
	}
	self.source = source

	for {
		at := strings.IndexByte(source[self.consumedBytes:], '\n')
		if at < 0 {
			break
		}

		line := source[self.consumedBytes : self.consumedBytes+at]
		self.consumedBytes += at + 1

		if text, isKept := self.strip(line, endOfLine); isKept {
			self.settledText.WriteString(text)
			self.settledText.WriteByte('\n')
		}
	}

	boundary := endOfThought
	if !isSettled {
		boundary = endOfArrival
	}

	tail, isKept := self.peek(source[self.consumedBytes:], boundary)
	if !isKept {
		tail = ""
	}

	return self.settledText.String(), tail
}

func (self *plainThought) peek(line string, boundary lineBoundary) (string, bool) {
	fenceMark, fenceLength := self.fenceMark, self.fenceLength
	text, isKept := self.strip(line, boundary)
	self.fenceMark, self.fenceLength = fenceMark, fenceLength

	return text, isKept
}

func (self *plainThought) strip(line string, boundary lineBoundary) (string, bool) {
	text := strings.TrimLeft(line, " \t")

	if self.fenceLength > 0 {
		if isClosingFence(text, self.fenceMark, self.fenceLength) {
			self.fenceLength = 0
			return "", false
		}

		return line, true
	}
	if mark, length := openingFence(text); length > 0 {
		self.fenceMark, self.fenceLength = mark, length
		return "", false
	}

	text = stripQuote(text)
	text = stripHeading(text)
	text, marker := stripMarker(text)

	if isRule(text) || isTableRule(text) {
		return "", false
	}

	text = stripTableRow(text)
	if boundary != endOfThought && hasHardBreak(text) {
		text = text[:len(text)-1]
	}

	return marker + stripInline(text, boundary == endOfArrival), true
}

func openingFence(line string) (byte, int) {
	if line == "" || (line[0] != '`' && line[0] != '~') {
		return 0, 0
	}

	length := runLength(line, 0, line[0])
	if length < 3 {
		return 0, 0
	}
	if line[0] == '`' && strings.IndexByte(line[length:], '`') >= 0 {
		return 0, 0
	}

	return line[0], length
}

func isClosingFence(line string, mark byte, length int) bool {
	run := runLength(line, 0, mark)

	return run >= length && strings.TrimRight(line[run:], " \t") == ""
}

func hasHardBreak(line string) bool {
	backslashes := 0
	for backslashes < len(line) && line[len(line)-1-backslashes] == '\\' {
		backslashes++
	}

	return backslashes%2 == 1
}

func isTableRule(line string) bool {
	text := strings.Trim(strings.TrimRight(line, " \t"), "|")
	if text == "" {
		return false
	}

	for at := range len(text) {
		if strings.IndexByte("-: |", text[at]) < 0 {
			return false
		}
	}

	return strings.ContainsRune(text, '-')
}

func stripTableRow(line string) string {
	text := strings.TrimRight(line, " \t")
	if !strings.HasPrefix(text, "|") || !strings.HasSuffix(text, "|") || len(text) < 2 {
		return line
	}

	var cells []string
	cellStart := 1
	for at := 1; at < len(text); at++ {
		switch text[at] {
		case '\\':
			at++
		case '|':
			cells = append(cells, strings.TrimSpace(text[cellStart:at]))
			cellStart = at + 1
		}
	}

	return strings.Join(strings.Fields(strings.Join(cells, " ")), " ")
}

func stripQuote(line string) string {
	for strings.HasPrefix(line, ">") {
		line = strings.TrimLeft(line[1:], " \t")
	}

	return line
}

func stripHeading(line string) string {
	hashes := runLength(line, 0, '#')
	if hashes == 0 || hashes > 6 {
		return line
	}
	if hashes < len(line) && line[hashes] != ' ' && line[hashes] != '\t' {
		return line
	}

	text := strings.TrimRight(line[hashes:], " \t")
	beforeHashes := strings.TrimRight(text, "#")
	if beforeHashes == "" || strings.HasSuffix(beforeHashes, " ") || strings.HasSuffix(beforeHashes, "\t") {
		text = beforeHashes
	}

	return strings.TrimSpace(text)
}

func stripMarker(line string) (string, string) {
	if len(line) > 1 && (line[0] == '-' || line[0] == '*' || line[0] == '+') && line[1] == ' ' {
		return strings.TrimLeft(line[2:], " "), "• "
	}

	digits := 0
	for digits < len(line) && line[digits] >= '0' && line[digits] <= '9' {
		digits++
	}
	if digits == 0 || digits > 9 || digits+1 >= len(line) {
		return line, ""
	}
	if line[digits] != '.' && line[digits] != ')' {
		return line, ""
	}
	if line[digits+1] != ' ' {
		return line, ""
	}

	number, _ := strconv.Atoi(line[:digits])

	return strings.TrimLeft(line[digits+2:], " "), strconv.Itoa(number) + ". "
}

func isRule(line string) bool {
	text := strings.TrimRight(line, " \t")
	if len(text) < 3 {
		return false
	}

	first := text[0]
	if first != '-' && first != '*' && first != '_' && first != '=' {
		return false
	}
	for at := range len(text) {
		if text[at] != first {
			return false
		}
	}

	return true
}

var (
	autolinkPattern = regexp.MustCompile(`^<([A-Za-z][A-Za-z0-9.+-]{1,31}:[^\s<>]*)>`)
	emailPattern    = regexp.MustCompile(
		"^<([a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?" +
			`(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*)>`,
	)
	entityPattern = regexp.MustCompile(`^&(?:#[xX][0-9a-fA-F]{1,6}|#[0-9]{1,7}|[A-Za-z][A-Za-z0-9]{1,31});`)
)

type inlinePiece struct {
	text     string
	mark     byte
	count    int
	canOpen  bool
	canClose bool
}

func stripInline(line string, isArriving bool) string {
	var pieces []inlinePiece
	var text strings.Builder

	for at := 0; at < len(line); {
		switch {
		case line[at] == '\\' && at+1 < len(line) && isPunctuation(line[at+1]):
			text.WriteByte(line[at+1])
			at += 2

		case line[at] == '`':
			length := runLength(line, at, '`')
			if end := closingCodeRun(line, at+length, length); end >= 0 {
				text.WriteString(line[at+length : end])
				at = end + length
			} else if isArriving {
				text.WriteString(line[at+length:])
				at = len(line)
			} else {
				text.WriteString(line[at : at+length])
				at += length
			}

		case line[at] == '<' && autolinkPattern.MatchString(line[at:]):
			match := autolinkPattern.FindStringSubmatch(line[at:])
			text.WriteString(match[1])
			at += len(match[0])

		case line[at] == '<' && emailPattern.MatchString(line[at:]):
			match := emailPattern.FindStringSubmatch(line[at:])
			text.WriteString(match[1])
			at += len(match[0])

		case line[at] == '&' && entityPattern.MatchString(line[at:]):
			entity := entityPattern.FindString(line[at:])
			text.Write(util.ResolveEntityNames(util.ResolveNumericReferences([]byte(entity))))
			at += len(entity)

		case line[at] == '*' || line[at] == '_' || line[at] == '~':
			if text.Len() > 0 {
				pieces = append(pieces, inlinePiece{text: text.String()})
				text.Reset()
			}
			length := runLength(line, at, line[at])
			pieces = append(pieces, delimiterRun(line, at, length))
			at += length

		case line[at] == '!' && at+1 < len(line) && line[at+1] == '[':
			link, isLink := parseInlineLink(line, at+1, isArriving)
			if !isLink {
				text.WriteByte('!')
				at++
				continue
			}

			if link.label != "" {
				text.WriteString(link.label)
				text.WriteByte(' ')
			}
			text.WriteString(link.addressed())
			at = link.end

		case line[at] == '[':
			link, isLink := parseInlineLink(line, at, isArriving)
			if !isLink {
				text.WriteByte('[')
				at++
				continue
			}

			text.WriteString(link.label)
			text.WriteByte(' ')
			text.WriteString(link.addressed())
			at = link.end

		default:
			text.WriteByte(line[at])
			at++
		}
	}
	if text.Len() > 0 {
		pieces = append(pieces, inlinePiece{text: text.String()})
	}

	pairDelimiters(pieces)
	if isArriving {
		for i := range pieces {
			if pieces[i].canOpen || i == len(pieces)-1 {
				pieces[i].count = 0
			}
		}
	}

	var out strings.Builder
	out.Grow(len(line))
	for _, piece := range pieces {
		out.WriteString(piece.text)
		out.WriteString(strings.Repeat(string(piece.mark), piece.count))
	}

	return out.String()
}

func runLength(line string, at int, mark byte) int {
	length := 0
	for at+length < len(line) && line[at+length] == mark {
		length++
	}

	return length
}

func closingCodeRun(line string, from int, length int) int {
	for at := from; at < len(line); {
		if line[at] != '`' {
			at++
			continue
		}

		run := runLength(line, at, '`')
		if run == length {
			return at
		}
		at += run
	}

	return -1
}

func delimiterRun(line string, at int, length int) inlinePiece {
	mark := line[at]
	before, _ := utf8.DecodeLastRuneInString(line[:at])
	after, _ := utf8.DecodeRuneInString(line[at+length:])

	isSpaceBefore := at == 0 || unicode.IsSpace(before)
	isSpaceAfter := at+length == len(line) || unicode.IsSpace(after)
	isPunctuationBefore := !isSpaceBefore && isUnicodePunctuation(before)
	isPunctuationAfter := !isSpaceAfter && isUnicodePunctuation(after)

	isLeftFlanking := !isSpaceAfter && (!isPunctuationAfter || isSpaceBefore || isPunctuationBefore)
	isRightFlanking := !isSpaceBefore && (!isPunctuationBefore || isSpaceAfter || isPunctuationAfter)

	piece := inlinePiece{mark: mark, count: length, canOpen: isLeftFlanking, canClose: isRightFlanking}
	switch {
	case mark == '_':
		piece.canOpen = isLeftFlanking && (!isRightFlanking || isPunctuationBefore)
		piece.canClose = isRightFlanking && (!isLeftFlanking || isPunctuationAfter)
	case mark == '~' && length > 2:
		piece.canOpen = false
		piece.canClose = false
	}

	return piece
}

func isUnicodePunctuation(character rune) bool {
	return unicode.IsPunct(character) || unicode.IsSymbol(character)
}

func pairDelimiters(pieces []inlinePiece) {
	for closer := range pieces {
		if !pieces[closer].canClose {
			continue
		}

		for opener := closer - 1; opener >= 0 && pieces[closer].count > 0; opener-- {
			candidate := &pieces[opener]
			if !candidate.canOpen || candidate.count == 0 || candidate.mark != pieces[closer].mark {
				continue
			}
			if candidate.mark == '~' && candidate.count != pieces[closer].count {
				continue
			}

			pairLength := min(candidate.count, pieces[closer].count)
			candidate.count -= pairLength
			pieces[closer].count -= pairLength
			for between := opener + 1; between < closer; between++ {
				pieces[between].canOpen = false
			}
		}
	}
}

type inlineLink struct {
	label       string
	destination string
	end         int
}

func (self inlineLink) addressed() string {
	return "(" + self.destination + ")"
}

func parseInlineLink(line string, at int, isArriving bool) (inlineLink, bool) {
	bracket := closingBracket(line, at)
	if bracket < 0 || bracket+1 >= len(line) || line[bracket+1] != '(' {
		return inlineLink{}, false
	}

	link := inlineLink{label: stripInline(line[at+1:bracket], false)}
	if destination, end, isParsed := parseDestination(line, bracket+2); isParsed {
		link.destination = destination
		link.end = end
		return link, true
	}
	if !isArriving {
		return inlineLink{}, false
	}

	link.destination = line[bracket+2:]
	link.end = len(line)

	return link, true
}

func parseDestination(line string, at int) (string, int, bool) {
	at = skipBlanks(line, at)

	var destination string
	switch {
	case at < len(line) && line[at] == '<':
		end := strings.IndexAny(line[at+1:], "<>")
		if end < 0 || line[at+1+end] != '>' {
			return "", 0, false
		}
		destination = line[at+1 : at+1+end]
		at += end + 2

	default:
		start := at
		depth := 0
		for ; at < len(line); at++ {
			character := line[at]
			if character == '\\' && at+1 < len(line) && isPunctuation(line[at+1]) {
				at++
				continue
			}
			if character == ' ' || character == '\t' || character < 0x20 {
				break
			}
			if character == '(' {
				depth++
			}
			if character == ')' {
				if depth == 0 {
					break
				}
				depth--
			}
		}
		if depth != 0 {
			return "", 0, false
		}
		destination = line[start:at]
	}

	if blank := skipBlanks(line, at); blank > at && blank < len(line) && strings.IndexByte(`"'(`, line[blank]) >= 0 {
		shut := line[blank]
		if shut == '(' {
			shut = ')'
		}
		end := strings.IndexByte(line[blank+1:], shut)
		if end < 0 {
			return "", 0, false
		}
		at = blank + 1 + end + 1
	}

	at = skipBlanks(line, at)
	if at >= len(line) || line[at] != ')' {
		return "", 0, false
	}

	return destination, at + 1, true
}

func skipBlanks(line string, at int) int {
	for at < len(line) && (line[at] == ' ' || line[at] == '\t') {
		at++
	}

	return at
}

func closingBracket(line string, at int) int {
	depth := 0
	for index := at; index < len(line); index++ {
		switch line[index] {
		case '\\':
			index++
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return index
			}
		}
	}

	return -1
}

func isPunctuation(character byte) bool {
	return strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", character) >= 0
}
