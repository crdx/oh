package toolresult_test

import (
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/toolresult"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
)

func TestURLRoundTripsOpaqueValues(t *testing.T) {
	address := toolresult.URL("tame-impala", "call/a?b&c")
	reference, err := toolresult.Parse(address)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if reference.SessionName != "tame-impala" || reference.CallID != "call/a?b&c" {
		t.Errorf("got reference %#v", reference)
	}
}

func TestParseRefusesOtherAddresses(t *testing.T) {
	for _, address := range []string{
		"https://example.test/",
		"oh://tool-result/elsewhere?session=tame-impala&call=one",
		"oh://tool-result?session=tame-impala",
		"oh://tool-result?session=tame-impala&call=one&extra=two",
	} {
		if _, err := toolresult.Parse(address); err == nil {
			t.Errorf("expected %q to be refused", address)
		}
	}
}

func TestATaskURLRoundTripsItsRun(t *testing.T) {
	want := toolresult.TaskReference{
		EndedAt:     time.Date(2026, 10, 24, 12, 0, 0, 123456789, time.UTC),
		SessionName: "tame-impala",
		TaskName:    "build&test",
		Run:         2,
	}
	address := toolresult.TaskURL(want)
	if !toolresult.IsTask(address) || toolresult.IsTask(toolresult.URL("tame-impala", "call-1")) {
		t.Fatalf("%q was not told apart from a tool result", address)
	}
	got, err := toolresult.ParseTask(address)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if !got.EndedAt.Equal(want.EndedAt) || got.SessionName != want.SessionName || got.TaskName != want.TaskName || got.Run != want.Run {
		t.Errorf("got reference %#v, want %#v", got, want)
	}
}

func TestATaskURLLeavesOutAnUnknownEndTime(t *testing.T) {
	address := toolresult.TaskURL(toolresult.TaskReference{SessionName: "tame-impala", TaskName: "build", Run: 1})
	if want := "oh://task-result?run=1&session=tame-impala&task=build"; address != want {
		t.Errorf("got %q, want %q", address, want)
	}
	got, err := toolresult.ParseTask(address)
	if err != nil || !got.EndedAt.IsZero() || got.TaskName != "build" || got.Run != 1 {
		t.Errorf("got %#v and %v", got, err)
	}
}

func TestParseTaskRefusesOtherAddresses(t *testing.T) {
	for _, address := range []string{
		"oh://tool-result?session=tame-impala&call=one",
		"oh://task-result?session=tame-impala&task=build",
		"oh://task-result?session=tame-impala&task=build&run=1&ended=",
		"oh://task-result?session=tame-impala&task=build&run=0&ended=1",
		"oh://task-result?session=tame-impala&task=build&run=one&ended=1",
		"oh://task-result?session=tame-impala&task=build&run=1&ended=soon",
		"oh://task-result?session=tame-impala&task=build&run=1&ended=1&extra=two",
	} {
		if _, err := toolresult.ParseTask(address); err == nil {
			t.Errorf("expected %q to be refused", address)
		}
	}
}

func TestASubagentURLRoundTripsItsConversation(t *testing.T) {
	want := toolresult.SubagentReference{SessionName: "tame-impala", SubagentName: "tame-adder"}
	address := toolresult.SubagentURL(want)
	if !toolresult.IsSubagent(address) || toolresult.IsSubagent(toolresult.URL("tame-impala", "call-1")) {
		t.Fatalf("%q was not told apart from a tool result", address)
	}
	got, err := toolresult.ParseSubagent(address)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if got != want {
		t.Errorf("got reference %#v, want %#v", got, want)
	}
}

func TestParseSubagentRefusesOtherAddresses(t *testing.T) {
	for _, address := range []string{
		"oh://tool-result?session=tame-impala&call=one",
		"oh://subagent-conversation?session=tame-impala",
		"oh://subagent-conversation?session=tame-impala&subagent=",
		"oh://subagent-conversation?session=tame-impala&subagent=tame-adder&extra=two",
	} {
		if _, err := toolresult.ParseSubagent(address); err == nil {
			t.Errorf("expected %q to be refused", address)
		}
	}
}

func TestReadFindsTheSelectedToolResult(t *testing.T) {
	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{Kind: agent.ToolCallRequestEvent, ID: "wanted", Name: "edit", Arguments: `{}`}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{Kind: agent.ToolCallResultEvent, ID: "wanted", Name: "edit", Text: "the output\n"}); err != nil {
		t.Fatal(err)
	}
	name := writer.Name()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	exchange, err := toolresult.Read(directory, toolresult.URL(name, "wanted"))
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if exchange.Request.Name != "edit" || exchange.Request.Arguments != `{}` {
		t.Errorf("got request %#v", exchange.Request)
	}
	if exchange.Result.Name != "edit" || exchange.Result.Text != "the output\n" {
		t.Errorf("got result %#v", exchange.Result)
	}
}

func TestReadReportsAMissingResult(t *testing.T) {
	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	name := writer.Name()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = toolresult.Read(directory, toolresult.URL(name, "missing"))
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("got error %v", err)
	}
}
