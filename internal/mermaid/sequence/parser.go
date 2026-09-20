package sequence

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"crdx.org/oh/internal/mermaid/diagram"
)

const (
	SequenceDiagramKeyword = "sequenceDiagram"
	SolidArrowSyntax       = "->>"
	DottedArrowSyntax      = "-->>"
)

var (
	participantRegex = regexp.MustCompile(`(?i)^\s*(?:participant|actor)\s+(.+)$`)

	participantAsRegex = regexp.MustCompile(`(?i)^(.*?\S)\s+as\s+(.+)$`)

	fragmentKeywordRegex = regexp.MustCompile(`(?i)^(loop|opt|alt|par|critical|break|rect|else|and|option|end)\b`)

	arrowTokens = []string{"<<-->>", "<<->>", "-->>", "--x", "--)", "-->", "->>", "-x", "-)", "->"}

	autonumberRegex = regexp.MustCompile(`(?i)^\s*autonumber\s*$`)

	fragmentStartRegex = regexp.MustCompile(`(?i)^\s*(loop|opt|alt|par|critical|break|rect)\b\s*(.*)$`)

	fragmentDividerRegex = regexp.MustCompile(`(?i)^\s*(else|and|option)\b\s*(.*)$`)

	rectColorRegex = regexp.MustCompile(`(?i)^\s*rgba?\([^)]*\)\s*`)

	fragmentEndRegex = regexp.MustCompile(`(?i)^\s*end\s*$`)

	noteRegex = regexp.MustCompile(`(?i)^\s*note\s+(right of|left of|over)\s+([^:]+?)\s*:\s*(.*)$`)
)

type SequenceDiagram struct {
	Participants []*Participant
	Messages     []*Message
	Events       []Event
	Autonumber   bool
}

type FragmentType int

const (
	FragmentLoop FragmentType = iota
	FragmentOpt
	FragmentAlt
	FragmentPar
	FragmentCritical
	FragmentBreak
	FragmentRect
)

func (self FragmentType) String() string {
	switch self {
	case FragmentLoop:
		return "loop"
	case FragmentOpt:
		return "opt"
	case FragmentAlt:
		return "alt"
	case FragmentPar:
		return "par"
	case FragmentCritical:
		return "critical"
	case FragmentBreak:
		return "break"
	case FragmentRect:
		return "rect"
	default:
		return fmt.Sprintf("FragmentType(%d)", int(self))
	}
}

var fragmentKeywords = map[string]FragmentType{
	"loop": FragmentLoop, "opt": FragmentOpt, "alt": FragmentAlt,
	"par": FragmentPar, "critical": FragmentCritical,
	"break": FragmentBreak, "rect": FragmentRect,
}

var dividerKeywords = map[string]FragmentType{
	"else": FragmentAlt, "and": FragmentPar, "option": FragmentCritical,
}

type Fragment struct {
	Type  FragmentType
	Label string
}

type EventKind int

const (
	EventMessage EventKind = iota
	EventFragmentStart
	EventFragmentDivider
	EventFragmentEnd
	EventNote
)

func (self EventKind) String() string {
	switch self {
	case EventMessage:
		return "message"
	case EventFragmentStart:
		return "fragment-start"
	case EventFragmentDivider:
		return "fragment-divider"
	case EventFragmentEnd:
		return "fragment-end"
	case EventNote:
		return "note"
	default:
		return fmt.Sprintf("EventKind(%d)", int(self))
	}
}

type Event struct {
	Kind     EventKind
	Message  *Message
	Fragment *Fragment
	Note     *Note
}

type NotePlacement int

const (
	NoteOver NotePlacement = iota
	NoteLeftOf
	NoteRightOf
)

type Note struct {
	Placement    NotePlacement
	Participants []*Participant
	Text         string
}

type Participant struct {
	ID    string
	Label string
	Index int
}

type Message struct {
	From        *Participant
	To          *Participant
	Label       string
	ArrowType   ArrowType
	CentralFrom bool
	CentralTo   bool
	Number      int
}

