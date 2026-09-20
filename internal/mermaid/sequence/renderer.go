package sequence

import (
	"errors"
	"fmt"
	"strings"

	"crdx.org/oh/internal/mermaid/diagram"
	"crdx.org/oh/internal/mermaid/runewidth"
)

const (
	defaultSelfMessageWidth   = 4
	defaultMessageSpacing     = 1
	defaultParticipantSpacing = 5
	boxPaddingLeftRight       = 2
	minBoxWidth               = 3
	boxBorderWidth            = 2
	labelLeftMargin           = 2
	labelBufferSpace          = 10
	frameIndent               = 2
	frameLabelInset           = 2
)

type diagramLayout struct {
	participantWidths  []int
	participantCenters []int
	totalWidth         int
	messageSpacing     int
	selfMessageWidth   int
}

func calculateLayout(sd *SequenceDiagram, config *diagram.Config) *diagramLayout {
	participantSpacing := config.SequenceParticipantSpacing
	if participantSpacing <= 0 {
		participantSpacing = defaultParticipantSpacing
	}

	widths := make([]int, len(sd.Participants))
	for i, p := range sd.Participants {
		w := max(runewidth.StringWidth(p.Label)+boxPaddingLeftRight, minBoxWidth)
		widths[i] = w
	}

	centres := make([]int, len(sd.Participants))
	currentX := 0
	for i := range sd.Participants {
		boxWidth := widths[i] + boxBorderWidth
		if i == 0 {
			centres[i] = boxWidth / 2
			currentX = boxWidth
		} else {
			currentX += participantSpacing
			centres[i] = currentX + boxWidth/2
			currentX += boxWidth
		}
	}

	last := len(sd.Participants) - 1
	totalWidth := centres[last] + (widths[last]+boxBorderWidth)/2

	msgSpacing := config.SequenceMessageSpacing
	if msgSpacing <= 0 {
		msgSpacing = defaultMessageSpacing
	}
	selfWidth := config.SequenceSelfMessageWidth
	if selfWidth <= 0 {
		selfWidth = defaultSelfMessageWidth
	}

	return &diagramLayout{
		participantWidths:  widths,
		participantCenters: centres,
		totalWidth:         totalWidth,
		messageSpacing:     msgSpacing,
		selfMessageWidth:   selfWidth,
	}
}

func Render(sd *SequenceDiagram, config *diagram.Config) (string, error) {
	if sd == nil || len(sd.Participants) == 0 {
		return "", errors.New("no participants")
	}
	if config == nil {
		config = diagram.DefaultConfig()
	}

	chars := Unicode
	if config.UseAscii {
		chars = ASCII
	}

	layout := calculateLayout(sd, config)

	events := sd.Events
	if len(events) == 0 {
		for _, msg := range sd.Messages {
			events = append(events, Event{Kind: EventMessage, Message: msg})
		}
	}

	if gutter := (fragmentDepth(events) - 1) * frameIndent; gutter > 0 {
		shiftLayoutRight(layout, gutter)
	}

	if gutter := noteLeftGutter(events, layout); gutter > 0 {
		shiftLayoutRight(layout, gutter)
	}

	var lines []string

	lines = append(lines, buildLine(sd.Participants, layout, func(i int) string {
		return string(chars.TopLeft) + strings.Repeat(string(chars.Horizontal), layout.participantWidths[i]) + string(chars.TopRight)
	}))

	lines = append(lines, buildLine(sd.Participants, layout, func(i int) string {
		w := layout.participantWidths[i]
		labelLen := runewidth.StringWidth(sd.Participants[i].Label)
		pad := (w - labelLen) / 2
		return string(chars.Vertical) + strings.Repeat(" ", pad) + sd.Participants[i].Label +
			strings.Repeat(" ", w-pad-labelLen) + string(chars.Vertical)
	}))

	lines = append(lines, buildLine(sd.Participants, layout, func(i int) string {
		w := layout.participantWidths[i]
		return string(chars.BottomLeft) + strings.Repeat(string(chars.Horizontal), w/2) +
			string(chars.TeeDown) + strings.Repeat(string(chars.Horizontal), w-w/2-1) +
			string(chars.BottomRight)
	}))

	lines = append(lines, renderEvents(events, layout, chars)...)

	lines = append(lines, buildLifeline(layout, chars))
	return strings.Join(lines, "\n") + "\n", nil
}

