package notification

import (
	"context"
	"strings"

	"crdx.org/oh/internal/app/width"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/pkg/ask"
	"crdx.org/oh/pkg/toolbox/notify"
)

const detailWidth = 80

func SendTurnError(
	ctx context.Context,
	writeEscape notify.EscapeWriter,
	isTerminalFocused func() bool,
	workspace *work.Space,
	failure error,
) error {
	_, err := notify.SendIfUnfocused(ctx, writeEscape, isTerminalFocused, notify.Args{
		Title:   title(workspace),
		Message: failure.Error(),
		Icon:    "error",
	})
	return err
}

func SendQuestion(
	ctx context.Context,
	writeEscape notify.EscapeWriter,
	isTerminalFocused func() bool,
	workspace *work.Space,
	question ask.Question,
) error {
	_, err := notify.SendIfUnfocused(ctx, writeEscape, isTerminalFocused, notify.Args{
		Title:   title(workspace),
		Message: questionMessage(question),
		Icon:    "question",
	})
	return err
}

func title(workspace *work.Space) string {
	return "oh — " + workspace.GetName()
}

func questionMessage(question ask.Question) string {
	detail := detailLine(question.Detail)
	if detail == "" {
		return question.Label
	}

	return question.Label + "\n" + detail
}

func detailLine(detail string) string {
	line, rest, _ := strings.Cut(strings.TrimSpace(detail), "\n")
	if strings.TrimSpace(rest) != "" {
		line += " " + width.Ellipsis
	}

	return width.Elide(line, detailWidth)
}