type ArrowType int

const (
	SolidArrow ArrowType = iota
	DottedArrow
	SolidOpen
	DottedOpen
	SolidCross
	DottedCross
	SolidPoint
	DottedPoint
	BidirectionalSolid
	BidirectionalDotted
)

func (self ArrowType) String() string {
	switch self {
	case SolidArrow:
		return "solid"
	case DottedArrow:
		return "dotted"
	case SolidOpen:
		return "solid-open"
	case DottedOpen:
		return "dotted-open"
	case SolidCross:
		return "solid-cross"
	case DottedCross:
		return "dotted-cross"
	case SolidPoint:
		return "solid-point"
	case DottedPoint:
		return "dotted-point"
	case BidirectionalSolid:
		return "bidirectional-solid"
	case BidirectionalDotted:
		return "bidirectional-dotted"
	default:
		return fmt.Sprintf("ArrowType(%d)", int(self))
	}
}

func (self ArrowType) isDotted() bool {
	switch self {
	case DottedArrow, DottedOpen, DottedCross, DottedPoint, BidirectionalDotted:
		return true
	case SolidArrow, SolidOpen, SolidCross, SolidPoint, BidirectionalSolid:
		return false
	}
	return false
}

func (self ArrowType) isBidirectional() bool {
	return self == BidirectionalSolid || self == BidirectionalDotted
}

func (self ArrowType) head(chars BoxChars, isRightward bool) (rune, bool) {
	switch self {
	case SolidArrow, DottedArrow, BidirectionalSolid, BidirectionalDotted:
		if isRightward {
			return chars.ArrowRight, true
		}
		return chars.ArrowLeft, true
	case SolidCross, DottedCross:
		return chars.CrossHead, true
	case SolidPoint, DottedPoint:
		if isRightward {
			return chars.PointRight, true
		}
		return chars.PointLeft, true
	case SolidOpen, DottedOpen:
		return 0, false
	}
	return 0, false
}

func IsSequenceDiagram(input string) bool {
	lines := strings.SplitSeq(input, "\n")
	for line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine == "" || strings.HasPrefix(trimmedLine, "%%") {
			continue
		}
		return hasSequenceKeyword(trimmedLine)
	}
	return false
}

func hasSequenceKeyword(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	kw := strings.ToLower(SequenceDiagramKeyword)
	if !strings.HasPrefix(lower, kw) {
		return false
	}
	rest := lower[len(kw):]
	return rest == "" || rest[0] == ' ' || rest[0] == '\t'
}

