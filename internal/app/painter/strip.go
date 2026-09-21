package painter

import "strings"

type plainThought struct {
	source        string
	consumedBytes int
	settledText   strings.Builder
	isInFence     bool
}

func (self *plainThought) Reset() {
	self.source = ""
	self.consumedBytes = 0
	self.settledText.Reset()
	self.isInFence = false
}

func (self *plainThought) Text(source string) (string, string) {
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

		if text, isKept := self.strip(line); isKept {
			self.settledText.WriteString(text)
			self.settledText.WriteByte('\n')
		}
	}

	tail, isKept := self.peek(source[self.consumedBytes:])
	if !isKept {
		tail = ""
	}

	return self.settledText.String(), tail
}

func (self *plainThought) peek(line string) (string, bool) {
	fenceState := self.isInFence
	text, isKept := self.strip(line)
	self.isInFence = fenceState

	return text, isKept
}

func (self *plainThought) strip(line string) (string, bool) {
	text := strings.TrimLeft(line, " \t")

	if strings.HasPrefix(text, "```") || strings.HasPrefix(text, "~~~") {
		self.isInFence = !self.isInFence
		return "", false
	}
	if self.isInFence {
		return line, true
	}

	text = stripQuote(text)
	text = stripHeading(text)
	text, hasMarker := stripMarker(text)

	if isRule(text) || isTableRule(text) {
		return "", false
	}

	text = stripInline(stripTableRow(text))
	if hasMarker {
		return "• " + text, true
	}

	return text, true
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

	return strings.Join(strings.Fields(strings.ReplaceAll(strings.Trim(text, "|"), "|", " ")), " ")
}

func stripQuote(line string) string {
	for strings.HasPrefix(line, ">") {
		line = strings.TrimLeft(line[1:], " \t")
	}

	return line
}

func stripHeading(line string) string {
	hashes := 0
	for hashes < len(line) && line[hashes] == '#' {
		hashes++
	}
	if hashes == 0 || hashes > 6 {
		return line
	}
	if hashes < len(line) && line[hashes] != ' ' {
		return line
	}

	return strings.TrimLeft(line[hashes:], " ")
}

func stripMarker(line string) (string, bool) {
	if len(line) > 1 && (line[0] == '-' || line[0] == '*' || line[0] == '+') && line[1] == ' ' {
		return strings.TrimLeft(line[2:], " "), true
	}

	digits := 0
	for digits < len(line) && line[digits] >= '0' && line[digits] <= '9' {
		digits++
	}
	if digits == 0 || digits+1 >= len(line) {
		return line, false
	}
	if line[digits] != '.' && line[digits] != ')' {
		return line, false
	}
	if line[digits+1] != ' ' {
		return line, false
	}

	return strings.TrimLeft(line[digits+2:], " "), true
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

func stripInline(line string) string {
	var out strings.Builder
	out.Grow(len(line))

	for at := 0; at < len(line); {
		switch {
		case line[at] == '\\' && at+1 < len(line) && isPunctuation(line[at+1]):
			out.WriteByte(line[at+1])
			at += 2

		case line[at] == '`':
			at += runLength(line, at, '`')

		case line[at] == '*' || line[at] == '_' || line[at] == '~':
			at += runLength(line, at, line[at])

		case line[at] == '!' && at+1 < len(line) && line[at+1] == '[':
			at++

		case line[at] == '[':
			out.WriteString(labelOf(line, at))
			at = skipLink(line, at)

		default:
			out.WriteByte(line[at])
			at++
		}
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

func labelOf(line string, at int) string {
	if end := closingBracket(line, at); end > 0 {
		return stripInline(line[at+1 : end])
	}

	return ""
}

func skipLink(line string, at int) int {
	bracket := closingBracket(line, at)
	if bracket < 0 {
		return at + 1
	}
	if bracket+1 < len(line) && (line[bracket+1] == '(' || line[bracket+1] == '[') {
		shut := byte(')')
		if line[bracket+1] == '[' {
			shut = ']'
		}
		if end := strings.IndexByte(line[bracket+1:], shut); end >= 0 {
			return bracket + 1 + end + 1
		}
	}

	return bracket + 1
}

func closingBracket(line string, at int) int {
	depth := 0
	for index := at; index < len(line); index++ {
		switch line[index] {
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
	return strings.IndexByte("\\`*_{}[]()#+-.!~>|", character) >= 0
}
