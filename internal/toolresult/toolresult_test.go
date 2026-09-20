package toolresult_test

import (
	"strings"
	"testing"

	"crdx.org/io/internal/toolresult"
	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/session"
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
