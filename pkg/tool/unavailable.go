package tool

import (
	"context"
	"encoding/json"
	"errors"
)

type unavailableTool struct {
	snapshot Snapshot
	reason   string
}

func Unavailable(snapshot Snapshot, reason string) Tool {
	return unavailableTool{snapshot: snapshot, reason: reason}
}

func IsUnavailable(subject Tool) bool {
	_, isUnavailable := subject.(unavailableTool)
	return isUnavailable
}

func (self unavailableTool) Name() string               { return self.snapshot.Definition.Name }
func (self unavailableTool) Description() string        { return self.snapshot.Definition.Description }
func (self unavailableTool) Schema() Schema             { return self.snapshot.Definition.Schema }
func (self unavailableTool) Revision() string           { return self.snapshot.Revision }
func (self unavailableTool) CompatibleWith(string) bool { return false }
func (self unavailableTool) Concurrent() bool           { return false }
func (self unavailableTool) ReadOnly() bool             { return true }
func (self unavailableTool) StateKey() string           { return "" }

func (self unavailableTool) Render(arguments string) (CallRendering, bool) {
	return CallRendering{Subject: DescribeUnparsedArguments(self, arguments)}, true
}

func (self unavailableTool) Parse(arguments string) (ToolCall, error) {
	return _call{
		rendering: CallRendering{Subject: DescribeUnparsedArguments(self, arguments)},
		exec: func(context.Context) (ToolCallResult, error) {
			return ToolCallResult{}, errors.New(self.reason)
		},
	}, nil
}

func (self unavailableTool) Restore(json.RawMessage) error { return nil }
