package mermaid

import (
	"regexp"
	"strings"

	"crdx.org/oh/internal/mermaid/runewidth"
)

var htmlBreakPattern = regexp.MustCompile(`(?i)<br\s*/?>`)

const graphLabelLineGap = 1

type graphLabel struct {
	lines []string
	width int
}

func newGraphLabel(raw string) graphLabel {
	normalisedText := htmlBreakPattern.ReplaceAllString(raw, "\n")
	normalisedText = strings.ReplaceAll(normalisedText, `\n`, "\n")

	lines := strings.Split(normalisedText, "\n")

	width := 0
	for _, line := range lines {
		width = Max(width, runewidth.StringWidth(line))
	}

	return graphLabel{
		lines: lines,
		width: width,
	}
}

func (self graphLabel) contentHeight() int {
	if len(self.lines) == 0 {
		return 0
	}
	return len(self.lines) + (len(self.lines)-1)*graphLabelLineGap
}
