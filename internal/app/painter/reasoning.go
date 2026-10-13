package painter

import "strings"

func withoutReasoningHeading(thought string) (string, bool) {
	if !strings.HasPrefix(thought, "**") {
		return thought, thought == "*"
	}

	title, after, isClosed := strings.Cut(thought[2:], "**")
	if strings.ContainsRune(title, '\n') || strings.HasPrefix(title, "*") {
		return thought, false
	}
	if !isClosed {
		return thought, true
	}
	if strings.TrimSpace(title) == "" || strings.HasSuffix(title, "*") {
		return thought, false
	}
	if len(after) < len("\n\n") {
		return thought, after == "" || after == "\n"
	}
	if !strings.HasPrefix(after, "\n\n") {
		return thought, false
	}

	body := after[len("\n\n"):]
	if strings.TrimSpace(body) == "" {
		return thought, true
	}

	return body, false
}