func renderEvents(events []Event, layout *diagramLayout, chars BoxChars) []string {
	var lines []string
	for i := 0; i < len(events); {
		ev := events[i]
		if ev.Kind == EventFragmentStart {
			end := matchingFragmentEnd(events, i)
			lines = append(lines, wrapFragment(ev.Fragment, events[i+1:end], layout, chars)...)
			i = end + 1
			continue
		}

		if ev.Kind == EventFragmentEnd || ev.Kind == EventFragmentDivider {
			i++
			continue
		}

		if ev.Kind == EventNote {
			for range layout.messageSpacing {
				lines = append(lines, buildLifeline(layout, chars))
			}
			lines = append(lines, renderNote(ev.Note, layout, chars)...)
			i++
			continue
		}

		msg := ev.Message
		for range layout.messageSpacing {
			lines = append(lines, buildLifeline(layout, chars))
		}
		if msg.From == msg.To {
			lines = append(lines, renderSelfMessage(msg, layout, chars)...)
		} else {
			lines = append(lines, renderMessage(msg, layout, chars)...)
		}
		i++
	}
	return lines
}

func fragmentDepth(events []Event) int {
	maxDepth, cur := 0, 0
	for _, ev := range events {
		switch ev.Kind {
		case EventFragmentStart:
			cur++
			if cur > maxDepth {
				maxDepth = cur
			}
		case EventFragmentEnd:
			cur--
		case EventMessage, EventFragmentDivider, EventNote:
		}
	}
	return maxDepth
}

func matchingFragmentEnd(events []Event, start int) int {
	depth := 0
	for i := start; i < len(events); i++ {
		switch events[i].Kind {
		case EventFragmentStart:
			depth++
		case EventFragmentEnd:
			depth--
			if depth == 0 {
				return i
			}
		case EventMessage, EventFragmentDivider, EventNote:
		}
	}
	return len(events)
}

func shiftLayoutRight(layout *diagramLayout, n int) {
	for i := range layout.participantCenters {
		layout.participantCenters[i] += n
	}
	layout.totalWidth += n
}

func noteLeftGutter(events []Event, layout *diagramLayout) int {
	gutter, depth := 0, 0
	for _, ev := range events {
		switch ev.Kind {
		case EventFragmentStart:
			depth++
		case EventFragmentEnd:
			depth--
		case EventNote:
			left, _ := noteBoxColumns(ev.Note, layout)
			extra := 0
			if depth > 0 {
				extra = 1 + (depth-1)*frameIndent
			}
			if need := -left + extra; need > gutter {
				gutter = need
			}
		case EventMessage, EventFragmentDivider:
		}
	}
	return gutter
}

func noteCells(note *Note) []string {
	text := note.Text
	for _, br := range []string{"<br/>", "<br />", "<br>"} {
		text = strings.ReplaceAll(text, br, " ")
	}
	return runewidth.Cells(text)
}

func noteBoxColumns(note *Note, layout *diagramLayout) (int, int) {
	boxW := len(noteCells(note)) + 4
	centres := layout.participantCenters
	first := centres[note.Participants[0].Index]
	last := centres[note.Participants[len(note.Participants)-1].Index]

	switch note.Placement {
	case NoteRightOf:
		left := first + 2
		return left, left + boxW - 1
	case NoteLeftOf:
		right := first - 2
		return right - boxW + 1, right
	case NoteOver:
	}

	lo, hi := first, last
	if hi < lo {
		lo, hi = hi, lo
	}
	left, right := lo-1, hi+1
	if extra := boxW - (right - left + 1); extra > 0 {
		left -= extra / 2
		right += extra - extra/2
	}
	return left, right
}

