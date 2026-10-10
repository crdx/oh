package subagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"crdx.org/oh/internal/util"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/bash"
)

const Name = "subagent"

const (
	Start  = "start"
	Send   = "send"
	Status = "status"
	Output = "output"
	Wait   = "wait"
	Stop   = "stop"
	List   = "list"
)

type Task struct {
	Prompt    string `json:"prompt"`
	Intent    string `json:"intent"`
	Workspace string `json:"workspace,omitempty"`
}

const waitLimit = bash.CommandTimeout

func Mention(name string) string {
	return Name + ":" + name
}

type Args struct {
	Action       string   `json:"action"`
	Names        []string `json:"names,omitempty"`
	Name         string   `json:"name,omitempty"`
	Message      string   `json:"message,omitempty"`
	SystemPrompt string   `json:"system_prompt,omitempty"`
	Subagents    []Task   `json:"subagents,omitempty"`
	WaitSeconds  int      `json:"wait_seconds,omitempty"`
}

type Manager interface {
	Start(ctx context.Context, systemPrompt string, tasks []Task) (string, error)
	Send(ctx context.Context, name string, message string) (string, error)
	Status(names []string) (string, error)
	Output(names []string) (string, error)
	Wait(ctx context.Context, names []string, limit time.Duration) (string, error)
	Stop(names []string) (string, error)
	List() string
}

func New(manager Manager, model string) tool.Tool {
	definition := tool.Definition{
		Name: Name,
		Description: "start independent subagents with a shared optional system prompt and individual tasks, " +
			"send a finished subagent a follow-up, and inspect or control them by name; at most 5 run at once; " +
			"a subagent knows only a short description of its sandbox, your system_prompt, and its prompt: " +
			"none of your instructions, context files or conversation reach it, so give it everything it needs; " +
			"each subagent can read but not write its workspace, and has a shell only while you do, " +
			"writing only to its own scratch, which you reach as subagents/<name> inside yours; " +
			"finished subagents whose answers you have not read through wait or output report back after a short batching window",
		Schema: tool.Schema{
			tool.Enum("action", "what to do", Start, Send, Status, Output, Wait, Stop, List),
			tool.StringArray("names", "the subagents for status, output, wait, or stop; empty means all of them").Optional(),
			tool.Integer("wait_seconds", fmt.Sprintf("how many seconds to wait at most — max %s (default)", util.CompactDuration(waitLimit))).Optional(),
			tool.String("name", "the finished subagent to send a follow-up to (for send)").Optional(),
			tool.String("message", "the follow-up to send (for send)").Optional(),
			tool.String("system_prompt", "the only instructions every subagent shares, beside a short description of its sandbox (for start)").Optional(),
			tool.ObjectArray("subagents", "one task per independent subagent (for start)", tool.Schema{
				tool.String("prompt", "the whole task for this subagent, with whatever context it needs"),
				tool.String("intent", "what this subagent is for, as a 5 to 7 word phrase opening with a capitalised -ing verb, such as \"Reviewing the parser for missed errors\""),
				tool.String("workspace", "the subagent's read-only workspace, any directory you can read; absent means yours").Optional(),
			}).Optional(),
		},
	}
	describe := func(args Args) tool.CallRendering {
		rendering := Describe(args)
		if args.Action == Start && model != "" {
			rendering.Qualifier = "on " + model
		}
		return rendering
	}
	return tool.Implement(definition, describe).Decode(decode).Validate(validate).TakesAtMost(getTimeLimit).Plain(func(ctx context.Context, args Args) (string, error) {
		switch args.Action {
		case Start:
			tasks := slices.Clone(args.Subagents)
			for index := range tasks {
				tasks[index].Intent = bash.SpokenIntent(tasks[index].Intent)
			}
			return manager.Start(ctx, args.SystemPrompt, tasks)
		case Send:
			return manager.Send(ctx, args.Name, args.Message)
		case Status:
			return manager.Status(args.Names)
		case Output:
			return manager.Output(args.Names)
		case Wait:
			return manager.Wait(ctx, args.Names, getWaitLimit(args))
		case Stop:
			return manager.Stop(args.Names)
		case List:
			return manager.List(), nil
		}
		return "", errors.New("unknown subagent action")
	})
}