func Parse(input string) (*SequenceDiagram, error) {
	if !utf8.ValidString(input) {
		return nil, errors.New("input is not valid UTF-8")
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return nil, errors.New("empty input")
	}

	rawLines := diagram.SplitLines(input)
	lines := diagram.RemoveComments(rawLines)
	if len(lines) == 0 {
		return nil, errors.New("no content found")
	}

	if !hasSequenceKeyword(strings.TrimSpace(lines[0])) {
		return nil, fmt.Errorf("expected %q keyword", SequenceDiagramKeyword)
	}
	lines = lines[1:]

	sd := &SequenceDiagram{
		Participants: []*Participant{},
		Messages:     []*Message{},
		Autonumber:   false,
	}
	participantMap := make(map[string]*Participant)
	var openFragments []FragmentType

	for i, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		if autonumberRegex.MatchString(trimmedLine) {
			sd.Autonumber = true
			continue
		}

		if matches := noteRegex.FindStringSubmatch(trimmedLine); matches != nil {
			placement := NoteOver
			switch strings.ToLower(matches[1]) {
			case "left of":
				placement = NoteLeftOf
			case "right of":
				placement = NoteRightOf
			}
			var parts []*Participant
			for id := range strings.SplitSeq(matches[2], ",") {
				id = strings.Trim(strings.TrimSpace(id), `"`)
				if id != "" {
					parts = append(parts, sd.getParticipant(id, participantMap))
				}
			}
			if len(parts) == 0 {
				return nil, fmt.Errorf("line %d: note without a participant", i+2)
			}
			text := strings.TrimSpace(matches[3])
			for _, pre := range []string{"nowrap:", "wrap:"} {
				if strings.HasPrefix(strings.ToLower(text), pre) {
					text = strings.TrimSpace(text[len(pre):])
					break
				}
			}
			sd.Events = append(sd.Events, Event{
				Kind: EventNote,
				Note: &Note{Placement: placement, Participants: parts, Text: text},
			})
			continue
		}

		if wasMatched, err := sd.parseParticipant(trimmedLine, participantMap); err != nil {
			return nil, fmt.Errorf("line %d: %w", i+2, err)
		} else if wasMatched {
			continue
		}

		if sd.parseMessage(trimmedLine, participantMap) {
			continue
		}

		if match := fragmentStartRegex.FindStringSubmatch(trimmedLine); match != nil {
			fType := fragmentKeywords[strings.ToLower(match[1])]
			label := strings.TrimSpace(match[2])
			if fType == FragmentRect {
				label = strings.TrimSpace(rectColorRegex.ReplaceAllString(label, ""))
			}
			sd.Events = append(sd.Events, Event{
				Kind:     EventFragmentStart,
				Fragment: &Fragment{Type: fType, Label: label},
			})
			openFragments = append(openFragments, fType)
			continue
		}

		if match := fragmentDividerRegex.FindStringSubmatch(trimmedLine); match != nil {
			want := dividerKeywords[strings.ToLower(match[1])]
			if len(openFragments) == 0 || openFragments[len(openFragments)-1] != want {
				return nil, fmt.Errorf("line %d: %q outside a matching %s block", i+2, trimmedLine, want)
			}
			sd.Events = append(sd.Events, Event{
				Kind:     EventFragmentDivider,
				Fragment: &Fragment{Type: want, Label: strings.TrimSpace(match[2])},
			})
			continue
		}

		if fragmentEndRegex.MatchString(trimmedLine) {
			if len(openFragments) == 0 {
				return nil, fmt.Errorf("line %d: %q without a matching fragment opener", i+2, trimmedLine)
			}
			sd.Events = append(sd.Events, Event{Kind: EventFragmentEnd})
			openFragments = openFragments[:len(openFragments)-1]
			continue
		}

		return nil, fmt.Errorf("line %d: invalid syntax: %q", i+2, trimmedLine)
	}

	if len(openFragments) > 0 {
		return nil, fmt.Errorf("unclosed fragment: missing %d \"end\"", len(openFragments))
	}

	if len(sd.Participants) == 0 {
		return nil, errors.New("no participants found")
	}

	return sd, nil
}

func (self *SequenceDiagram) parseParticipant(line string, participants map[string]*Participant) (bool, error) {
	match := participantRegex.FindStringSubmatch(line)
	if match == nil {
		return false, nil
	}

	rest := strings.TrimSpace(match[1])
	id := rest
	label := ""
	if asMatch := participantAsRegex.FindStringSubmatch(rest); asMatch != nil {
		id, label = asMatch[1], asMatch[2]
	}
	id, idOK := parseName(id)
	if !idOK {
		return true, fmt.Errorf("invalid participant name %q", id)
	}
	if label == "" {
		label = id
	}
	label = strings.Trim(label, `"`)

	if _, exists := participants[id]; exists {
		return true, fmt.Errorf("duplicate participant %q", id)
	}

	participant := &Participant{
		ID:    id,
		Label: label,
		Index: len(self.Participants),
	}
	self.Participants = append(self.Participants, participant)
	participants[id] = participant
	return true, nil
}

func findArrow(line string) (int, string) {
	isInQuotes := false
	for i := range len(line) {
		switch {
		case line[i] == '"':
			isInQuotes = !isInQuotes
		case !isInQuotes && (line[i] == '-' || line[i] == '<'):
			for _, tok := range arrowTokens {
				if strings.HasPrefix(line[i:], tok) {
					return i, tok
				}
			}
		}
	}
	return -1, ""
}

