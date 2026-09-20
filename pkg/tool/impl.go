package tool

import (
	"context"
	"encoding/json"
	"time"
)

type _tool struct {
	name        string
	description string
	schema      Schema
	parse       func(arguments string) (_call, error)

	parallel  bool
	readOnly  bool
	stateName string
	restore   Restorer
	emphasis  func(call ToolCall) Emphasis
}

func (self _tool) Name() string        { return self.name }
func (self _tool) Description() string { return self.description }
func (self _tool) Schema() Schema      { return self.schema }
func (self _tool) Concurrent() bool    { return self.parallel }
func (self _tool) ReadOnly() bool      { return self.readOnly }
func (self _tool) StateKey() string    { return self.stateName }

func (self _tool) Restore(state json.RawMessage) error {
	if self.restore == nil {
		return nil
	}

	return self.restore(state)
}

func (self _tool) Parse(arguments string) (ToolCall, error) {
	call, err := self.parse(arguments)
	if err != nil {
		return nil, err
	}
	if self.emphasis != nil {
		call.emphasis = self.emphasis(call)
		call.emphasis.Source = call.emphasisSource
	}

	return call, nil
}

type _call struct {
	subject        string
	qualifier      string
	emphasis       Emphasis
	emphasisSource string
	continuation   []CallRendering
	timeLimit      time.Duration
	exec           func(ctx context.Context) (ToolCallResult, error)
}

func (self _call) Subject() string               { return self.subject }
func (self _call) Qualifier() string             { return self.qualifier }
func (self _call) Emphasis() Emphasis            { return self.emphasis }
func (self _call) Continuation() []CallRendering { return self.continuation }
func (self _call) TimeLimit() time.Duration      { return self.timeLimit }

func (self _call) Exec(ctx context.Context) (ToolCallResult, error) { return self.exec(ctx) }
