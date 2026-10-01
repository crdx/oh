package painter

import (
	"strings"
	"time"

	"crdx.org/oh/internal/app/call"
	"crdx.org/oh/internal/app/dynamic"
	"crdx.org/oh/internal/app/hostcommand"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/width"
	"crdx.org/oh/pkg/toolbox/bash"
)

const (
	hostCommandStopHint     = "esc to stop"
	hostCommandOutputIndent = "  "
)

func RenderHostCommand(command string, latestLine string, elapsedTime time.Duration, columns int) []string {
	rows := []string{runningHostCommand(command, elapsedTime, columns)}

	room := columns - width.Of(hostCommandOutputIndent)
	if latestLine == "" || room < 1 {
		return rows
	}

	return append(rows, hostCommandOutputIndent+style.Subtle(width.Elide(latestLine, room)))
}

func runningHostCommand(command string, elapsedTime time.Duration, columns int) string {
	label := call.LabelForRendering(bash.DescribeCommand(command))
	line := dynamic.RunningLine(label, elapsedTime, hostcommand.TimeLimit, 0)

	gap := columns - style.Width(line) - style.Width(hostCommandStopHint)
	if gap < 1 {
		return dynamic.RunningLine(label, elapsedTime, hostcommand.TimeLimit, columns)
	}

	return line + strings.Repeat(" ", gap) + style.Subtle(hostCommandStopHint)
}
