package edit

import (
	"os"
	"strings"

	"crdx.org/oh/internal/state"
)

type History struct {
	path     string
	limit    int
	lines    []string
	isShared func() bool
}

func NewHistory(path string, limit int) *History {
	return &History{path: path, limit: limit, lines: readHistory(path, limit)}
}

func (self *History) ShareWhen(isShared func() bool) {
	self.isShared = isShared
}

func (self *History) Add(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}

	own, _ := withEntry(self.lines, line, self.limit)

	if self.path == "" {
		self.lines = own
		return
	}

	_ = state.Locked(self.path, func() error {
		everyEntry, isAdded := withEntry(readHistory(self.path, self.limit), line, self.limit)

		if self.isSharing() {
			own = everyEntry
		}

		if !isAdded {
			return nil
		}

		return state.WriteFile(self.path, []byte(encodeHistory(everyEntry)))
	})

	self.lines = own
}

func (self *History) isSharing() bool {
	return self.isShared != nil && self.isShared()
}

func (self *History) recall() *Recall {
	return &Recall{lines: self.lines, index: len(self.lines)}
}

func (self *History) search(current string) *historySearch {
	return &historySearch{
		lines:    self.lines,
		original: current,
		states: []searchState{{
			index: len(self.lines),
			text:  current,
		}},
	}
}

func withEntry(lines []string, line string, limit int) ([]string, bool) {
	if len(lines) > 0 && lines[len(lines)-1] == line {
		return lines, false
	}

	return trimmed(append(lines, line), limit), true
}

func trimmed(lines []string, limit int) []string {
	if limit > 0 && len(lines) > limit {
		return lines[len(lines)-limit:]
	}
	return lines
}

func readHistory(path string, limit int) []string {
	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path) //nolint:gosec // the path is ours, not user input
	if err != nil {
		return nil
	}

	var lines []string

	for line := range strings.SplitSeq(string(data), "\n") {
		if line != "" {
			lines = append(lines, unescape(line))
		}
	}

	return trimmed(lines, limit)
}

func encodeHistory(lines []string) string {
	var out strings.Builder

	for _, line := range lines {
		out.WriteString(escape(line))
		out.WriteString("\n")
	}

	return out.String()
}

func escape(line string) string {
	return strings.ReplaceAll(strings.ReplaceAll(line, `\`, `\\`), "\n", `\n`)
}

func unescape(line string) string {
	var out strings.Builder

	isEscaped := false

	for _, value := range line {
		switch {
		case isEscaped && value == 'n':
			out.WriteRune('\n')
			isEscaped = false
		case isEscaped:
			if value != '\\' {
				out.WriteRune('\\')
			}
			out.WriteRune(value)
			isEscaped = false
		case value == '\\':
			isEscaped = true
		default:
			out.WriteRune(value)
		}
	}

	if isEscaped {
		out.WriteRune('\\')
	}

	return out.String()
}

type Recall struct {
	lines        []string
	index        int
	pendingInput string
}

func (self *Recall) Walk(current string, direction int) (string, bool) {
	if direction < 0 {
		if self.index == 0 {
			return "", false
		}

		if self.index == len(self.lines) {
			self.pendingInput = current
		}

		self.index--

		return self.lines[self.index], true
	}

	if self.index >= len(self.lines) {
		return "", false
	}

	self.index++

	if self.index == len(self.lines) {
		return self.pendingInput, true
	}

	return self.lines[self.index], true
}

type historySearch struct {
	lines    []string
	original string
	states   []searchState
}

type searchState struct {
	query      string
	index      int
	text       string
	hasMatched bool
}

func (self *historySearch) getQuery() string {
	return self.current().query
}

func (self *historySearch) getText() string {
	return self.current().text
}

func (self *historySearch) add(value rune) {
	previous := self.current()
	query := previous.query + string(value)
	start := previous.index

	if !previous.hasMatched {
		if previous.query == "" {
			start = len(self.lines) - 1
		} else {
			start = -1
		}
	}

	self.states = append(self.states, self.find(query, start, previous))
}

func (self *historySearch) deleteBackward() {
	if len(self.states) > 1 {
		self.states = self.states[:len(self.states)-1]
	}
}

func (self *historySearch) previous() {
	current := self.current()
	start := current.index - 1
	if !current.hasMatched {
		if current.query == "" {
			start = len(self.lines) - 1
		} else {
			start = -1
		}
	}

	self.states[len(self.states)-1] = self.find(current.query, start, current)
}

func (self *historySearch) find(query string, start int, fallback searchState) searchState {
	for i := start; i >= 0; i-- {
		if strings.Contains(self.lines[i], query) {
			return searchState{query: query, index: i, text: self.lines[i], hasMatched: true}
		}
	}

	fallback.query = query
	fallback.hasMatched = false
	return fallback
}

func (self *historySearch) current() searchState {
	return self.states[len(self.states)-1]
}

func (self *historySearch) recall() *Recall {
	return &Recall{
		lines:        self.lines,
		index:        self.current().index,
		pendingInput: self.original,
	}
}