func validName(name string) bool {
	return name != "" && !strings.ContainsAny(name, `"<>:,;(`)
}

func parseName(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) >= 2 && strings.HasPrefix(raw, `"`) && strings.HasSuffix(raw, `"`) {
		inner := raw[1 : len(raw)-1]
		return inner, inner != "" && !strings.Contains(inner, `"`)
	}
	return raw, validName(raw)
}

type parsedMessage struct {
	fromID      string
	toID        string
	arrow       string
	label       string
	centralFrom bool
	centralTo   bool
}

func splitMessage(line string) (parsedMessage, bool) {
	index, arrow := findArrow(line)
	if index < 0 {
		return parsedMessage{}, false
	}

	left := strings.TrimSpace(line[:index])
	if !strings.HasPrefix(left, `"`) && fragmentKeywordRegex.MatchString(left) {
		return parsedMessage{}, false
	}
	centralFrom := strings.HasSuffix(left, "()")
	if centralFrom {
		left = strings.TrimSpace(strings.TrimSuffix(left, "()"))
	}
	fromID, fromOK := parseName(left)

	rest := strings.TrimSpace(line[index+len(arrow):])
	centralTo := strings.HasPrefix(rest, "()")
	if centralTo {
		rest = strings.TrimSpace(strings.TrimPrefix(rest, "()"))
	}
	start := 0
	if strings.HasPrefix(rest, `"`) {
		if end := strings.Index(rest[1:], `"`); end >= 0 {
			start = end + 2
		}
	}
	colon := strings.Index(rest[start:], ":")
	if colon < 0 {
		return parsedMessage{}, false
	}
	colon += start
	toID, toOK := parseName(rest[:colon])
	if !fromOK || !toOK {
		return parsedMessage{}, false
	}

	return parsedMessage{
		fromID:      fromID,
		toID:        toID,
		arrow:       arrow,
		label:       strings.TrimSpace(rest[colon+1:]),
		centralFrom: centralFrom,
		centralTo:   centralTo,
	}, true
}

func (self *SequenceDiagram) parseMessage(line string, participants map[string]*Participant) bool {
	parsedMessage, ok := splitMessage(line)
	if !ok {
		return false
	}

	from := self.getParticipant(parsedMessage.fromID, participants)
	to := self.getParticipant(parsedMessage.toID, participants)

	var aType ArrowType
	switch parsedMessage.arrow {
	case "->>":
		aType = SolidArrow
	case "-->>":
		aType = DottedArrow
	case "->":
		aType = SolidOpen
	case "-->":
		aType = DottedOpen
	case "-x":
		aType = SolidCross
	case "--x":
		aType = DottedCross
	case "-)":
		aType = SolidPoint
	case "--)":
		aType = DottedPoint
	case "<<->>":
		aType = BidirectionalSolid
	case "<<-->>":
		aType = BidirectionalDotted
	}

	msgNumber := 0
	if self.Autonumber {
		msgNumber = len(self.Messages) + 1
	}

	msg := &Message{
		From:        from,
		To:          to,
		Label:       parsedMessage.label,
		ArrowType:   aType,
		CentralFrom: parsedMessage.centralFrom,
		CentralTo:   parsedMessage.centralTo,
		Number:      msgNumber,
	}
	self.Messages = append(self.Messages, msg)
	self.Events = append(self.Events, Event{Kind: EventMessage, Message: msg})
	return true
}

func (self *SequenceDiagram) getParticipant(id string, participants map[string]*Participant) *Participant {
	if p, exists := participants[id]; exists {
		return p
	}

	participant := &Participant{
		ID:    id,
		Label: id,
		Index: len(self.Participants),
	}
	self.Participants = append(self.Participants, participant)
	participants[id] = participant
	return participant
}
