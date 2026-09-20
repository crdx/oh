package codex

import (
	"encoding/json"
	"testing"

	"crdx.org/io/pkg/tool"
)

type sizedTool struct{}

func (sizedTool) Name() string                        { return "read" }
func (sizedTool) Description() string                 { return "read a file" }
func (sizedTool) Schema() tool.Schema                 { return tool.Schema{} }
func (sizedTool) Concurrent() bool                    { return true }
func (sizedTool) ReadOnly() bool                      { return true }
func (sizedTool) StateKey() string                    { return "" }
func (sizedTool) Parse(string) (tool.ToolCall, error) { return nil, nil } //nolint:nilnil // the stub is never parsed
func (sizedTool) Restore(json.RawMessage) error       { return nil }

func TestToolsSizeMeasuresTheWireDefinitions(t *testing.T) {
	const wire = `[{"type":"function","name":"read","description":"read a file","strict":false,"parameters":{"type":"object","properties":{},"additionalProperties":false}}]`

	if got := ToolsSize([]tool.Tool{sizedTool{}}); got != len(wire) {
		t.Errorf("got %d bytes, want %d", got, len(wire))
	}
}
