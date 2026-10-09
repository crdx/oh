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

func GetSequence(text string, start int) Sequence {
	if start+1 >= len(text) {
		return Sequence{End: len(text)}
	}

	switch text[start+1] {
	case '[':
		end := getCSIEnd(text, start)
		if cells, isCursorRight := getCursorRight(text[start:end]); isCursorRight {
			return Sequence{End: end, Cells: cells}
		}
		return Sequence{End: end, IsStyle: true}
	case ']':
		end, isTerminated := getOSCEnd(text, start)
		if !isTerminated {
			return Sequence{End: end}
		}
		sequence := text[start:end]
		if hyperlink, isHyperlink := getHyperlink(sequence); isHyperlink {
			return Sequence{End: end, Hyperlink: hyperlink, IsHyperlink: true}
		}
		sizedText, cells := getTextSizing(sequence)
		return Sequence{End: end, Text: sizedText, Cells: cells}
	case '_', 'P', '^', 'X':
		return Sequence{End: getStringEnd(text, start)}
	default:
		return Sequence{End: getLegacyEnd(text, start)}
	}
}

func GetEnd(text string, start int) int {
	return GetSequence(text, start).End
}

func getCSIEnd(text string, start int) int {
	for end := start + 2; end < len(text); end++ {
		if text[end] >= 0x40 && text[end] <= 0x7e {
			return end + 1
		}
	}
	return len(text)
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

func getOSCEnd(text string, start int) (int, bool) {
	for end := start + 2; end < len(text); end++ {
		switch {
		case text[end] == '\a':
			return end + 1, true
		case text[end] == '\x1b' && end+1 < len(text) && text[end+1] == '\\':
			return end + 2, true
		}
	}
	return len(text), false
}

func getStringEnd(text string, start int) int {
	for end := start + 2; end < len(text); end++ {
		switch {
		case text[end] == '\a':
			return end + 1
		case text[end] == '\x1b' && end+1 < len(text) && text[end+1] == '\\':
			return end + 2
		}
	}

	return len(text)
}

func getLegacyEnd(text string, start int) int {
	end := start + 1
	for end < len(text) && text[end] != 'm' && text[end] != 'K' {
		end++
	}
	if end < len(text) {
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
