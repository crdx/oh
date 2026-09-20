package notification

import (
	"context"
	"strings"

	"crdx.org/io/internal/app/width"
	"crdx.org/io/internal/app/work"
	"crdx.org/io/pkg/ask"
	"crdx.org/io/pkg/toolbox/notify"
)

const detailWidth = 80

func SendTurnError(
	ctx context.Context,
	writeEscape notify.EscapeWriter,
	workspace *work.Space,
	failure error,
) error {
	return notify.Send(ctx, writeEscape, notify.Args{
		Title:   title(workspace),
		Message: failure.Error(),
		Icon:    "error",
	})
}

func SendQuestion(
	ctx context.Context,
	writeEscape notify.EscapeWriter,
	workspace *work.Space,
	question ask.Question,
) error {
	return notify.Send(ctx, writeEscape, notify.Args{
		Title:   title(workspace),
		Message: questionMessage(question),
		Icon:    "question",
	})
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