func renderNote(note *Note, layout *diagramLayout, chars BoxChars) []string {
	cells := noteCells(note)
	left, right := noteBoxColumns(note, layout)
	if left < 0 {
		left = 0
	}

	border := func(leftBorder, rightBorder rune) string {
		line := padCells(buildLifeline(layout, chars), right+1)
		line[left] = string(leftBorder)
		for column := left + 1; column < right; column++ {
			line[column] = string(chars.Horizontal)
		}
		line[right] = string(rightBorder)
		return cellsToString(line)
	}

	middle := padCells(buildLifeline(layout, chars), right+1)
	for column := left; column <= right; column++ {
		middle[column] = " "
	}
	middle[left] = string(chars.Vertical)
	middle[right] = string(chars.Vertical)
	inner := right - left - 1
	column := left + 1 + (inner-len(cells))/2
	for _, cell := range cells {
		if column > left && column < right {
			middle[column] = cell
		}
		column++
	}

	return []string{
		border(chars.TopLeft, chars.TopRight),
		cellsToString(middle),
		border(chars.BottomLeft, chars.BottomRight),
	}
}

func wrapFragment(frag *Fragment, inner []Event, layout *diagramLayout, chars BoxChars) []string {
	sections, dividerLabels := splitSections(inner)
	var body []string
	dividerAt := map[int]string{}
	for i, sec := range sections {
		if i > 0 {
			dividerAt[len(body)] = dividerLabels[i-1]
			body = append(body, "")
		}
		body = append(body, renderEvents(sec, layout, chars)...)
	}
	body = append(body, buildLifeline(layout, chars))

	leftIndex, rightIndex := involvedParticipants(inner, layout)
	leftCol := layout.participantCenters[leftIndex] - frameIndent*(fragmentDepth(inner)+1)
	rightCol := layout.participantCenters[rightIndex] + frameIndent

	rd := 0
	for _, ev := range inner {
		switch ev.Kind {
		case EventFragmentStart:
			rd++
		case EventFragmentEnd:
			rd--
		case EventNote:
			nl, nr := noteBoxColumns(ev.Note, layout)
			if x := nl - 1 - rd*frameIndent; x < leftCol {
				leftCol = x
			}
			if x := nr + 1 + rd*frameIndent; x > rightCol {
				rightCol = x
			}
		case EventMessage, EventFragmentDivider:
		}
	}
	if leftCol < 0 {
		leftCol = 0
	}

	for _, line := range body {
		if lineWidth := runewidth.StringWidth(line) + 1; lineWidth > rightCol {
			rightCol = lineWidth
		}
	}

	label := frag.Type.String()
	if frag.Label != "" {
		label += " " + frag.Label
	}

	widen := func(text string) {
		if end := leftCol + frameLabelInset + runewidth.StringWidth("["+text+"]") + 1; end > rightCol {
			rightCol = end
		}
	}
	widen(label)
	for _, l := range dividerLabels {
		if l != "" {
			widen(l)
		}
	}

	out := []string{fragmentBorder(layout, chars, leftCol, rightCol, label, true)}
	for index, l := range body {
		if dl, ok := dividerAt[index]; ok {
			out = append(out, fragmentDivider(layout, chars, leftCol, rightCol, dl))
		} else {
			out = append(out, overlayFrameSides(l, chars, leftCol, rightCol))
		}
	}
	out = append(out, fragmentBorder(layout, chars, leftCol, rightCol, "", false))
	return out
}

func splitSections(inner []Event) ([][]Event, []string) {
	var sections [][]Event
	var labels []string
	cur := []Event{}
	depth := 0
	for _, ev := range inner {
		switch ev.Kind {
		case EventFragmentStart:
			depth++
		case EventFragmentEnd:
			depth--
		case EventFragmentDivider:
			if depth == 0 {
				sections = append(sections, cur)
				labels = append(labels, ev.Fragment.Label)
				cur = []Event{}
				continue
			}
		case EventMessage, EventNote:
		}
		cur = append(cur, ev)
	}
	return append(sections, cur), labels
}

