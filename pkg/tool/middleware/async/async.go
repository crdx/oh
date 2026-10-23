package async

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/tool"
)

type Options struct {
	IsRestartable         bool
	BackgroundUnavailable string
	TaskPolicy            func(context.Context, string) (any, error)
	PublishPort           func(context.Context, uint16, string) (string, error)
	RunLimit              time.Duration
}

func Tool(subject tool.Tool, manager *tasks.Manager, options Options) tool.Tool {
	return configuredTool{Tool: subject, manager: manager, options: options}
}

type configuredTool struct {
	tool.Tool

	manager *tasks.Manager
	options Options
}

func (self configuredTool) Description() string {
	if self.options.BackgroundUnavailable != "" {
		return self.Tool.Description()
	}
	description := strings.TrimSpace(self.Tool.Description())
	if description != "" && !strings.HasSuffix(description, ".") {
		description += "."
	}
	return description + fmt.Sprintf(" A call still running after %s continues in the background; ", activeSpan(self.manager.ForegroundLimit())) +
		fmt.Sprintf("set background=true to start it there at once. A task stops after %s, ", activeSpan(max(tasks.RunLimit, self.options.RunLimit))) +
		"or sooner where the tool has a shorter time limit. To wait for a task, you can just end your turn."
}

func activeSpan(limit time.Duration) string {
	count, unit := int(limit/time.Second), "second"
	if limit >= time.Minute && limit%time.Minute == 0 {
		count, unit = int(limit/time.Minute), "minute"
	}
	if count != 1 {
		unit += "s"
	}
	return fmt.Sprintf("%d active %s", count, unit)
}

func (self configuredTool) Schema() tool.Schema {
	if self.options.BackgroundUnavailable != "" {
		return self.Tool.Schema()
	}
	schema := slices.Clone(self.Tool.Schema())
	schema = append(
		schema,
		tool.Boolean("background", "start this call as a background task").Optional(),
		tool.String("task_name", "an optional name matching "+tasks.NamePattern()+", such as build or docs; never add a number yourself, since a repeated name is numbered automatically").Optional(),
	)
	if self.options.PublishPort != nil {
		schema = append(schema, tool.Integer("forward_port", "a fixed server TCP port to make reachable to the user when starting a background task").Optional())
	}
	if self.options.IsRestartable {
		schema = append(schema, tool.Boolean("respawn", fmt.Sprintf("restart after each run exits; requires background; stops after %d runs in a row each end within %s", tasks.QuickRunsTolerated, tasks.QuickRunLimit)).Optional())
	}
	return schema
}

func (self configuredTool) CompatibleWith(revision string) bool {
	return tool.AcceptsRevision(self.Tool, revision)
}

func (self configuredTool) MarksSuccess() bool { return tool.MarksSuccess(self.Tool) }

func (self configuredTool) Render(arguments string) (tool.CallRendering, bool) {
	return self.RenderUnder(self.Schema(), arguments)
}

func (self configuredTool) RenderUnder(schema tool.Schema, arguments string) (tool.CallRendering, bool) {
	argumentsJSON, options, values, decodeErr := self.decode(schema, arguments)
	if argumentsJSON == "" {
		return self.Tool.Render(arguments)
	}
	rendering, isRendered := tool.RenderUnder(self.Tool, baseSchema(schema), argumentsJSON)
	if isRendered && decodeErr == nil && self.validateOptions(options, values) == nil && options.Name != "" {
		rendering.Introduces = options.Name
	}
	return rendering, isRendered
}

func (self configuredTool) Parse(arguments string) (tool.ToolCall, error) {
	return self.ParseUnder(self.Schema(), arguments)
}

func (self configuredTool) ParseUnder(schema tool.Schema, arguments string) (tool.ToolCall, error) {
	if schema.Find("background") == nil {
		return tool.ParseUnder(self.Tool, schema, arguments)
	}
	argumentsJSON, options, err := self.extract(schema, arguments)
	if err != nil {
		return nil, err
	}
	call, err := tool.ParseUnder(self.Tool, baseSchema(schema), argumentsJSON)
	if err != nil || self.manager == nil || self.options.BackgroundUnavailable != "" {
		return call, err
	}
	return scheduledCall{
		ToolCall:  call,
		toolName:  self.Name(),
		manager:   self.manager,
		options:   options,
		settings:  self.options,
		arguments: argumentsJSON,
	}, nil
}

func baseSchema(schema tool.Schema) tool.Schema {
	return slices.DeleteFunc(slices.Clone(schema), func(parameter tool.Parameter) bool {
		return IsReserved(parameter.Name)
	})
}

func IsReserved(name string) bool {
	return name == "background" || name == "respawn" || name == "task_name" || name == "forward_port"
}

type callOptions struct {
	Name         string
	ForwardPort  int
	IsBackground bool
	IsRespawning bool
}

func (self configuredTool) extract(schema tool.Schema, arguments string) (string, callOptions, error) {
	argumentsJSON, options, values, err := self.decode(schema, arguments)
	if err != nil {
		return "", callOptions{}, err
	}
	if err := self.validateOptions(options, values); err != nil {
		return "", options, err
	}
	return argumentsJSON, options, nil
}

