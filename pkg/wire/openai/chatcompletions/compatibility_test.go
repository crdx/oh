package chatcompletions_test

import (
	"testing"

	"crdx.org/oh/pkg/agent"
)

func TestSendReportsARequestThatCouldNotBeOpened(t *testing.T) {
	client := newClient(t, "http://127.0.0.1:1/v1/chat/completions")
	client.AddUserMessage("hello")

	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err == nil {
		t.Fatal("expected the unreachable endpoint to be reported")
	}
}

func TestEveryStreamEventCanStopDelivery(t *testing.T) {
	toolCall := `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"add","arguments":"{}"}}]}}]}`

	for _, test := range []struct {
		name     string
		payloads []string
		stopAt   int
	}{
		{
			name:     "reasoning delta",
			payloads: []string{`{"choices":[{"delta":{"reasoning":"think"}}]}`},
			stopAt:   1,
		},
		{
			name: "reasoning completion before content",
			payloads: []string{
				`{"choices":[{"delta":{"reasoning":"think"}}]}`,
				`{"choices":[{"delta":{"content":"answer"}}]}`,
			},
			stopAt: 2,
		},
		{
			name:     "content delta",
			payloads: []string{`{"choices":[{"delta":{"content":"answer"}}]}`},
			stopAt:   1,
		},
		{
			name: "reasoning completion before tool calls",
			payloads: []string{
				`{"choices":[{"delta":{"reasoning":"think"}}]}`,
				toolCall,
			},
			stopAt: 2,
		},
		{
			name: "reasoning completion at done",
			payloads: []string{
				`{"choices":[{"delta":{"reasoning":"think"}}]}`,
				"[DONE]",
			},
			stopAt: 2,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var bodies []string
			server := scriptedServer(t, &bodies, test.payloads...)
			client := newClient(t, server.URL)
			client.AddUserMessage("hello")

			deliveries := 0
			if _, err := client.Send(t.Context(), func(agent.Output) bool {
				deliveries++
				return deliveries != test.stopAt
			}); err != nil {
				t.Fatal(err)
			}
			if deliveries != test.stopAt {
				t.Errorf("got %d deliveries, want %d", deliveries, test.stopAt)
			}
		})
	}
}

func TestCompatibleReasoningAndCompleteToolCallsAreStreamed(t *testing.T) {
	var bodies []string
	server := scriptedServer(
		t,
		&bodies,
		`{"choices":[{"delta":{"reasoning":"Use the tool."}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-ollama","type":"function","function":{"name":"add","arguments":"{\"left\":2,\"right\":3}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":321,"completion_tokens":20,"total_tokens":341}}`,
		"[DONE]",
	)

	client := newClient(t, server.URL)
	observer := &countingObserver{}
	client.ObserveHTTP(observer)
	client.AddUserMessage("add two and three")

	var outputs []agent.Output
	reply, err := client.Send(t.Context(), func(output agent.Output) bool {
		outputs = append(outputs, output)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}

	if observer.requests != 1 {
		t.Errorf("observed %d conversation requests", observer.requests)
	}
	if len(outputs) != 2 || outputs[0].Kind != agent.ModelReasoningEvent || outputs[0].Text != "Use the tool." ||
		outputs[1].Kind != agent.ModelReasoningEvent || !outputs[1].Done {
		t.Errorf("got outputs %#v", outputs)
	}
	wantCall := agent.ToolCall{
		ID:        "call-ollama",
		Name:      "add",
		Arguments: `{"left":2,"right":3}`,
	}
	if len(reply.Calls) != 1 || reply.Calls[0] != wantCall {
		t.Errorf("got calls %+v", reply.Calls)
	}
	if reply.Usage.InputTokens != 321 {
		t.Errorf("got usage %+v", reply.Usage)
	}
}
