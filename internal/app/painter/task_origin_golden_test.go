package painter

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/experimental"
	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/taskrecord"
	"crdx.org/oh/pkg/tasks"
)

func TestGoldenTaskCompletionsShowTheirOriginatingTool(t *testing.T) {
	startedAt := time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)
	var sections []string
	for _, test := range []struct {
		name     string
		toolName string
	}{
		{name: "check-1", toolName: "offload"},
		{name: "check-1", toolName: "bash"},
		{name: "check-1", toolName: "find"},
		{name: "check-1"},
		{name: "offload-1", toolName: "offload"},
		{name: "bash-2", toolName: "bash"},
	} {
		toolName := test.toolName
		event := taskrecord.EndedEvent(tasks.Conclusion{Snapshot: tasks.Snapshot{
			ID: test.name, ToolName: toolName, Run: 1, State: tasks.Done,
			StartedAt: startedAt, EndedAt: startedAt.Add(14 * time.Second),
			Invocation: &tasks.Invocation{Arguments: `{"intent":"Rechecking offline command contracts"}`},
			Output:     "offload output stays unchanged\n",
		}, IsFinal: true})
		for _, columns := range []int{120, 60, 30, 16} {
			for _, isPending := range []bool{false, true} {
				notices, _ := DrawnNotices(event, NoticeDrawing{
					Introductions: NewIntroductions(),
					Toggles:       experimental.New(map[string]any{string(experimental.TaskOutputLines): int64(1)}),
					SessionName:   "tame-impala",
				})
				pending := NewPendingMessages(notices, true, link.Roots{})
				if !isPending {
					pending.MarkSent()
				}
				rows := pending.Rows(columns)
				for _, row := range rows {
					if style.Width(row) > columns {
						t.Errorf("tool %q has a row wider than %d columns: %q", toolName, columns, style.Plain(row))
					}
				}
				title := toolName
				if test.name != "check-1" {
					title = test.name + " / " + toolName
				}
				sections = append(sections, fmt.Sprintf("── %q · %d columns · pending %t\n%s\n", title, columns, isPending, strings.Join(rows, "\n")))
			}
		}
	}
	drawn := strings.Join(sections, "\n")
	compareCallRowGolden(t, "task-origin.ansi", drawn)
	compareCallRowGolden(t, "task-origin.txt", style.Plain(drawn))
}

func TestATaskOriginUsesGoldWithoutColouringItsParenthesesOrOutput(t *testing.T) {
	event := taskrecord.EndedEvent(tasks.Conclusion{Snapshot: tasks.Snapshot{
		ID: "offload", ToolName: "offload", Run: 1, State: tasks.Done, Output: "offload",
	}, IsFinal: true})
	notices, _ := DrawnNotices(event, NoticeDrawing{
		Introductions: NewIntroductions(),
		Toggles:       experimental.New(map[string]any{string(experimental.TaskOutputLines): int64(1)}),
	})
	rows := submittedContentRows(submittedMessage{
		text: notices[0].Text, kind: pendingHarnessSubmission, toolName: notices[0].ToolName,
	}, 120, false, link.Roots{})
	if !strings.Contains(rows[0], "("+style.Warning("offload")+")") {
		t.Errorf("the origin does not use gold within normal parentheses: %q", rows[0])
	}
	if !strings.Contains(rows[0], style.Code("offload")) {
		t.Errorf("the task name lost its code style: %q", rows[0])
	}
	if strings.Count(strings.Join(rows, "\n"), style.Warning("offload")) != 1 {
		t.Errorf("the origin style reached another part of the notice: %q", rows)
	}
}
