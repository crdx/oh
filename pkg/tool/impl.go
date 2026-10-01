package tool

import (
	"context"
	"encoding/json"
	"time"
)

type _tool struct {
	name                string
	description         string
	schema              Schema
	revision            string
	compatibleRevisions map[string]struct{}
	parse               func(arguments string) (_call, error)
	render              func(arguments string) (CallRendering, bool)

	parallel        bool
	readOnly        bool
	isSuccessMarked bool
	stateName       string
	restore         Restorer
	fallback        CallRendering
}

func (self _tool) Name() string        { return self.name }
func (self _tool) Description() string { return self.description }
func (self _tool) Schema() Schema      { return self.schema }
func (self _tool) Revision() string    { return self.revision }
func (self _tool) CompatibleWith(revision string) bool {
	_, isCompatible := self.compatibleRevisions[revision]
	return isCompatible
}
func (self _tool) Concurrent() bool                              { return self.parallel }
func (self _tool) ReadOnly() bool                                { return self.readOnly }
func (self _tool) StateKey() string                              { return self.stateName }
func (self _tool) MarksSuccess() bool                            { return self.isSuccessMarked }
func (self _tool) Render(arguments string) (CallRendering, bool) { return self.render(arguments) }

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

	return call, nil
}

type _call struct {
	rendering CallRendering
	timeLimit time.Duration
	exec      func(ctx context.Context) (ToolCallResult, error)
}

func (self _call) Rendering() CallRendering { return self.rendering }
func (self _call) TimeLimit() time.Duration { return self.timeLimit }

func (self _call) Exec(ctx context.Context) (ToolCallResult, error) { return self.exec(ctx) }
