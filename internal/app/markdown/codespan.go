package markdown

import "strings"

func CodeSpan(text string) string {
	longestRun := 0
	currentRun := 0
	for _, character := range text {
		if character == '`' {
			currentRun++
			longestRun = max(longestRun, currentRun)
		} else {
			currentRun = 0
		}
	}

	fence := strings.Repeat("`", longestRun+1)
	if longestRun > 0 {
		return fence + " " + text + " " + fence
	}
	return fence + text + fence
}
