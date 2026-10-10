package job

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/jobs"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/bash"
	"crdx.org/oh/pkg/toolbox/forward"
)

const (
	actionStart   = "start"
	actionStatus  = "status"
	actionOutput  = "output"
	actionStop    = "stop"
	actionList    = "list"
	actionDiscard = "discard"
	actionPrune   = "prune"
	actionChoices = "start, status, output, stop, discard, prune, or list"
)

var actions = []string{
	actionStart,
	actionStatus,
	actionOutput,
	actionStop,
	actionDiscard,
	actionPrune,
	actionList,
}

type Args struct {
	Action  string `json:"action"`
	Name    string `json:"name"`
	Port    int    `json:"port,omitempty"`
	Command string `json:"command"`
	Intent  string `json:"intent,omitempty"`
	Respawn bool   `json:"respawn,omitempty"`
}

func New(
	manager *jobs.Manager,
	root *file.Root,
	buildPolicy func(context.Context) (sandbox.Policy, error),
	ports forward.Ports,
) tool.Tool {
	return build(manager, root, buildPolicy, ports, tool.Definition{
		Name:        "job",
		Description: description,
		Schema: schema(
			tool.Integer("port", "an optional TCP port inside the sandbox to forward to the user (for start)").Optional(),
		),
	})
}

func NewOnHost(
	manager *jobs.Manager,
	root *file.Root,
	buildPolicy func(context.Context) (sandbox.Policy, error),
) tool.Tool {
	return build(manager, root, buildPolicy, nil, tool.Definition{
		Name:        "job",
		Description: hostDescription,
		Schema:      schema(),
	})
}

func schema(extra ...tool.Parameter) tool.Schema {
	return slices.Concat(
		tool.Schema{
			tool.Enum("action", "what to do", actions...),
			tool.String("name", fmt.Sprintf("the job name; for start, use one short role such as 'check', not a specific compound such as 'cachecheck'—a live duplicate is automatically numbered, such as 'check-1'; 1–%d characters from [a-z0-9-] (for all actions except 'list', 'prune')", jobs.NameLengthLimit)).Optional(),
		},
		extra,
		tool.Schema{
			tool.String("command", "the command line (for action 'start'); if omitted, re-runs previous job by name").Optional(),
			tool.String("intent", "for action 'start', "+bash.IntentDescription).Optional(),
			tool.Boolean("respawn", fmt.Sprintf(
				"for action 'start', start the command again each time it exits, still notifying you; stop the job to end it. Respawning gives up after %d runs in a row each end within %s",
				jobs.QuickRunsTolerated,
				util.CompactDuration(jobs.QuickRunLimit),
			)).Optional(),
		},
	)
}

func build(
	manager *jobs.Manager,
	root *file.Root,
	buildPolicy func(context.Context) (sandbox.Policy, error),
	ports forward.Ports,
	definition tool.Definition,
) tool.Tool {
	return tool.Implement(
		definition,
		Describe,
	).
		Validate(validate).
		Exec(func(ctx context.Context, args Args) (string, tool.ToolCallMetrics, error) {
			return run(ctx, manager, root, buildPolicy, ports, args)
		})
}

const EndAdvice = "You are told when it ends, and woken if you have ended your turn, so end your turn rather than waiting or polling for it."

const description = "run a shell command in the background. For a server, bind a fixed port and pass it as `port`; the tool forwards that same number, reports the URL the user opens it at, and does not discover an ephemeral port. " + EndAdvice

const hostDescription = "run a shell command in the background, directly on the host. A server binds the host's own network, so the user opens it at the address it listens on. " + EndAdvice

func Describe(args Args) tool.CallRendering {
	rendering := describeAction(args)
	rendering.ReportsStatus = slices.Contains(statusActions, args.Action)
	switch args.Action {
	case actionStart:
		rendering.Intent = bash.SpokenIntent(args.Intent)
		rendering.Introduces = args.Name
	case actionStatus, actionOutput, actionStop, actionDiscard:
		rendering.Mentions = []string{args.Name}
	}
	return rendering
}

var statusActions = []string{actionStart, actionStatus, actionStop, actionDiscard, actionPrune}

func describeAction(args Args) tool.CallRendering {
	switch args.Action {
	case actionStart:
		subject := args.Name
		var emphasis tool.Emphasis
		if args.Port != 0 {
			subject += ":" + strconv.Itoa(args.Port)
			emphasis = tool.Emphasis{Kind: tool.EmphasisLead, Value: args.Name}
		}
		qualifier := ""
		if args.Respawn {
			qualifier = "respawning on exit"
		}
		if strings.TrimSpace(args.Command) == "" {
			return tool.CallRendering{Kind: "job_restart", Subject: subject, Qualifier: qualifier, Emphasis: emphasis}
		}
		return tool.CallRendering{
			Kind:         "job_start",
			Subject:      subject,
			Qualifier:    qualifier,
			Emphasis:     emphasis,
			Continuation: []tool.CallRendering{bash.DescribeCommand(args.Command)},
		}
	case actionList:
		return tool.CallRendering{Kind: "job_list", Subject: "jobs"}
	case actionPrune:
		return tool.CallRendering{Kind: "job_prune", Subject: "jobs"}
	case actionStatus, actionOutput, actionStop, actionDiscard:
		return tool.CallRendering{Kind: "job_" + args.Action, Subject: args.Name}
	}

	return tool.CallRendering{}
}

