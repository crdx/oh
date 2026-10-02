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

func drawnJobCalls(t *testing.T, introductions *Introductions, events ...agent.Event) string {
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

func jobStart(id string, name string, intent string) agent.Event {
	event := agent.Event{Kind: agent.ToolCallRequestEvent, ID: id, Name: "job"}
	event.RenderingKind = "job_start"
	event.Subject = name
	event.Intent = intent
	event.Introduces = name
	return event
}

func jobCall(id string, kind string, names ...string) agent.Event {
	event := agent.Event{Kind: agent.ToolCallRequestEvent, ID: id, Name: "job"}
	event.RenderingKind = kind
	event.Subject = strings.Join(names, " || ")
	event.Mentions = names
	return event
}

func settledJob(id string) agent.Event {
	return agent.Event{Kind: agent.ToolCallResultEvent, ID: id, Name: "job", Status: agent.SuccessStatus}
}

func TestACallNamingAJobIsRemindedOfItsIntent(t *testing.T) {
	painted := drawnJobCalls(t, nil,
		jobStart("1", "docs", "Serving the documentation locally"), settledJob("1"),
		jobStart("2", "build", "Building every package"), settledJob("2"),
		jobCall("3", "job_status", "docs"), settledJob("3"),
		jobCall("4", "job_wait_any", "docs", "build"), settledJob("4"),
		jobCall("5", "job_status", "other"), settledJob("5"),
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
		t.Errorf("drew %q, want no reminder for a job nobody started", drawn)
	}
}

func TestAReminderComesFromTheStartBeforeIt(t *testing.T) {
	drawn := style.Plain(drawnJobCalls(t, nil,
		jobStart("1", "docs", "Serving the documentation locally"), settledJob("1"),
		jobCall("2", "job_status", "docs"), settledJob("2"),
		jobStart("3", "docs", "Serving the docs on a new port"), settledJob("3"),
		jobCall("4", "job_stop", "docs"), settledJob("4"),
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

func TestAJobStartedInAnEarlierTurnIsStillRemembered(t *testing.T) {
	introductions := NewIntroductions()
	drawnJobCalls(t, introductions, jobStart("1", "docs", "Serving the documentation locally"), settledJob("1"))

	drawn := style.Plain(drawnJobCalls(t, introductions, jobCall("2", "job_status", "docs"), settledJob("2")))
	if !strings.Contains(drawn, "status docs (Serving the documentation locally)") {
		t.Errorf("drew %q, want the earlier turn's intent", drawn)
	}

	introductions.Forget()
	if drawn := style.Plain(drawnJobCalls(t, introductions, jobCall("3", "job_status", "docs"), settledJob("3"))); strings.Contains(drawn, "(") {
		t.Errorf("drew %q after forgetting, want no reminder", drawn)
	}
}
