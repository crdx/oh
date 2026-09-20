package input

import (
	"strings"

	"crdx.org/io/internal/app/edit"
	"crdx.org/io/internal/app/style"
)

const (
	edgePad                    = 2
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
	rightWidth := getWidth(right, edgePad)
	if rightWidth > width {
		rightWidth = 0
	}
	return max(width-rightWidth-edgePad-2, 0)
}

type Block struct {
	Top           Ruler
	Input         edit.Frame
	Bottom        Ruler
	Status        []string
	FrameFeedback bool
	Question      []string
	Rule          style.Style
}

func (self Block) Rows(width int) ([]string, int, int) {
	rows := make([]string, 0, len(self.Status)+len(self.Input.Rows)+4)

	top := self.Top
	if self.Input.IsSearching && !self.isAsking() {
		top.Left = style.Subtle("reverse-i-search: " + self.Input.SearchQuery)
	}

	bottom := self.Bottom
	if getWidth(bottom.Left, edgePad)+getWidth(bottom.Right, edgePad) > width {
		bottom.Right = ""
	}

	body, bodyRow, bodyColumn := self.body()
	rule := self.rule()
	statusRows, topWidth := self.renderStatus(width, rule)

	rows = append(rows, statusRows...)
	topRule := top.render(topWidth, rule)
	if topWidth != width {
		reach := strings.Repeat("─", feedbackInset)
		topRule = rule(reach+"┴") + topRule + rule("┴"+reach)
	}
	rows = append(rows, topRule)
	rows = append(rows, body...)
	rows = append(rows, bottom.render(width, rule))

	return rows, len(statusRows) + bodyRow + 1, bodyColumn
}

func (self Block) renderStatus(width int, rule style.Style) ([]string, int) {
	if !self.FrameFeedback || width < MinimumFramedFeedbackWidth {
		return self.Status, width
	}

	contentWidth := FeedbackContentWidth(width)
	inset := strings.Repeat(" ", feedbackInset)
	rows := make([]string, 0, len(self.Status)+1)
	rows = append(rows, inset+rule("╭"+strings.Repeat("─", FeedbackRuleWidth(width))+"╮"))
	for _, status := range self.Status {
		padding := strings.Repeat(" ", max(contentWidth-style.Width(status), 0))
		rows = append(rows, inset+rule("│")+" "+status+padding+" "+rule("│"))
	}

	return rows, FeedbackRuleWidth(width)
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
	leftWidth := getWidth(self.Left, edgePad)
	rightWidth := getWidth(self.Right, edgePad)

	head := ""
	if leftWidth == 0 || leftWidth+rightWidth > width {
		leftWidth = 0
	} else {
		head = rule(strings.Repeat("─", edgePad)) + " " + self.Left + " "
	}

	tail := ""
	if rightWidth == 0 || leftWidth+rightWidth > width {
		rightWidth = 0
	} else {
		tail = " " + self.Right + " " + rule(strings.Repeat("─", edgePad))
	}

	middleWidth := max(width-leftWidth-rightWidth, 0)

	return head + renderCentredSpan(middleWidth, self.Center, leftWidth, width, rule) + tail
}

func renderCentredSpan(availableWidth int, center string, startColumn int, ruleWidth int, rule style.Style) string {
	centerWidth := getWidth(center, 0)
	beforeWidth := (ruleWidth-centerWidth)/2 - startColumn
	if centerWidth == 0 || beforeWidth < 0 || beforeWidth+centerWidth > availableWidth {
		return rule(strings.Repeat("─", availableWidth))
	}

	afterWidth := availableWidth - centerWidth - beforeWidth
	leadingGap := " "
	trailingGap := " "

	if beforeWidth == 0 && startColumn > 0 {
		leadingGap = ""
		afterWidth++
	}
	if afterWidth == 0 && startColumn+availableWidth < ruleWidth {
		trailingGap = ""
		beforeWidth++
	}

	before := rule(strings.Repeat("─", beforeWidth))
	after := rule(strings.Repeat("─", afterWidth))

	return before + leadingGap + center + trailingGap + after
}

func getWidth(str string, edgePadding int) int {
	cells := style.Width(str)
	if cells == 0 {
		return 0
	}

	return cells + edgePadding + 2
}