func validate(args Args) error {
	if !slices.Contains(actions, args.Action) {
		return fmt.Errorf("action must be %s (got %q)", actionChoices, args.Action)
	}

	if args.Action == actionStart && strings.TrimSpace(args.Intent) == "" {
		return errors.New(`intent is required to start a job`)
	}

	if args.Action != actionStart && args.Respawn {
		return errors.New(`respawn requires action="start"`)
	}

	if args.Action != actionStart && args.Port != 0 {
		return errors.New(`port requires action="start"`)
	}
	if args.Action == actionStart && args.Port != 0 {
		if _, err := getPort(args.Port); err != nil {
			return err
		}
	}

	if args.Action == actionList || args.Action == actionPrune {
		return nil
	}
	if strings.TrimSpace(args.Name) == "" {
		return errors.New("name is required")
	}
	if args.Action == actionStart {
		return jobs.ValidateName(args.Name)
	}

	return nil
}

func getPort(port int) (uint16, error) {
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("port must be 1–65535 (got %d)", port)
	}

	return uint16(port), nil
}

func run(
	ctx context.Context,
	manager *jobs.Manager,
	root *file.Root,
	buildPolicy func(context.Context) (sandbox.Policy, error),
	ports forward.Ports,
	args Args,
) (string, tool.ToolCallMetrics, error) {
	report, err := act(ctx, manager, root, buildPolicy, ports, args)
	if err != nil {
		return report, tool.ToolCallMetrics{}, err
	}

	return report, tool.GetMetrics(report), nil
}

func act(
	ctx context.Context,
	manager *jobs.Manager,
	root *file.Root,
	buildPolicy func(context.Context) (sandbox.Policy, error),
	ports forward.Ports,
	args Args,
) (string, error) {
	switch args.Action {
	case actionStart:
		command := strings.TrimSpace(args.Command)
		if command == "" {
			rememberedCommand, isRemembered := manager.RememberedCommand(args.Name)
			if !isRemembered {
				return "", errors.New("command required; this job has no remembered command")
			}
			command = rememberedCommand
		}

		policy, err := buildPolicy(ctx)
		if err != nil {
			return "", err
		}

		start := manager.Start
		if args.Respawn {
			start = manager.StartRespawning
		}

		snapshot, err := start(ctx, args.Name, root.Name(), command, policy)
		if err != nil {
			return "", err
		}

		report := snapshot.Describe()
		if args.Port == 0 {
			return report, nil
		}

		port, portErr := getPort(args.Port)
		switch {
		case portErr != nil:
			err = portErr
		case ports == nil:
			err = errors.New("port forwarding is unavailable")
		default:
			var publication string
			publication, err = forward.Publish(ports, port, snapshot.Name)
			if err == nil {
				return report + "\n" + publication, nil
			}
		}

		if _, discardErr := manager.Discard(snapshot.Name); discardErr != nil {
			return "", errors.Join(
				fmt.Errorf("could not forward port %d for job %q: %w", args.Port, snapshot.Name, err),
				fmt.Errorf("could not discard job %q after the forward failed: %w", snapshot.Name, discardErr),
			)
		}

		return "", fmt.Errorf(
			"could not forward port %d for job %q, which was stopped and discarded: %w",
			args.Port,
			snapshot.Name,
			err,
		)

	case actionStatus:
		snapshot, err := manager.Status(args.Name)
		if err != nil {
			return "", err
		}

		return snapshot.Describe(), nil

	case actionOutput:
		output, snapshot, err := manager.Output(args.Name)
		if err != nil {
			return "", err
		}

		return jobs.Report(snapshot.DescribeWith(output), output, snapshot.DroppedBytes), nil

	case actionDiscard:
		discardedJob, err := manager.Discard(args.Name)
		if err != nil {
			return "", err
		}

		return discardedJob.Describe() + ", and discarded", nil

	case actionPrune:
		prunedNames := manager.PruneFinished()
		if len(prunedNames) == 0 {
			return "there are no finished jobs to prune.", nil
		}

		return "pruned " + strings.Join(prunedNames, ", ") + ".", nil

	case actionStop:
		snapshot, err := manager.Stop(args.Name)
		if err != nil {
			return "", err
		}

		return snapshot.Describe(), nil

	default:
		return listing(manager.List()), nil
	}
}

func listing(snapshots []jobs.Snapshot) string {
	if len(snapshots) == 0 {
		return "no jobs have been started in this session."
	}

	lines := make([]string, 0, len(snapshots))
	for _, snapshot := range snapshots {
		lines = append(lines, snapshot.Describe()+" — "+snapshot.Command)
	}

	return strings.Join(lines, "\n")
}
