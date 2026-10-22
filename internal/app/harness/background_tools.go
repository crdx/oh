package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"

	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/toolset"
	"crdx.org/oh/internal/app/tty"
	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/tool/middleware/async"
	"crdx.org/oh/pkg/toolbox/bash"
	"crdx.org/oh/pkg/toolbox/forward"
	"crdx.org/oh/pkg/toolbox/task"
)

type backgroundOptions struct {
	settings         config.Config
	manager          *tasks.Manager
	resumedSession   *store.Session
	isPrinting       bool
	isYolo           bool
	isForwardOffered bool
	policyFor        func(string) func(context.Context, string) (any, error)
}

func canSuperviseTasks(isPrinting bool, keyboard *os.File) bool {
	return !isPrinting && tty.Is(keyboard) && tty.Is(os.Stdout)
}

func isOffered(resumedSession *store.Session, requestedNames []string, name string) bool {
	if resumedSession == nil {
		return toolset.Offers(requestedNames, name)
	}
	return slices.ContainsFunc(resumedSession.Meta.ToolDefinitions, func(definition store.ToolDefinition) bool {
		return definition.Name == name
	})
}

func isUsable(toolboxTools []tool.Tool, frozenDefinitions []store.ToolDefinition, name string) bool {
	index := slices.IndexFunc(toolboxTools, func(subject tool.Tool) bool { return subject.Name() == name })
	if index < 0 {
		return false
	}
	if len(frozenDefinitions) == 0 {
		return true
	}
	return slices.ContainsFunc(store.RestoreTools(frozenDefinitions), func(snapshot tool.Snapshot) bool {
		return snapshot.Definition.Name == name && tool.IsCompatible(toolboxTools[index], snapshot)
	})
}

func hasFrozenBackground(definitions []store.ToolDefinition, name string) bool {
	return slices.ContainsFunc(definitions, func(definition store.ToolDefinition) bool {
		return definition.Name == name && slices.ContainsFunc(definition.Parameters, func(parameter store.ToolParameter) bool {
			return parameter.Name == "background"
		})
	})
}

func backgroundTools(toolboxTools []tool.Tool, options backgroundOptions) ([]tool.Tool, error) {
	var frozenDefinitions []store.ToolDefinition
	if options.resumedSession != nil {
		frozenDefinitions = options.resumedSession.Meta.ToolDefinitions
		if len(frozenDefinitions) == 0 {
			return slices.DeleteFunc(slices.Clone(toolboxTools), func(subject tool.Tool) bool {
				return subject.Name() == task.Name
			}), nil
		}
	}
	controller := options.manager
	if !isUsable(toolboxTools, frozenDefinitions, task.Name) {
		controller = nil
	}
	unavailable := ""
	switch {
	case options.isPrinting:
		unavailable = "background calls are unavailable in print mode"
	case options.manager == nil:
		unavailable = "background calls need an interactive terminal"
	case controller == nil:
		unavailable = "background tasks require the task tool, which is disabled"
	}
	var forwardTool tool.Tool
	if options.isForwardOffered && isUsable(toolboxTools, frozenDefinitions, forward.Name) {
		if index := slices.IndexFunc(toolboxTools, func(subject tool.Tool) bool { return subject.Name() == forward.Name }); index >= 0 {
			forwardTool = toolboxTools[index]
		}
	}
	wrappedTools := slices.Clone(toolboxTools)
	for index, subject := range wrappedTools {
		isBackgroundable := options.settings.IsToolBackgroundable(subject.Name())
		if !isBackgroundable && !hasFrozenBackground(frozenDefinitions, subject.Name()) {
			continue
		}
		if hasReservedParameter(subject) {
			if !isBackgroundable {
				continue
			}
			return nil, fmt.Errorf("%s has an argument reserved for background execution", subject.Name())
		}
		toolUnavailable := unavailable
		if !isBackgroundable {
			toolUnavailable = "background calls are disabled for the " + subject.Name() + " tool"
		}
		var publishPort func(context.Context, uint16, string) (string, error)
		if subject.Name() == bash.Name {
			publishPort = portPublisher(forwardTool, options.isYolo)
		}
		wrappedTools[index] = async.Tool(subject, controller, async.Options{
			PublishPort:           publishPort,
			IsRestartable:         subject.Name() == bash.Name,
			BackgroundUnavailable: toolUnavailable,
			TaskPolicy:            options.policyFor(subject.Name()),
			RunLimit:              options.settings.Tools[subject.Name()].Timeout,
		})
	}
	return wrappedTools, nil
}

func hasReservedParameter(subject tool.Tool) bool {
	return slices.ContainsFunc(subject.Schema(), func(parameter tool.Parameter) bool {
		return async.IsReserved(parameter.Name)
	})
}

func portPublisher(forwardTool tool.Tool, isYolo bool) func(context.Context, uint16, string) (string, error) {
	if isYolo {
		return func(_ context.Context, port uint16, _ string) (string, error) {
			return "Use " + portgrant.URL(portgrant.LocalHost, port) + " on the host network.", nil
		}
	}
	if forwardTool == nil {
		return nil
	}
	return func(ctx context.Context, port uint16, taskName string) (string, error) {
		arguments, err := json.Marshal(forward.Args{Action: "add", Port: int(port), TaskName: taskName})
		if err != nil {
			return "", err
		}
		call, err := forwardTool.Parse(string(arguments))
		if err != nil {
			return "", err
		}
		result, err := call.Exec(ctx)
		if err != nil {
			return "", err
		}
		return result.Output, nil
	}
}