var statusActions = []string{Start, Send, Status, Stop}

func Describe(args Args) tool.CallRendering {
	rendering := tool.CallRendering{
		Kind:          Name + "_" + args.Action,
		ReportsStatus: slices.Contains(statusActions, args.Action),
	}
	switch args.Action {
	case Start:
		rendering.Subject = fmt.Sprintf("%d subagents", len(args.Subagents))
		if len(args.Subagents) == 1 {
			rendering.Subject = "1 subagent"
			rendering.Intent = bash.SpokenIntent(args.Subagents[0].Intent)
		}
	case Send:
		rendering.Subject = args.Name
		rendering.Mentions = mentions([]string{args.Name})
	case List:
		rendering.Subject = "subagents"
	case Wait:
		rendering.Subject = everyOrAll(args.Names, " && ")
		rendering.Mentions = mentions(args.Names)
		if args.WaitSeconds > 0 {
			rendering.Qualifier = "for up to " + util.CompactDuration(getWaitLimit(args))
		}
	case Status, Output, Stop:
		rendering.Subject = everyOrAll(args.Names, ", ")
		rendering.Mentions = mentions(args.Names)
	}
	return rendering
}

func everyOrAll(names []string, separator string) string {
	if len(names) == 0 {
		return "all"
	}
	return strings.Join(names, separator)
}

func mentions(names []string) []string {
	var mentionNames []string
	for _, name := range names {
		mentionNames = append(mentionNames, Mention(name))
	}
	return mentionNames
}

func getTimeLimit(args Args) time.Duration {
	if args.Action != Wait {
		return 0
	}
	return getWaitLimit(args)
}

func getWaitLimit(args Args) time.Duration {
	if args.WaitSeconds <= 0 {
		return waitLimit
	}
	return min(time.Duration(args.WaitSeconds)*time.Second, waitLimit)
}

func decode(raw string) (Args, error) {
	var args Args
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return args, fmt.Errorf("could not parse the arguments: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return args, errors.New("more than one argument object or invalid trailing text")
	}
	return args, nil
}

func validate(args Args) error {
	isStart := args.Action == Start
	isSend := args.Action == Send
	switch args.Action {
	case Start:
		if len(args.Subagents) == 0 {
			return errors.New("start requires a nonempty subagents list")
		}
		for index, task := range args.Subagents {
			if strings.TrimSpace(task.Prompt) == "" {
				return fmt.Errorf("subagents[%d].prompt is empty", index)
			}
			if strings.TrimSpace(task.Intent) == "" {
				return fmt.Errorf("subagents[%d].intent is empty", index)
			}
		}
	case Send:
		if args.Name == "" || strings.TrimSpace(args.Message) == "" {
			return errors.New("send requires a name and a nonempty message")
		}
	case Status, Output, Wait, Stop, List:
	default:
		return fmt.Errorf("unknown subagent action %q", args.Action)
	}
	if args.Action == List && len(args.Names) > 0 {
		return errors.New("list takes no names")
	}
	if (isStart || isSend) && len(args.Names) > 0 {
		return errors.New("names is for status, output, wait, and stop")
	}
	if !isSend && (args.Name != "" || args.Message != "") {
		return errors.New("name and message require action=send")
	}
	if args.Action != Wait && args.WaitSeconds != 0 {
		return errors.New("wait_seconds requires action=wait")
	}
	if !isStart && (args.SystemPrompt != "" || len(args.Subagents) != 0) {
		return errors.New("system_prompt and subagents require action=start")
	}
	return nil
}