func fragmentDivider(layout *diagramLayout, chars BoxChars, leftCol int, rightCol int, label string) string {
	line := padCells(buildLifeline(layout, chars), rightCol+1)
	line[leftCol] = string(chars.TeeRight)
	for column := leftCol + 1; column < rightCol; column++ {
		line[column] = string(chars.DottedLine)
	}
	line[rightCol] = string(chars.TeeLeft)
	if label != "" {
		column := leftCol + frameLabelInset
		for _, cell := range runewidth.Cells("[" + label + "]") {
			if column < rightCol {
				line[column] = cell
				column++
			}
		}
	}
	return cellsToString(line)
}

func involvedParticipants(events []Event, layout *diagramLayout) (int, int) {
	minIndex, maxIndex := -1, -1
	note := func(index int) {
		if minIndex == -1 || index < minIndex {
			minIndex = index
		}
		if maxIndex == -1 || index > maxIndex {
			maxIndex = index
		}
	}
	for _, ev := range events {
		if ev.Kind == EventMessage {
			note(ev.Message.From.Index)
			note(ev.Message.To.Index)
		}
	}
	if minIndex == -1 {
		return 0, len(layout.participantCenters) - 1
	}
	return minIndex, maxIndex
}

func fragmentBorder(layout *diagramLayout, chars BoxChars, leftCol int, rightCol int, label string, isTop bool) string {
	line := padCells(buildLifeline(layout, chars), rightCol+1)

	leftCorner, rightCorner := chars.BottomLeft, chars.BottomRight
	if isTop {
		leftCorner, rightCorner = chars.TopLeft, chars.TopRight
	}
	line[leftCol] = string(leftCorner)
	for column := leftCol + 1; column < rightCol; column++ {
		line[column] = string(chars.Horizontal)
	}
	line[rightCol] = string(rightCorner)

	if label != "" {
		column := leftCol + frameLabelInset
		for _, cell := range runewidth.Cells("[" + label + "]") {
			if column < rightCol {
				line[column] = cell
				column++
			}
		}
	}
	return cellsToString(line)
}

func overlayFrameSides(line string, chars BoxChars, leftCol int, rightCol int) string {
	cells := padCells(line, rightCol+1)
	cells[leftCol] = string(chars.Vertical)
	cells[rightCol] = string(chars.Vertical)
	return cellsToString(cells)
}

func padCells(text string, cellCount int) []string {
	cells := runewidth.Cells(text)
	for len(cells) < cellCount {
		cells = append(cells, " ")
	}
	return cells
}

func cellsToString(cells []string) string {
	return strings.TrimRight(strings.Join(cells, ""), " ")
}

func buildLine(participants []*Participant, layout *diagramLayout, draw func(int) string) string {
	var sb strings.Builder
	for i := range participants {
		boxWidth := layout.participantWidths[i] + boxBorderWidth
		left := layout.participantCenters[i] - boxWidth/2

		neededWidth := left - runewidth.StringWidth(sb.String())
		if neededWidth > 0 {
			sb.WriteString(strings.Repeat(" ", neededWidth))
		}
		sb.WriteString(draw(i))
	}
	return sb.String()
}

func drawRightwardArrow(line []string, from int, to int, arrow ArrowType, style rune, chars BoxChars) {
	line[from] = string(chars.TeeRight)

	for i := from + 1; i < to; i++ {
		line[i] = string(style)
	}

	if head, hasHead := arrow.head(chars, true); hasHead {
		line[to-1] = string(head)
	}

	if arrow.isBidirectional() {
		line[from+1] = string(chars.ArrowLeft)
	}

	line[to] = string(chars.Vertical)
}

func drawLeftwardArrow(line []string, from int, to int, arrow ArrowType, style rune, chars BoxChars) {
	line[to] = string(chars.Vertical)
	line[to+1] = string(style)

	if head, hasHead := arrow.head(chars, false); hasHead {
		line[to+1] = string(head)
	}

	for i := to + 2; i < from; i++ {
		line[i] = string(style)
	}

	if arrow.isBidirectional() {
		line[from-1] = string(chars.ArrowRight)
	}

	line[from] = string(chars.TeeLeft)
}

