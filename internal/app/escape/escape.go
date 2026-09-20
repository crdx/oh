package escape

import (
	"strconv"
	"strings"
)

const (
	maximumCursorCells = 65_535
	textSizingPrefix   = "\x1b]66;"
	HyperlinkClose     = "\x1b]8;;\x1b\\"
	MessageMark        = "\x1b]133;A\x1b\\"
)

type Sequence struct {
	End         int
	Text        string
	Cells       int
	Hyperlink   string
	IsStyle     bool
	IsHyperlink bool
}

func GetSequence(runes []rune, start int) Sequence {
	if start+1 >= len(runes) {
		return Sequence{End: len(runes)}
	}

	switch runes[start+1] {
	case '[':
		end := getCSIEnd(runes, start)
		if cells, isCursorRight := getCursorRight(string(runes[start:end])); isCursorRight {
			return Sequence{End: end, Cells: cells}
		}
		return Sequence{End: end, IsStyle: true}
	case ']':
		end, isTerminated := getOSCEnd(runes, start)
		if !isTerminated {
			return Sequence{End: end}
		}
		sequence := string(runes[start:end])
		if hyperlink, isHyperlink := getHyperlink(sequence); isHyperlink {
			return Sequence{End: end, Hyperlink: hyperlink, IsHyperlink: true}
		}
		text, cells := getTextSizing(sequence)
		return Sequence{End: end, Text: text, Cells: cells}
	case '_', 'P', '^', 'X':
		return Sequence{End: getStringEnd(runes, start)}
	default:
		return Sequence{End: getLegacyEnd(runes, start)}
	}
}

func GetEnd(runes []rune, start int) int {
	return GetSequence(runes, start).End
}

func getCSIEnd(runes []rune, start int) int {
	for end := start + 2; end < len(runes); end++ {
		if runes[end] >= 0x40 && runes[end] <= 0x7e {
			return end + 1
		}
	}
	return len(runes)
}

func getCursorRight(sequence string) (int, bool) {
	if !strings.HasPrefix(sequence, "\x1b[") || !strings.HasSuffix(sequence, "C") {
		return 0, false
	}

	cells, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(sequence, "\x1b["), "C"))
	if err != nil || cells <= 0 {
		return 0, false
	}
	return min(cells, maximumCursorCells), true
}

func getOSCEnd(runes []rune, start int) (int, bool) {
	for end := start + 2; end < len(runes); end++ {
		switch {
		case runes[end] == '\a':
			return end + 1, true
		case runes[end] == '\x1b' && end+1 < len(runes) && runes[end+1] == '\\':
			return end + 2, true
		}
	}
	return len(runes), false
}

func getStringEnd(runes []rune, start int) int {
	for end := start + 2; end < len(runes); end++ {
		switch {
		case runes[end] == '\a':
			return end + 1
		case runes[end] == '\x1b' && end+1 < len(runes) && runes[end+1] == '\\':
			return end + 2
		}
	}

	return len(runes)
}

func getLegacyEnd(runes []rune, start int) int {
	end := start + 1
	for end < len(runes) && runes[end] != 'm' && runes[end] != 'K' {
		end++
	}
	if end < len(runes) {
		end++
	}
	return end
}

func getHyperlink(sequence string) (string, bool) {
	body := strings.TrimPrefix(sequence, "\x1b]")
	body = strings.TrimSuffix(strings.TrimSuffix(body, "\x1b\\"), "\a")
	if !strings.HasPrefix(body, "8;") {
		return "", false
	}

	_, address, found := strings.Cut(strings.TrimPrefix(body, "8;"), ";")
	if !found {
		return "", false
	}
	if address == "" {
		return "", true
	}

	return sequence, true
}

func getTextSizing(sequence string) (string, int) {
	if !strings.HasPrefix(sequence, textSizingPrefix) {
		return "", 0
	}

	body := strings.TrimPrefix(sequence, textSizingPrefix)
	body = strings.TrimSuffix(strings.TrimSuffix(body, "\x1b\\"), "\a")
	metadata, text, found := strings.Cut(body, ";")
	if !found {
		return "", 0
	}

	scale, scaledWidth := 1, 0
	hasScaledWidth := false
	for field := range strings.SplitSeq(metadata, ":") {
		key, value, found := strings.Cut(field, "=")
		if !found {
			continue
		}
		parsedNumber, err := strconv.Atoi(value)
		if err != nil {
			if key == "s" || key == "w" {
				return "", 0
			}
			continue
		}
		switch key {
		case "s":
			if parsedNumber < 1 || parsedNumber > 7 {
				return "", 0
			}
			scale = parsedNumber
		case "w":
			if parsedNumber < 1 || parsedNumber > 7 {
				return "", 0
			}
			scaledWidth = parsedNumber
			hasScaledWidth = true
		}
	}
	if !hasScaledWidth || text == "" {
		return "", 0
	}
	return text, scale * scaledWidth
}
