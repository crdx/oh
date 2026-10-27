package task

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/tool"
)

const (
	Name          = "task"
	ActionStatus  = "status"
	ActionOutput  = "output"
	ActionStop    = "stop"
	ActionRestart = "restart"
	ActionDiscard = "discard"
	ActionList    = "list"
	ActionPrune   = "prune"
)

var actions = []string{ActionStatus, ActionOutput, ActionStop, ActionList, ActionPrune, ActionRestart, ActionDiscard}

type Args struct {
	Action string `json:"action"`
	Name   string `json:"name,omitempty"`
}

func New(manager *tasks.Manager) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        Name,
			Description: "manage background tool invocations and read their live output. Restart reuses the saved tool arguments. Discard stops the task and removes its record. A completion reports its result and wakes the conversation, so end your turn to wait for a task. Never wait with sleep or by polling its status.",
			Schema: tool.Schema{
				tool.Enum("action", "what to do", actions...),
				tool.String("name", "the task name or id, except for list and prune").Optional(),
			},
		},
		Describe,
	).Validate(validate).Plain(func(ctx context.Context, args Args) (string, error) {
		return act(ctx, manager, args)
	})
}

func Describe(args Args) tool.CallRendering {
	subject := args.Name
	if subject == "" {
		subject = "tasks"
		if args.Action == ActionPrune {
			subject = "finished tasks"
		}
	}
	rendering := tool.CallRendering{
		Kind: "task_" + args.Action, Subject: subject,
		ReportsStatus: args.Action == ActionStatus || args.Action == ActionStop || args.Action == ActionPrune || args.Action == ActionRestart || args.Action == ActionDiscard,
	}
	if args.Name != "" {
		rendering.Mentions = []string{args.Name}
	}
	return rendering
}

func validate(args Args) error {
	if !slices.Contains(actions, args.Action) {
		return fmt.Errorf("action must be %s", strings.Join(actions, ", "))
	}
	if args.Action == ActionList || args.Action == ActionPrune {
		if args.Name != "" {
			return errors.New("list and prune take no name")
		}
		return nil
	}
	if args.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

func act(ctx context.Context, manager *tasks.Manager, args Args) (string, error) {
	switch args.Action {
	case ActionStatus:
		snapshot, err := manager.Status(args.Name)
		if err != nil {
			return "", err
		}
		return describe(snapshot), nil
	case ActionOutput:
		snapshot, err := manager.Output(args.Name)
		if err != nil {
			return "", err
		}
		return describeOutput(snapshot), nil
	case ActionStop:
		if err := manager.Stop(args.Name, "the agent stopped it"); err != nil {
			return "", err
		}
		snapshot, err := manager.Status(args.Name)
		if err != nil {
			return "", err
		}
		return describe(snapshot), nil
	case ActionRestart:
		snapshot, err := manager.Restart(ctx, args.Name)
		if err != nil {
			return "", err
		}
		output := describe(snapshot)
		if snapshot.Invocation != nil && snapshot.Invocation.ForwardPort != 0 {
			port := snapshot.Invocation.ForwardPort
			publication, state := manager.AwaitPublication(ctx, snapshot.ID, tasks.PublicationLimit)
			output += "\n" + tasks.PublicationReceipt(publication, state, port)
		}
		return output, nil
	case ActionDiscard:
		snapshot, err := manager.Discard(ctx, args.Name)
		if err != nil {
			return "", err
		}
		return "discarded " + describe(snapshot), nil
	case ActionList:
		snapshots := manager.List()
		if len(snapshots) == 0 {
			return "no background tasks are listed.", nil
		}
		lines := make([]string, 0, len(snapshots))
		for _, snapshot := range snapshots {
			lines = append(lines, describe(snapshot))
		}
		return strings.Join(lines, "\n"), nil
	case ActionPrune:
		names := manager.PruneFinished()
		if len(names) == 0 {
			return "there are no finished tasks to prune.", nil
		}
		return "pruned " + strings.Join(names, ", ") + ".", nil
	default:
		return "", errors.New("unrecognised action")
	}
}

func describeOutput(snapshot tasks.Snapshot) string {
	lines := []string{describe(snapshot)}
	for _, previous := range snapshot.Previous {
		line := fmt.Sprintf("run %d: %s", previous.Run, previous.State)
		if previous.Failure != "" {
			line += " — " + previous.Failure
		}
		lines = append(lines, line)
		if previous.Output != "" {
			lines = append(lines, previous.Output)
		}
	}
	switch {
	case snapshot.Output != "":
		lines = append(lines, snapshot.Output)
	case len(snapshot.Previous) != 0:
	case snapshot.State == tasks.Queued || snapshot.State == tasks.Running || snapshot.State == tasks.Stopping:
		lines[0] += "; no output yet"
	default:
		lines[0] += "; no output"
	}
	return strings.Join(lines, "\n")
}

func describe(snapshot tasks.Snapshot) string {
	status := snapshot.ID + ": " + snapshot.State.Spoken()
	if snapshot.Run > 1 {
		status += fmt.Sprintf(", run %d", snapshot.Run)
	}
	if snapshot.IsRespawning {
		status += ", respawns on exit"
	}
	if snapshot.IsRespawnLimitReached {
		status += ", " + tasks.RespawnLimitReason()
	}
	if snapshot.Failure != "" {
		status += " — " + snapshot.Failure
	}
	return status
}
