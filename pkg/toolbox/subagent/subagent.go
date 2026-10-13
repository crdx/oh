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

	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/bash"
)

const Name = "subagent"

var ErrWithheld = errors.New("subagents unavailable; ctrl+x s grants them")

const (
	Start  = "start"
	Send   = "send"
	Status = "status"
	Output = "output"
	Stop   = "stop"
	List   = "list"
	Models = "models"
)

type Task struct {
	Model     string `json:"model"`
	Prompt    string `json:"prompt"`
	Intent    string `json:"intent"`
	Workspace string `json:"workspace,omitempty"`
}

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
}

type Manager interface {
	Start(ctx context.Context, systemPrompt string, tasks []Task) (string, error)
	Send(ctx context.Context, name string, message string) (string, error)
	Broadcast(ctx context.Context, message string) ([]string, error)
	Status(names []string) (string, error)
	Output(names []string) (string, error)
	Stop(names []string) (string, error)
	List() string
	Models() string
}

func New(manager Manager, isAllowed func() bool) tool.Tool {
	definition := tool.Definition{
		Name: Name,
		Description: "start independent subagents with a shared optional system prompt and individual tasks, " +
			"send one a message, which a running subagent reads once its current step finishes and a finished one takes as a follow-up, " +
			"or send every running subagent the same message by leaving out the name, " +
			"and inspect or control them by name; " +
			"call models to list valid models and their supported efforts, " +
			"then give every task an explicit provider/model@effort selection from that list; use provider/model when no efforts are listed; " +
			"a follow-up keeps the model the subagent started on; " +
			"only a limited number run at once, and start says how many are running against the limit; " +
			"a subagent knows only a short description of its sandbox, your system_prompt, and its prompt: " +
			"none of your instructions, context files or conversation reach it, so give it everything it needs; " +
			"each subagent can read but not write its workspace, and has a shell only while you do, " +
			"it can also read your configured read, home, and executable search paths, " +
			"but not your configured write paths or anything granted during this session, " +
			"writing only to its own scratch, which is subagents/<name> inside your scratch but which it calls /tmp, " +
			"so a /tmp path it reports may mean your /tmp/subagents/<name>/...; " +
			"it cannot read the rest of your scratch, so put what it needs in its prompt, " +
			"or, for a follow-up, write files into your /tmp/subagents/<name> and refer to them by the /tmp paths it sees; " +
			"never trust an answer blindly: however confident, it is an unverified claim that may be wrong, " +
			"miss part of its task, or describe checks it never ran, " +
			"so before you act on it or pass it on, verify what matters yourself, " +
			"such as by opening files it reports, rerunning key commands, or reading the code it cites; " +
			"a finished subagent's answer reaches you on its own, between your tool calls while you work " +
			"or by waking you once you have ended your turn, so carry on or end your turn rather than waiting or polling for it",
		Schema: tool.Schema{
			tool.Enum("action", "what to do", Start, Send, Status, Output, Stop, List, Models),
			tool.StringArray("names", "the subagents for status, output, or stop; empty means all of them").Optional(),
			tool.String("name", "the subagent to send a message to (for send); absent means every running subagent").Optional(),
			tool.String("message", "the message to send (for send)").Optional(),
			tool.String("system_prompt", "the only instructions every subagent shares, beside a short description of its sandbox (for start)").Optional(),
			tool.ObjectArray("subagents", "one task per independent subagent (for start)", tool.Schema{
				tool.String("model", "the exact provider/model@effort selection from action=models; omit @effort only for a model that takes no effort"),
				tool.String("prompt", "the whole task for this subagent, with whatever context it needs"),
				tool.String("intent", "what this subagent is for, as a 5 to 7 word phrase opening with a capitalised -ing verb, such as \"Reviewing the parser for missed errors\""),
				tool.String("workspace", "the subagent's read-only workspace, any directory you can read; absent means yours").Optional(),
			}).Optional(),
		},
	}
	return tool.Implement(definition, Describe).Decode(decode).Validate(validate).Requires(isAllowed, ErrWithheld).Plain(func(ctx context.Context, args Args) (string, error) {
		return act(ctx, manager, args)
	})
}