func (self configuredTool) decode(
	schema tool.Schema,
	arguments string,
) (string, callOptions, map[string]json.RawMessage, error) {
	if strings.TrimSpace(arguments) == "" {
		arguments = "{}"
	}
	_, validationErr := schema.Decode(arguments)
	var values map[string]json.RawMessage
	if err := json.Unmarshal([]byte(arguments), &values); err != nil {
		return "", callOptions{}, nil, err
	}
	var options callOptions
	if raw, hasBackground := values["background"]; hasBackground {
		_ = json.Unmarshal(raw, &options.IsBackground)
		delete(values, "background")
	}
	if raw, hasRespawn := values["respawn"]; hasRespawn {
		_ = json.Unmarshal(raw, &options.IsRespawning)
		delete(values, "respawn")
	}
	if raw, hasName := values["task_name"]; hasName {
		_ = json.Unmarshal(raw, &options.Name)
		delete(values, "task_name")
	}
	if raw, hasPort := values["forward_port"]; hasPort {
		_ = json.Unmarshal(raw, &options.ForwardPort)
		delete(values, "forward_port")
	}
	argumentsJSON, err := json.Marshal(values)
	if err != nil {
		return "", options, nil, err
	}
	return string(argumentsJSON), options, values, validationErr
}

func (self configuredTool) validateOptions(options callOptions, values map[string]json.RawMessage) error {
	if self.options.BackgroundUnavailable != "" && options.IsBackground {
		return errors.New(self.options.BackgroundUnavailable)
	}
	if options.Name != "" {
		if err := tasks.ValidateName(options.Name); err != nil {
			return err
		}
	}
	if _, err := forwardPortNumber(options.ForwardPort); err != nil {
		return err
	}
	if options.ForwardPort != 0 && self.options.PublishPort == nil {
		return errors.New("port forwarding is unavailable")
	}
	if !options.IsBackground && (options.Name != "" || options.ForwardPort != 0) {
		return errors.New("task_name and forward_port require background=true")
	}
	if self.Name() == "bash" && options.ForwardPort != 0 {
		var network string
		_ = json.Unmarshal(values["network"], &network)
		if network == "host" {
			return errors.New("forward_port requires the sandbox loopback network")
		}
	}
	if options.IsRespawning && !options.IsBackground {
		return errors.New("respawn requires background=true")
	}
	return nil
}

type scheduledCall struct {
	tool.ToolCall

	toolName  string
	manager   *tasks.Manager
	options   callOptions
	settings  Options
	arguments string
}

func (self scheduledCall) Exec(ctx context.Context) (tool.ToolCallResult, error) {
	port, err := forwardPortNumber(self.options.ForwardPort)
	if err != nil {
		return tool.ToolCallResult{}, err
	}
	invocation := tasks.Invocation{
		Arguments:    self.arguments,
		ForwardPort:  port,
		IsRespawning: self.options.IsRespawning,
	}
	plan, err := self.settings.prepare(ctx, invocation, self.ToolCall)
	if err != nil {
		return tool.ToolCallResult{}, err
	}
	snapshot, err := self.manager.Start(ctx, "", self.toolName, self.options.IsBackground, self.options.IsRespawning,
		plan.Execute, plan.Policy, tasks.StartOptions{
			Name:       self.options.Name,
			Invocation: &invocation,
			Publish:    plan.Publish,
			RunLimit:   plan.RunLimit,
		})
	if err != nil {
		return tool.ToolCallResult{}, err
	}
	conclusion, isBackground, err := self.manager.WaitOrDetach(ctx, snapshot.ID)
	if err != nil {
		return tool.ToolCallResult{}, err
	}
	if !isBackground {
		return conclusion.Result, conclusion.Failure
	}
	receipt := "scheduled as " + snapshot.ID
	if !self.options.IsBackground {
		receipt = "moved to the background as " + snapshot.ID
	}
	if port != 0 {
		publication, state := self.manager.AwaitPublication(ctx, snapshot.ID, tasks.ForegroundLimit)
		receipt += "\n" + tasks.PublicationReceipt(publication, state, port)
	}
	return tool.ToolCallResult{Output: receipt, IsPending: true}, nil
}

func (self Options) prepare(ctx context.Context, invocation tasks.Invocation, call tool.ToolCall) (tasks.PreparationPlan, error) {
	plan := tasks.PreparationPlan{Execute: call.Exec, RunLimit: self.RunLimit}
	if self.TaskPolicy != nil {
		plan.Policy = tasks.PolicySource(func() (any, error) {
			return self.TaskPolicy(ctx, invocation.Arguments)
		})
	}
	if invocation.ForwardPort != 0 {
		if self.PublishPort == nil {
			return tasks.PreparationPlan{}, errors.New("port forwarding is unavailable")
		}
		plan.Publish = func(ctx context.Context, id string, port uint16) (string, error) {
			return self.PublishPort(ctx, port, id)
		}
	}
	return plan, nil
}

func Bind(subject tool.Tool, schema tool.Schema) {
	wrappedTool, isWrapped := subject.(configuredTool)
	if !isWrapped || wrappedTool.manager == nil || wrappedTool.options.BackgroundUnavailable != "" || schema.Find("background") == nil {
		return
	}
	wrappedTool.manager.BindTool(subject.Name(), func(ctx context.Context, invocation tasks.Invocation) (tasks.PreparationPlan, error) {
		call, err := tool.ParseUnder(wrappedTool.Tool, baseSchema(schema), invocation.Arguments)
		if err != nil {
			return tasks.PreparationPlan{}, err
		}
		return wrappedTool.options.prepare(ctx, invocation, call)
	})
}

func (self scheduledCall) TimeLimit() time.Duration { return self.ToolCall.TimeLimit() }

var (
	_ tool.SchemaParser  = configuredTool{}
	_ tool.SuccessMarker = configuredTool{}
)

func forwardPortNumber(number int) (uint16, error) {
	if number < 0 || number > 65535 {
		return 0, errors.New("forward_port must be 0 or between 1 and 65535")
	}
	return uint16(number), nil
}