func buildLifeline(layout *diagramLayout, chars BoxChars) string {
	line := make([]string, layout.totalWidth+1)
	for i := range line {
		line[i] = " "
	}
	for _, column := range layout.participantCenters {
		if column < len(line) {
			line[column] = string(chars.Vertical)
		}
	}
	return cellsToString(line)
}

func renderMessage(msg *Message, layout *diagramLayout, chars BoxChars) []string {
	var lines []string
	from, to := layout.participantCenters[msg.From.Index], layout.participantCenters[msg.To.Index]

	label := msg.Label
	if msg.Number > 0 {
		label = fmt.Sprintf("%d. %s", msg.Number, msg.Label)
	}

	if label != "" {
		start := min(from, to) + labelLeftMargin
		labelWidth := runewidth.StringWidth(label)
		w := max(layout.totalWidth, start+labelWidth) + labelBufferSpace
		line := padCells(buildLifeline(layout, chars), w)
		column := start
		for _, cell := range runewidth.Cells(label) {
			if column < len(line) {
				line[column] = cell
				column++
			}
		}
		lines = append(lines, cellsToString(line))
	}

	line := runewidth.Cells(buildLifeline(layout, chars))
	style := chars.SolidLine
	if msg.ArrowType.isDotted() {
		style = chars.DottedLine
	}

	if from < to {
		drawRightwardArrow(line, from, to, msg.ArrowType, style, chars)
	} else {
		drawLeftwardArrow(line, from, to, msg.ArrowType, style, chars)
	}
	if msg.CentralFrom {
		line[from] = string(chars.Circle)
	}
	if msg.CentralTo {
		line[to] = string(chars.Circle)
	}
	lines = append(lines, cellsToString(line))
	return lines
}

func renderSelfMessage(msg *Message, layout *diagramLayout, chars BoxChars) []string {
	var lines []string
	center := layout.participantCenters[msg.From.Index]
	width := layout.selfMessageWidth

	ensureWidth := func(line string) []string {
		return padCells(line, layout.totalWidth+width+1)
	}

	label := msg.Label
	if msg.Number > 0 {
		label = fmt.Sprintf("%d. %s", msg.Number, msg.Label)
	}

	if label != "" {
		line := ensureWidth(buildLifeline(layout, chars))
		start := center + labelLeftMargin
		labelWidth := runewidth.StringWidth(label)
		neededWidth := start + labelWidth + labelBufferSpace
		for len(line) < neededWidth {
			line = append(line, " ")
		}
		column := start
		for _, cell := range runewidth.Cells(label) {
			if column < len(line) {
				line[column] = cell
				column++
			}
		}
		lines = append(lines, cellsToString(line))
	}

	style := chars.Horizontal
	if msg.ArrowType.isDotted() {
		style = chars.DottedLine
	}

	l1 := ensureWidth(buildLifeline(layout, chars))
	l1[center] = string(chars.TeeRight)
	if msg.CentralFrom {
		l1[center] = string(chars.Circle)
	}
	for i := 1; i < width; i++ {
		l1[center+i] = string(style)
	}
	l1[center+width-1] = string(chars.SelfTopRight)
	lines = append(lines, cellsToString(l1))

	l2 := ensureWidth(buildLifeline(layout, chars))
	l2[center+width-1] = string(chars.Vertical)
	lines = append(lines, cellsToString(l2))

	l3 := ensureWidth(buildLifeline(layout, chars))
	l3[center] = string(chars.Vertical)
	if msg.CentralTo {
		l3[center] = string(chars.Circle)
	}
	l3[center+1] = string(style)
	if head, ok := msg.ArrowType.head(chars, false); ok {
		l3[center+1] = string(head)
	}
	for i := 2; i < width-1; i++ {
		l3[center+i] = string(style)
	}
	l3[center+width-1] = string(chars.SelfBottom)
	lines = append(lines, cellsToString(l3))

	return lines
}