func act(ctx context.Context, manager Manager, args Args) (string, error) {
	switch args.Action {
	case Start:
		tasks := slices.Clone(args.Subagents)
		for index := range tasks {
			tasks[index].Intent = bash.SpokenIntent(tasks[index].Intent)
		}
		return manager.Start(ctx, args.SystemPrompt, tasks)
	case Send:
		if args.Name == "" {
			return broadcast(ctx, manager, args.Message)
		}
		return manager.Send(ctx, args.Name, args.Message)
	case Status:
		return manager.Status(args.Names)
	case Output:
		return manager.Output(args.Names)
	case Stop:
		return manager.Stop(args.Names)
	case List:
		return manager.List(), nil
	case Models:
		return manager.Models(), nil
	}
	return "", errors.New("unknown subagent action")
}

var statusActions = []string{Start, Send, Status, Stop}

func Describe(args Args) tool.CallRendering {
	rendering := tool.CallRendering{
		Kind:          Name + "_" + args.Action,
		ReportsStatus: slices.Contains(statusActions, args.Action),
	}
	switch args.Action {
	case Start:
		rendering.Subject = startSubject(args)
		if len(args.Subagents) == 1 {
			rendering.Intent = bash.SpokenIntent(args.Subagents[0].Intent)
		}
	case Send:
		rendering.Subject = args.Name
		rendering.Mentions = mentions([]string{args.Name})
		if args.Name == "" {
			rendering.Subject = "every running subagent"
			rendering.Mentions = nil
		}
	case List:
		rendering.Subject = "subagents"
	case Models:
		rendering.Subject = "models"
	case Status, Output, Stop:
		rendering.Subject = everyOrAll(args.Names, ", ")
		rendering.Mentions = mentions(args.Names)
	}
	return rendering
}

func broadcast(ctx context.Context, manager Manager, message string) (string, error) {
	names, err := manager.Broadcast(ctx, message)
	if err != nil {
		return "", err
	}
	if len(names) == 1 {
		return "queued for " + names[0], nil
	}
	return "queued for " + strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1], nil
}

func startSubject(args Args) string {
	if len(args.Subagents) == 1 {
		if args.Subagents[0].Model != "" {
			return "1 subagent · " + args.Subagents[0].Model
		}
		return "1 subagent"
	}
	subject := fmt.Sprintf("%d subagents", len(args.Subagents))
	if len(args.Subagents) == 0 {
		return subject
	}
	var selections []string
	intents := make([]string, len(args.Subagents))
	for index, task := range args.Subagents {
		if task.Model != "" {
			selections = append(selections, task.Model)
		}
		intents[index] = bash.SpokenIntent(task.Intent)
	}
	if len(selections) > 0 {
		subject += " · " + strings.Join(selections, ", ")
	}
	return subject + " · " + strings.Join(intents, ", ")
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
			if strings.TrimSpace(task.Model) == "" {
				return fmt.Errorf("subagents[%d].model is required; call action=models for valid selections", index)
			}
			if strings.Contains(task.Model, "+") {
				return fmt.Errorf("subagents[%d].model must use provider/model@effort or provider/model", index)
			}
			if strings.TrimSpace(task.Prompt) == "" {
				return fmt.Errorf("subagents[%d].prompt is empty", index)
			}
			if strings.TrimSpace(task.Intent) == "" {
				return fmt.Errorf("subagents[%d].intent is empty", index)
			}
		}
	case Send:
		if strings.TrimSpace(args.Message) == "" {
			return errors.New("send requires a nonempty message")
		}
	case Status, Output, Stop, List, Models:
	default:
		return fmt.Errorf("unknown subagent action %q", args.Action)
	}
	if (args.Action == List || args.Action == Models) && len(args.Names) > 0 {
		return fmt.Errorf("%s takes no names", args.Action)
	}
	if (isStart || isSend) && len(args.Names) > 0 {
		return errors.New("names is for status, output, and stop")
	}
	if !isSend && (args.Name != "" || args.Message != "") {
		return errors.New("name and message require action=send")
	}
	if !isStart && (args.SystemPrompt != "" || len(args.Subagents) != 0) {
		return errors.New("system_prompt and subagents require action=start")
	}
	return nil
}
