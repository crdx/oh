package edit

import (
	"os"
	"path/filepath"
	"strings"
)

type History struct {
	path  string
	limit int
	lines []string
}

func NewHistory(path string, limit int) *History {
	self := &History{path: path, limit: limit}

	data, err := os.ReadFile(path) //nolint:gosec // the path is ours, not user input
	if err != nil {
		return self
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		if line != "" {
			self.lines = append(self.lines, unescape(line))
		}
	}

	self.trim()

	return self
}

func (self *History) Add(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}

	if len(self.lines) > 0 && self.lines[len(self.lines)-1] == line {
		return
	}

	self.lines = append(self.lines, line)
	self.trim()
	self.save()
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

func (self *History) trim() {
	if self.limit > 0 && len(self.lines) > self.limit {
		self.lines = self.lines[len(self.lines)-self.limit:]
	}
}

func (self *History) save() {
	if self.path == "" {
		return
	}

	if err := os.MkdirAll(filepath.Dir(self.path), 0o700); err != nil {
		return
	}

	var out strings.Builder

	for _, line := range self.lines {
		out.WriteString(escape(line))
		out.WriteString("\n")
	}

	_ = os.WriteFile(self.path, []byte(out.String()), 0o600)
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
