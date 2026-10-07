package markdown

import (
	"strings"

	"crdx.org/oh/internal/util/strutil"
)

func CodeBlock(language string, body string) string {
	longestRun := 0
	for _, line := range strutil.Lines(body) {
		run := 0
		for run < len(line) && line[run] == '`' {
			run++
		}
		longestRun = max(longestRun, run)
	}

	fence := strings.Repeat("`", max(3, longestRun+1))

	return fence + language + "\n" + body + "\n" + fence
}
