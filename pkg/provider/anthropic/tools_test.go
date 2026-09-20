package anthropic_test

import (
	"encoding/json"
	"strings"
	"testing"

	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/provider/anthropic"
	"crdx.org/io/pkg/tool"
)

func TestAToolIsOfferedWithAnInputSchema(t *testing.T) {
	server, bodies := turns(t, script(answer("Hello.")))

	var callCount int
	assistant := newAgent(t, server.URL, []tool.Tool{weatherTool(t, &callCount)})

	if _, err := assistant.Send(t.Context(), "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	toolsJSON := `"tools":[{"name":"weather","description":"report weather in a city",` +
		`"cache_control":{"type":"ephemeral"},` +
		`"input_schema":{"type":"object","properties":{"city":` +
		`{"type":"string","description":"the city to look up"}},` +
		`"required":["city"],"additionalProperties":false}}]`

	if !strings.Contains((*bodies)[0], toolsJSON) {
		t.Errorf("expected %s, got %s", toolsJSON, (*bodies)[0])
	}
}

func TestAToolWithNoArgumentsIsStillGivenASchema(t *testing.T) {
	server, bodies := turns(t, script(answer("Hello.")))

	assistant := newAgent(t, server.URL, []tool.Tool{emptyTool("wait")})

	if _, err := assistant.Send(t.Context(), "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	schemaJSON := `"input_schema":{"type":"object","properties":{},"additionalProperties":false}`

	if !strings.Contains((*bodies)[0], schemaJSON) {
		t.Errorf("expected %s, got %s", schemaJSON, (*bodies)[0])
	}
}

func TestOnlyTheLastToolEndsACacheablePrefix(t *testing.T) {
	server, bodies := turns(t, script(answer("Hello.")))

	var callCount int
	tools := []tool.Tool{emptyTool("wait"), weatherTool(t, &callCount)}
	assistant := newAgent(t, server.URL, tools)

	if _, err := assistant.Send(t.Context(), "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var request struct {
		Tools []struct {
			Name  string          `json:"name"`
			Cache json.RawMessage `json:"cache_control"`
		} `json:"tools"`
	}
	if err := json.Unmarshal([]byte((*bodies)[0]), &request); err != nil {
		t.Fatal(err)
	}

	if len(request.Tools) != 2 {
		t.Fatalf("expected both tools to be offered, got %+v", request.Tools)
	}

	if request.Tools[0].Cache != nil {
		t.Errorf("expected no breakpoint on the first tool, got %s", request.Tools[0].Cache)
	}

	if string(request.Tools[1].Cache) != `{"type":"ephemeral"}` {
		t.Errorf("expected the last tool to end the prefix, got %s", request.Tools[1].Cache)
	}
}

func TestToolsSizeIsTheEncodedLength(t *testing.T) {
	var callCount int
	tools := []tool.Tool{weatherTool(t, &callCount), emptyTool("wait")}

	size := anthropic.ToolsSize(tools)
	if size <= 0 {
		t.Fatalf("expected the tools to occupy some bytes, got %d", size)
	}

	server, bodies := turns(t, script(answer("Hello.")))
	assistant := newAgent(t, server.URL, tools)

	if _, err := assistant.Send(t.Context(), "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var request struct {
		Tools json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal([]byte((*bodies)[0]), &request); err != nil {
		t.Fatal(err)
	}

	if len(request.Tools) != size {
		t.Errorf("expected %d bytes to match what was sent, got %d", size, len(request.Tools))
	}
}

func TestEveryEffortLevelTheModelTakesIsOffered(t *testing.T) {
	for _, effort := range anthropic.Efforts {
		t.Run(effort, func(t *testing.T) {
			server, bodies := turns(t, script(answer("Hello.")))

			client := newClient(t, server.URL)
			client.Effort = effort
			client.AddUserMessage("hello")

			if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			want := `"output_config":{"effort":"` + effort + `"}`
			if !strings.Contains((*bodies)[0], want) {
				t.Errorf("expected %s, got %s", want, (*bodies)[0])
			}
		})
	}
}
