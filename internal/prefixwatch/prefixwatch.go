package prefixwatch

import (
	"crypto/sha256"
	"encoding/json"
	"strconv"
)

const (
	ToolsChanged  = "the tool list changed"
	SystemChanged = "the system prompt changed"
	TurnChanged   = "an earlier turn changed"
	TurnsDropped  = "earlier turns were dropped"
)

type mark [sha256.Size]byte

const answeredRole = "assistant"

type turnMark struct {
	raw        mark
	bare       mark
	isAnswered bool
}

type Watcher struct {
	tools    mark
	system   mark
	turns    []turnMark
	bound    int
	wasAsked bool
}

func (self *Watcher) Look(tools any, system any, turns []json.RawMessage) string {
	turnMarks := self.marksOf(turns)
	toolsMark, systemMark := markOf(tools), markOf(system)

	previous := *self
	self.tools, self.system, self.turns = toolsMark, systemMark, turnMarks
	self.bound, self.wasAsked = boundOf(turnMarks), true

	switch {
	case !previous.wasAsked:
		return ""
	case toolsMark != previous.tools:
		return ToolsChanged
	case systemMark != previous.system:
		return SystemChanged
	case len(turnMarks) < previous.bound:
		return TurnsDropped
	}

	for at := range previous.bound {
		if turnMarks[at].bare != previous.turns[at].bare {
			return TurnChanged + " (" + strconv.Itoa(at+1) + " of " + strconv.Itoa(previous.bound) + ")"
		}
	}

	return ""
}

func boundOf(turns []turnMark) int {
	bound := 0
	for at, turn := range turns {
		if turn.isAnswered {
			bound = at + 1
		}
	}

	return bound
}

func (self *Watcher) marksOf(turns []json.RawMessage) []turnMark {
	marks := make([]turnMark, len(turns))
	for at, turn := range turns {
		raw := sha256.Sum256(turn)
		if at < len(self.turns) && self.turns[at].raw == raw {
			marks[at] = self.turns[at]
			continue
		}

		marks[at] = turnMark{raw: raw, bare: markOf(turn), isAnswered: isAnswered(turn)}
	}

	return marks
}

func isAnswered(turn json.RawMessage) bool {
	var read struct {
		Role string `json:"role"`
	}

	return json.Unmarshal(turn, &read) == nil && read.Role == answeredRole
}

func markOf(value any) mark {
	payload, err := json.Marshal(value)
	if err != nil {
		return mark{}
	}

	var read any
	if json.Unmarshal(payload, &read) != nil {
		return sha256.Sum256(payload)
	}

	bare, err := json.Marshal(withoutBreakpoints(read))
	if err != nil {
		return sha256.Sum256(payload)
	}

	return sha256.Sum256(bare)
}

func withoutBreakpoints(value any) any {
	switch node := value.(type) {
	case map[string]any:
		fields := make(map[string]any, len(node))
		for key, field := range node {
			if key == "cache_control" {
				continue
			}
			fields[key] = withoutBreakpoints(field)
		}

		return fields

	case []any:
		items := make([]any, len(node))
		for at, item := range node {
			items[at] = withoutBreakpoints(item)
		}

		return items
	}

	return value
}
