package chatcompletions_test

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/tool"
	"crdx.org/io/pkg/wire/openai/chatcompletions"
)

type weatherArguments struct {
	City string `json:"city"`
}

func weather() tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "weather",
			Description: "report weather in a city",
			Schema:      tool.Schema{tool.String("city", "the city to look up")},
		},
		func(arguments weatherArguments) (string, string) { return arguments.City, "" },
	).Plain(func(context.Context, weatherArguments) (string, error) { return "raining", nil })
}

func TestToolsSizeIsTheNumberOfBytesSentOnTheWire(t *testing.T) {
	var bodies []string
	server := scriptedServer(t, &bodies,
		`{"choices":[{"delta":{"content":"Hello."}}]}`,
		"[DONE]",
	)

	offered := []tool.Tool{weather()}
	client := newClient(t, server.URL)
	client.Configure("", []tool.Definition{tool.Describe(offered[0])})
	client.AddUserMessage("hello")
	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatal(err)
	}

	var request struct {
		Tools json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal([]byte(bodies[0]), &request); err != nil {
		t.Fatal(err)
	}

	if got, want := chatcompletions.ToolsSize(offered), len(request.Tools); got != want || got == 0 {
		t.Errorf("ToolsSize = %d, encoded tools = %d", got, want)
	}
}

func TestEveryToolResultShapeHasAStableWireRepresentation(t *testing.T) {
	client := newClient(t, "http://somewhere")
	client.AddToolResults([]agent.ToolCallResult{
		{ID: "empty"},
		{ID: "image", Image: tool.Image{MediaType: "image/png", Data: []byte{1, 2, 3}}},
		{ID: "error", Output: "failed", IsError: true},
	})

	items := client.Dump()
	if len(items) != 4 {
		t.Fatalf("got %d history items: %s", len(items), items)
	}

	type wireMessage struct {
		Role       string          `json:"role"`
		Content    json.RawMessage `json:"content"`
		ToolCallID string          `json:"tool_call_id"`
	}

	var messages []wireMessage
	for _, item := range items {
		var message wireMessage
		if err := json.Unmarshal(item, &message); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, message)
	}

	for index, want := range []struct {
		role       string
		toolCallID string
		content    string
	}{
		{role: "tool", toolCallID: "empty", content: "No output."},
		{role: "tool", toolCallID: "image", content: "No output."},
		{role: "tool", toolCallID: "error", content: "failed"},
	} {
		got := messages[index]
		if got.Role != want.role || got.ToolCallID != want.toolCallID || string(got.Content) != strconv.Quote(want.content) {
			t.Errorf("message %d = %+v, want %+v", index, got, want)
		}
	}

	attachment := messages[3]
	if attachment.Role != "user" || attachment.ToolCallID != "" {
		t.Errorf("got attachment message %+v", attachment)
	}
	wantAttachment := `[{"type":"text","text":"Image attached."},{"type":"image_url","image_url":{"url":"data:image/png;base64,AQID"}}]`
	if string(attachment.Content) != wantAttachment {
		t.Errorf("got attachment %s, want %s", attachment.Content, wantAttachment)
	}
}
