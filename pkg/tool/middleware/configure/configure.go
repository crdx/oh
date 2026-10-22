package configure

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/tool"
)

type Options struct {
	Description   string
	Concurrency   int
	TimeLimit     time.Duration
	RequireAccess func() error
	Approve       func(context.Context, string) error
}

func Tool(subject tool.Tool, options Options) tool.Tool {
	if options.Description == "" && options.Concurrency == 0 && options.TimeLimit == 0 && options.RequireAccess == nil && options.Approve == nil {
		return subject
	}

	var slots chan struct{}
	if options.Concurrency > 0 {
		slots = make(chan struct{}, options.Concurrency)
	}

	return configuredTool{Tool: subject, options: options, slots: slots}
}

type configuredTool struct {
	tool.Tool

	options Options
	slots   chan struct{}
}

func (self configuredTool) Description() string {
	if self.options.Description != "" {
		return self.options.Description
	}
	return self.Tool.Description()
}

func (self configuredTool) CompatibleWith(revision string) bool {
	return tool.AcceptsRevision(self.Tool, revision)
}

func (self configuredTool) MarksSuccess() bool { return tool.MarksSuccess(self.Tool) }

func (self configuredTool) Concurrent() bool {
	if self.options.Concurrency > 0 {
		return self.options.Concurrency > 1 && self.Tool.Concurrent()
	}
	return self.Tool.Concurrent()
}

func (self configuredTool) RenderUnder(schema tool.Schema, arguments string) (tool.CallRendering, bool) {
	return tool.RenderUnder(self.Tool, schema, arguments)
}

func (self configuredTool) Parse(arguments string) (tool.ToolCall, error) {
	call, err := self.Tool.Parse(arguments)
	return self.configure(call, arguments, err)
}

func (self configuredTool) ParseUnder(schema tool.Schema, arguments string) (tool.ToolCall, error) {
	call, err := tool.ParseUnder(self.Tool, schema, arguments)
	return self.configure(call, arguments, err)
}

func (self configuredTool) configure(call tool.ToolCall, arguments string, err error) (tool.ToolCall, error) {
	if err != nil {
		return nil, err
	}
	return configuredCall{ToolCall: call, arguments: arguments, options: self.options, slots: self.slots}, nil
}

type configuredCall struct {
	tool.ToolCall

	arguments string
	options   Options
	slots     chan struct{}
}

func (self configuredCall) TimeLimit() time.Duration {
	inner := self.ToolCall.TimeLimit()
	if self.options.TimeLimit == 0 || inner > 0 && inner < self.options.TimeLimit {
		return inner
	}
	return self.options.TimeLimit
}

func (self configuredCall) Exec(ctx context.Context) (tool.ToolCallResult, error) {
	if self.options.RequireAccess != nil {
		if err := self.options.RequireAccess(); err != nil {
			return tool.ToolCallResult{}, err
		}
	}
	if self.options.Approve != nil {
		if err := self.options.Approve(ctx, self.arguments); err != nil {
			return tool.ToolCallResult{}, err
		}
	}
	if self.slots != nil {
		if err := tool.TakeSlot(ctx, self.slots); err != nil {
			return tool.ToolCallResult{}, err
		}
		defer func() { <-self.slots }()
	}
	if err := ctx.Err(); err != nil {
		return tool.ToolCallResult{}, err
	}
	if self.options.RequireAccess != nil {
		if err := self.options.RequireAccess(); err != nil {
			return tool.ToolCallResult{}, err
		}
	}
	limit := self.TimeLimit()
	if limit == 0 {
		return self.ToolCall.Exec(ctx)
	}
	var boundedContext context.Context
	var cancel context.CancelFunc
	if tasks.Managed(ctx) {
		boundedContext, cancel = tasks.ActiveTimeout(ctx, limit)
	} else {
		boundedContext, cancel = context.WithTimeout(ctx, limit)
	}
	defer cancel()

	result, err := self.ToolCall.Exec(boundedContext)
	if errors.Is(context.Cause(boundedContext), context.DeadlineExceeded) && ctx.Err() == nil {
		note := fmt.Sprintf("stopped after the tool timeout of %s; output may be incomplete", limit)
		if strings.TrimSpace(result.Output) != "" {
			result.Output = note + ":\n" + result.Output
		}
		return result, fmt.Errorf("%s: %w", note, context.DeadlineExceeded)
	}
	return result, err
}

var (
	_ tool.SchemaParser  = configuredTool{}
	_ tool.SuccessMarker = configuredTool{}
)
