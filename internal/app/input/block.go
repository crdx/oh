package input

import (
	"strings"

	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/style"
)

const (
	edgePad                    = 2
	labelGap                   = 3
	edgeGap                    = 1
	feedbackInset              = 1
	feedbackBorderWidth        = 2*feedbackInset + 2
	feedbackFrameWidth         = feedbackBorderWidth + 2
	MinimumFramedFeedbackWidth = feedbackFrameWidth + 1
)

func FeedbackContentWidth(width int) int {
	if width < MinimumFramedFeedbackWidth {
		return width
	}

	return width - feedbackFrameWidth
}

func FeedbackRuleWidth(width int) int {
	if width < MinimumFramedFeedbackWidth {
		return width
	}

	return width - feedbackBorderWidth
}

type Ruler struct {
	Left   string
	Center string
	Right  string
}

func LeftContentWidth(width int, right string) int {
	_, rightWidth := keptSides(width, "", right)

	return max(width-rightWidth-getGap(rightWidth > 0)-(edgePad+1), 0)
}

func CenterContentWidth(width int, left string, right string) int {
	leftWidth, rightWidth := keptSides(width, left, right)

	return max(width-leftWidth-rightWidth-getGap(leftWidth > 0)-getGap(rightWidth > 0), 0)
}

func keptSides(width int, left string, right string) (int, int) {
	rightWidth := getSideWidth(right)
	if rightWidth+getGap(false) > width {
		rightWidth = 0
	}

	leftWidth := getSideWidth(left)
	if leftWidth > 0 && leftWidth+getGap(rightWidth > 0)+rightWidth > width {
		leftWidth = 0
	}

	return leftWidth, rightWidth
}

func getGap(isBetweenLabels bool) int {
	if isBetweenLabels {
		return labelGap
	}

	return edgeGap
}

func getSideWidth(text string) int {
	cells := style.Width(text)
	if cells == 0 {
		return 0
	}

	return edgePad + 1 + cells
}

type Block struct {
	Top            Ruler
	Input          edit.Frame
	Bottom         Ruler
	Activity       []string
	QueuedMessages []string
	QueueHint      string
	Feedback       []string
	Question       []string
	Dropdown       []string
	Rule           style.Style
}

func (self Block) Rows(width int) ([]string, int, int) {
	rows := make([]string, 0, len(self.Activity)+len(self.QueuedMessages)+len(self.Feedback)+len(self.Input.Rows)+len(self.Dropdown)+4)

	top := self.Top
	if self.Input.IsSearching && !self.isAsking() {
		top.Left = style.Subtle("reverse-i-search: " + self.Input.SearchQuery)
	}

	bottom := self.Bottom
	if leftWidth, rightWidth := getSideWidth(bottom.Left), getSideWidth(bottom.Right); leftWidth > 0 && leftWidth+getGap(rightWidth > 0)+rightWidth > width {
		bottom.Right = ""
	}

	body, bodyRow, bodyColumn := self.body()
	rule := self.rule()
	tabRows, topWidth := self.renderTab(width, rule)

	rows = append(rows, self.Activity...)
	rows = append(rows, tabRows...)
	topRule := top.render(topWidth, rule)
	if topWidth != width {
		reach := strings.Repeat("─", feedbackInset)
		topRule = rule(reach+"┴") + topRule + rule("┴"+reach)
	}
	rows = append(rows, topRule)
	rows = append(rows, body...)
	if !self.isAsking() {
		rows = append(rows, self.Dropdown...)
	}
	rows = append(rows, bottom.render(width, rule))

	return rows, len(self.Activity) + len(tabRows) + bodyRow + 1, bodyColumn
}

func (self Block) renderTab(width int, rule style.Style) ([]string, int) {
	if len(self.QueuedMessages) == 0 && len(self.Feedback) == 0 {
		return nil, width
	}
	if width < MinimumFramedFeedbackWidth {
		return append(append([]string{}, self.QueuedMessages...), self.Feedback...), width
	}

	ruleWidth := FeedbackRuleWidth(width)
	contentWidth := FeedbackContentWidth(width)
	inset := strings.Repeat(" ", feedbackInset)
	frame := func(content string) string {
		padding := strings.Repeat(" ", max(contentWidth-style.Width(content), 0))
		return inset + rule("│") + " " + content + padding + " " + rule("│")
	}

	rows := make([]string, 0, len(self.QueuedMessages)+len(self.Feedback)+2)
	rows = append(rows, inset+self.tabTop(ruleWidth, rule))
	for _, message := range self.QueuedMessages {
		rows = append(rows, frame(message))
	}
	if len(self.QueuedMessages) > 0 && len(self.Feedback) > 0 {
		rows = append(rows, inset+rule("├"+strings.Repeat("─", ruleWidth)+"┤"))
	}
	for _, feedback := range self.Feedback {
		rows = append(rows, frame(feedback))
	}

	return rows, ruleWidth
}

func (self Block) tabTop(ruleWidth int, rule style.Style) string {
	if len(self.QueuedMessages) == 0 || self.QueueHint == "" {
		return rule("╭" + strings.Repeat("─", ruleWidth) + "╮")
	}

	label := " " + self.QueueHint + " "
	lead := ruleWidth - style.Width(label) - edgePad
	if lead < edgePad {
		return rule("╭" + strings.Repeat("─", ruleWidth) + "╮")
	}

	return rule("╭"+strings.Repeat("─", lead)) + style.Subtle(label) + rule(strings.Repeat("─", edgePad)+"╮")
}

func (self Block) isAsking() bool {
	return len(self.Question) > 0
}

func (self Block) body() ([]string, int, int) {
	if !self.isAsking() {
		return self.Input.Rows, self.Input.Row, self.Input.Column
	}

	return self.Question, len(self.Question) - 1, 0
}

func (self Block) rule() style.Style {
	if self.Rule == nil {
		return style.Rule
	}

	return self.Rule
}

func (self Ruler) render(width int, rule style.Style) string {
	leftWidth, rightWidth := keptSides(width, self.Left, self.Right)
	hasLeft, hasRight := leftWidth > 0, rightWidth > 0
	middleWidth := width - leftWidth - rightWidth

	head := ""
	if hasLeft {
		head = rule(strings.Repeat("─", edgePad)) + " " + self.Left
	}

	tail := ""
	if hasRight {
		tail = self.Right + " " + rule(strings.Repeat("─", edgePad))
	}

	centerWidth := style.Width(self.Center)
	earliestBefore := getGap(hasLeft)
	latestBefore := middleWidth - centerWidth - getGap(hasRight)
	if centerWidth == 0 || latestBefore < earliestBefore {
		return head + renderGap(middleWidth, hasLeft, hasRight, rule) + tail
	}

	before := min(max((width-centerWidth)/2-leftWidth, earliestBefore), latestBefore)
	after := middleWidth - centerWidth - before

	return head + renderGap(before, hasLeft, true, rule) + self.Center + renderGap(after, true, hasRight, rule) + tail
}

func renderGap(cells int, hasLabelBefore bool, hasLabelAfter bool, rule style.Style) string {
	if cells <= 0 {
		return ""
	}

	openingSpace, closingSpace := "", ""
	if hasLabelBefore {
		openingSpace = " "
		cells--
	}
	if hasLabelAfter && cells > 0 {
		closingSpace = " "
		cells--
	}

	return openingSpace + rule(strings.Repeat("─", cells)) + closingSpace
}
