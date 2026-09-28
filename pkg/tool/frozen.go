package tool

import "encoding/json"

type frozenTool struct {
	current  Tool
	snapshot Snapshot
}

func WithSnapshot(current Tool, snapshot Snapshot) Tool {
	return frozenTool{current: current, snapshot: snapshot}
}

func (self frozenTool) Name() string        { return self.snapshot.Definition.Name }
func (self frozenTool) Description() string { return self.snapshot.Definition.Description }
func (self frozenTool) Schema() Schema      { return self.snapshot.Definition.Schema }
func (self frozenTool) Revision() string    { return self.snapshot.Revision }
func (self frozenTool) Concurrent() bool    { return self.current.Concurrent() }
func (self frozenTool) ReadOnly() bool      { return self.current.ReadOnly() }
func (self frozenTool) StateKey() string    { return self.current.StateKey() }

func (self frozenTool) Render(arguments string) (CallRendering, bool) {
	return self.current.Render(arguments)
}

func (self frozenTool) Parse(arguments string) (ToolCall, error) {
	return self.current.Parse(arguments)
}

func (self frozenTool) Restore(state json.RawMessage) error {
	return self.current.Restore(state)
}
