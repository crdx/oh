package painter

import (
	"bytes"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/dynamic"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/pkg/agent"
)

func drawnTaskCalls(t *testing.T, introductions *Introductions, events ...agent.Event) string {
	t.Helper()

	var screenOutput bytes.Buffer
	screen := output.NewTerminalOfSize(&screenOutput, 100, 24).AppendOnly()
	paint := New(screen, false, nil, nil, output.StreamingModeLine)
	if introductions != nil {
		paint.RememberIntroductionsIn(introductions)
	}
	for _, event := range events {
		paint.DrawEvent(event)
	}
	paint.Close(dynamic.Cancelled)
	screen.End()

	return screenOutput.String()
}

func taskStart(id string, name string, intent string) agent.Event {
	event := agent.Event{Kind: agent.ToolCallRequestEvent, ID: id, Name: "bash"}
	event.RenderingKind = "bash"
	event.Subject = "just " + name
	event.Intent = intent
	event.Introduces = name
	return event
}

func taskCall(id string, kind string, names ...string) agent.Event {
	event := agent.Event{Kind: agent.ToolCallRequestEvent, ID: id, Name: "task"}
	event.RenderingKind = kind
	event.Subject = strings.Join(names, " || ")
	event.Mentions = names
	return event
}

func settledCall(id string) agent.Event {
	return agent.Event{Kind: agent.ToolCallResultEvent, ID: id, Name: "task", Status: agent.SuccessStatus}
}

func TestACallNamingATaskIsRemindedOfItsIntent(t *testing.T) {
	painted := drawnTaskCalls(t, nil,
		taskStart("1", "docs", "Serving the documentation locally"), settledCall("1"),
		taskStart("2", "build", "Building every package"), settledCall("2"),
		taskCall("3", "task_status", "docs"), settledCall("3"),
		taskCall("4", "task_status", "docs", "build"), settledCall("4"),
		taskCall("5", "task_status", "other"), settledCall("5"),
	)
	drawn := style.Plain(painted)

	if !strings.Contains(painted, style.Reasoning("(Serving the documentation locally)")) {
		t.Errorf("painted %q, want the reminder drawn like reasoning", painted)
	}

	for _, want := range []string{
		"status docs (Serving the documentation locally)",
		"docs || build (Serving the documentation locally; Building every package)",
	} {
		if !strings.Contains(drawn, want) {
			t.Errorf("drew %q, want it to hold %q", drawn, want)
		}
	}
	if strings.Contains(drawn, "other (") {
		t.Errorf("drew %q, want no reminder for a task nobody started", drawn)
	}
}

func TestAReminderComesFromTheStartBeforeIt(t *testing.T) {
	drawn := style.Plain(drawnTaskCalls(t, nil,
		taskStart("1", "docs", "Serving the documentation locally"), settledCall("1"),
		taskCall("2", "task_status", "docs"), settledCall("2"),
		taskStart("3", "docs", "Serving the docs on a new port"), settledCall("3"),
		taskCall("4", "task_stop", "docs"), settledCall("4"),
	))

	for _, want := range []string{
		"status docs (Serving the documentation locally)",
		"stop docs (Serving the docs on a new port)",
	} {
		if !strings.Contains(drawn, want) {
			t.Errorf("drew %q, want it to hold %q", drawn, want)
		}
	}
}

func TestATaskStartedInAnEarlierTurnIsStillRemembered(t *testing.T) {
	introductions := NewIntroductions()
	drawnTaskCalls(t, introductions, taskStart("1", "docs", "Serving the documentation locally"), settledCall("1"))

	drawn := style.Plain(drawnTaskCalls(t, introductions, taskCall("2", "task_status", "docs"), settledCall("2")))
	if !strings.Contains(drawn, "status docs (Serving the documentation locally)") {
		t.Errorf("drew %q, want the earlier turn's intent", drawn)
	}

	introductions.Forget()
	if drawn := style.Plain(drawnTaskCalls(t, introductions, taskCall("3", "task_status", "docs"), settledCall("3"))); strings.Contains(drawn, "(") {
		t.Errorf("drew %q after forgetting, want no reminder", drawn)
	}
}
