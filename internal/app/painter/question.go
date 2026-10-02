package painter

import (
	"strings"
	"time"
	"unicode"

	"crdx.org/oh/internal/app/call"
	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/markdown"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/width"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/ask"
)

const (
	detailGutter  = "❯"
	optionGap     = " "
	lapseFallback = "auto-cancels"
	shellLanguage = "bash"
)

func RenderQuestion(
	question ask.Question,
	cursor int,
	columns int,
	shouldRenderHyperlinks bool,
	pathRoots link.Roots,
) []string {
	rows := renderQuestionHead(question, columns)
	details := renderQuestionDetail(question, columns)
	details = append(details, renderQuestionFields(question.Fields, columns, shouldRenderHyperlinks, pathRoots)...)

	if len(details) > 0 {
		rows = append(rows, "")
		rows = append(rows, details...)
	}

	return append(rows, "", renderOptions(question, cursor, columns))
}

func QuestionPinnedRows(question ask.Question, columns int) int {
	headRows := len(renderQuestionHead(question, columns))
	if question.Detail == "" && len(question.Fields) == 0 {
		return headRows
	}

	return headRows + 1
}

func QuestionYieldingRows(question ask.Question, columns int) (int, int) {
	return len(renderQuestionLabel(question.Label, columns)), len(renderQuestionIntent(question.Intent, columns))
}

func renderQuestionHead(question ask.Question, columns int) []string {
	rows := renderQuestionLabel(question.Label, columns)
	return append(rows, renderQuestionIntent(question.Intent, columns)...)
}

func renderQuestionIntent(intent string, columns int) []string {
	intent = strings.Join(strings.Fields(strutil.StripControl(intent)), " ")
	if intent == "" {
		return nil
	}

	return width.Wrap(intent, columns)
}

func QuestionHead(question ask.Question, remainingTime time.Duration) string {
	return renderCountdown(question.Lapse, remainingTime)
}

func renderQuestionLabel(label string, columns int) []string {
	labelStyle := NoticeStyle(agent.WarningStatus)
	rows := width.Wrap(label, columns)

	for index, row := range rows {
		rows[index] = labelStyle.Over(row)
	}

	return rows
}

func renderQuestionDetail(question ask.Question, columns int) []string {
	if question.Detail == "" {
		return nil
	}

	detail := highlightedDetail(question)

	mark, markStyle := detailMark(question.Language)
	indent := strings.Repeat(" ", style.Width(mark)+1)
	gutter := markStyle(mark) + " "
	room := max(columns-style.Width(indent), 1)

	var rows []string
	for line := range strings.SplitSeq(detail, "\n") {
		for _, row := range width.Wrap(line, room) {
			rows = append(rows, gutter+row)
			gutter = indent
		}
	}

	return rows
}

func renderQuestionFields(
	fields []ask.Field,
	columns int,
	shouldRenderHyperlinks bool,
	pathRoots link.Roots,
) []string {
	var rows []string

	for index, field := range fields {
		if field.IsSubject && len(rows) > 0 {
			rows = append(rows, "")
		}

		value := strutil.StripControl(field.Value)
		if shouldRenderHyperlinks {
			value = link.Render(value, pathRoots)
		}
		if field.IsSubject {
			value = style.Info.Over(value)
		}

		name := strutil.StripControl(field.Name)
		prefix := style.Subject(name+":") + " "
		indent := min(style.Width(prefix), max(columns-1, 0))
		rows = append(rows, width.WrapIndented(prefix+value, columns, indent)...)
		if field.IsSubject && index < len(fields)-1 {
			rows = append(rows, "")
		}
	}

	return rows
}

func highlightedDetail(question ask.Question) string {
	switch question.Language {
	case "":
		return question.Detail
	case shellLanguage:
		return markdown.HighlightURLs(question.Detail)
	default:
		return markdown.Highlight(question.Detail, question.Detail, question.Language, false)
	}
}

func detailMark(language string) (string, style.Style) {
	if mark, markStyle := call.ToolMark(language); mark != "" {
		return mark, markStyle
	}

	return detailGutter, style.Subject
}

func renderOptions(question ask.Question, cursor int, columns int) string {
	labels := make([]string, 0, len(question.Options))

	for index, option := range question.Options {
		labels = append(labels, renderOption(option, index == cursor))
	}

	return width.Elide(strings.Join(labels, optionGap), columns)
}

func renderOption(option ask.Option, isChosen bool) string {
	label := labelledOption(option)

	if isChosen {
		return style.ChosenRow("[" + label + "]")
	}

	return style.Subtle(" " + label + " ")
}

func labelledOption(option ask.Option) string {
	if option.Key == 0 || isKeyLeading(option) {
		return option.Label
	}

	return string(option.Key) + " " + option.Label
}

func isKeyLeading(option ask.Option) bool {
	letters := []rune(option.Label)

	return len(letters) > 0 && unicode.ToLower(letters[0]) == unicode.ToLower(option.Key)
}

func renderCountdown(lapse string, remainingTime time.Duration) string {
	if remainingTime <= 0 {
		return ""
	}

	if lapse == "" {
		lapse = lapseFallback
	}

	countdown := (remainingTime + time.Second - 1).Truncate(time.Second)

	return style.Subtle(lapse + " in " + util.CompactDuration(countdown))
}
