package job

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/jobs"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/internal/stop"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/bash"
	"crdx.org/oh/pkg/toolbox/forward"
)

const (
	actionStart   = "start"
	actionStatus  = "status"
	actionOutput  = "output"
	actionWait    = "wait"
	actionStop    = "stop"
	actionList    = "list"
	actionDiscard = "discard"
	actionPrune   = "prune"
	waitForAny    = "any"
	waitForAll    = "all"
	actionChoices = "start, status, output, wait, stop, discard, prune, or list"
)

const waitLimit = 4*time.Minute + 30*time.Second

var actions = []string{
	actionStart,
	actionStatus,
	actionOutput,
	actionWait,
	actionStop,
	actionDiscard,
	actionPrune,
	actionList,
}

type Args struct {
	Action      string   `json:"action"`
	Name        string   `json:"name"`
	Port        int      `json:"port,omitempty"`
	Names       []string `json:"names,omitempty"`
	WaitFor     string   `json:"wait_for,omitempty"`
	WaitSeconds int      `json:"wait_seconds,omitempty"`
	Command     string   `json:"command"`
	Intent      string   `json:"intent,omitempty"`
}

func New(
	manager *jobs.Manager,
	root *file.Root,
	buildPolicy func(context.Context) (sandbox.Policy, error),
	ports forward.Ports,
) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "job",
			Description: description,
			Schema: tool.Schema{
				tool.Enum("action", "what to do", actions...),
				tool.String("name", fmt.Sprintf("the job name; for start, use one short role such as 'check', not a specific compound such as 'cachecheck'—a live duplicate is automatically numbered, such as 'check-1'; 1–%d characters from [a-z0-9-] (for all actions except 'list', 'prune')", jobs.NameLengthLimit)).Optional(),
				tool.Integer("port", "an optional TCP port inside the sandbox to forward to the user (for start)").Optional(),
				tool.StringArray("names", "the job names to watch for wait").Optional(),
				tool.Enum("wait_for", "whether wait returns after any or all watched jobs end", waitForAny, waitForAll).Optional(),
				tool.Integer("wait_seconds", fmt.Sprintf("how many seconds to wait at most — max %s (default)", util.CompactDuration(waitLimit))).Optional(),
				tool.String("command", "the command line (for action 'start'); if omitted, re-runs previous job by name").Optional(),
				tool.String("intent", "for action 'start', "+bash.IntentDescription).Optional(),
			},
		},
		Describe,
	).
		Validate(validate).
		TakesAtMost(getTimeLimit).
		Exec(func(ctx context.Context, args Args) (string, tool.ToolCallMetrics, error) {
			return run(ctx, manager, root, buildPolicy, ports, args)
		})
}

const description = "run a shell command in the background. For a server, bind a fixed port and pass it as `port`; the tool forwards that same number, reports the URL the user opens it at, and does not discover an ephemeral port. In an interactive session you will be notified automatically when it finishes; in a non-interactive session you will not."

func Describe(args Args) tool.CallRendering {
	rendering := describeAction(args)
	switch args.Action {
	case actionStart:
		rendering.Intent = bash.SpokenIntent(args.Intent)
		rendering.Introduces = args.Name
	case actionWait:
		rendering.Mentions = getWaitNames(args)
	case actionStatus, actionOutput, actionStop, actionDiscard:
		rendering.Mentions = []string{args.Name}
	}
	return rendering
}

func describeAction(args Args) tool.CallRendering {
	switch args.Action {
	case actionStart:
		subject := args.Name
		var emphasis tool.Emphasis
		if args.Port != 0 {
			subject += ":" + strconv.Itoa(args.Port)
			emphasis = tool.Emphasis{Kind: tool.EmphasisLead, Value: args.Name}
		}
		if strings.TrimSpace(args.Command) == "" {
			return tool.CallRendering{Kind: "job_restart", Subject: subject, Emphasis: emphasis}
		}
		return tool.CallRendering{
			Kind:         "job_start",
			Subject:      subject,
			Emphasis:     emphasis,
			Continuation: []tool.CallRendering{bash.DescribeCommand(args.Command)},
		}
	case actionWait:
		qualifier := ""
		if args.WaitSeconds > 0 {
			qualifier = "for up to " + util.CompactDuration(getWaitLimit(args))
		}
		separator := " || "
		if getWaitFor(args) == waitForAll {
			separator = " && "
		}
		kind := "job_wait_any"
		if getWaitFor(args) == waitForAll {
			kind = "job_wait_all"
		}
		return tool.CallRendering{
			Kind:      kind,
			Subject:   strings.Join(getWaitNames(args), separator),
			Qualifier: qualifier,
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

	if args.Action != actionStart && args.Port != 0 {
		return errors.New(`port requires action="start"`)
	}
	if args.Action == actionStart && args.Port != 0 {
		if _, err := getPort(args.Port); err != nil {
			return err
		}
	}

	if args.Action == actionWait {
		return validateWait(args)
	}

	if len(args.Names) > 0 {
		return errors.New(`names requires action="wait"`)
	}
	if args.WaitFor != "" {
		return errors.New(`wait_for requires action="wait"`)
	}
	if args.WaitSeconds != 0 {
		return errors.New(`wait_seconds requires action="wait"`)
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

func validateWait(args Args) error {
	if strings.TrimSpace(args.Name) != "" && len(args.Names) > 0 {
		return errors.New("name and names cannot both be set")
	}

	names := getWaitNames(args)
	if len(names) == 0 {
		return errors.New("wait requires name or names")
	}

	knownNames := make(map[string]bool, len(names))
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return errors.New("wait names must be non-empty")
		}
		if knownNames[name] {
			return fmt.Errorf("duplicate name %q", name)
		}
		knownNames[name] = true
	}

	if !slices.Contains([]string{waitForAny, waitForAll}, getWaitFor(args)) {
		return errors.New("wait_for must be any or all")
	}

	if args.WaitSeconds < 0 {
		return errors.New("wait_seconds must be positive")
	}

	return nil
}

func getWaitNames(args Args) []string {
	if len(args.Names) > 0 {
		return args.Names
	}
	if strings.TrimSpace(args.Name) == "" {
		return nil
	}

	return []string{args.Name}
}

func getTimeLimit(args Args) time.Duration {
	if args.Action != actionWait {
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

func getWaitFor(args Args) string {
	if args.WaitFor == "" {
		return waitForAny
	}

	return args.WaitFor
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

		snapshot, err := manager.Start(ctx, args.Name, root.Name(), command, policy)
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

		return jobs.Report(snapshot.Describe(), output, snapshot.DroppedBytes), nil

	case actionWait:
		return waited(ctx, manager, getWaitNames(args), getWaitFor(args), getWaitLimit(args))

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

func waited(
	ctx context.Context,
	manager *jobs.Manager,
	names []string,
	waitFor string,
	limit time.Duration,
) (string, error) {
	waitContext, stopWaiting := context.WithTimeout(ctx, limit)
	defer stopWaiting()

	completedNames, err := waitForJobs(waitContext, manager, names, waitFor)
	if err == nil {
		if waitFor == waitForAll {
			return getReports(manager, names)
		}
		return getReports(manager, completedNames)
	}
	if ctx.Err() != nil {
		return "", stop.Error(ctx, "")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		return "", err
	}
	return getTimeoutReport(manager, names, waitFor, limit)
}

func waitForJobs(ctx context.Context, manager *jobs.Manager, names []string, waitFor string) ([]string, error) {
	remainingNames := slices.Clone(names)
	completedNames := make([]string, 0, len(names))

	for len(remainingNames) > 0 {
		completedName, err := manager.Wait(ctx, remainingNames)
		if err != nil {
			return completedNames, err
		}
		completedNames = append(completedNames, completedName)
		if waitFor == waitForAny {
			return completedNames, nil
		}
		remainingNames = slices.DeleteFunc(remainingNames, func(name string) bool { return name == completedName })
	}

	return completedNames, nil
}

func getReports(manager *jobs.Manager, names []string) (string, error) {
	reports := make([]string, 0, len(names))
	for _, name := range names {
		output, snapshot, err := manager.Output(name)
		if err != nil {
			return "", err
		}
		reports = append(reports, jobs.Report(snapshot.Describe(), output, snapshot.DroppedBytes))
	}

	return strings.Join(reports, "\n\n"), nil
}

func getTimeoutReport(manager *jobs.Manager, names []string, waitFor string, limit time.Duration) (string, error) {
	reports, err := getReports(manager, names)
	if err != nil {
		return "", err
	}

	note, isSaid := timeoutNote(manager, names, waitFor, limit)
	if !isSaid {
		return reports, nil
	}

	return reports + "\n\n" + note, nil
}

func timeoutNote(
	manager *jobs.Manager,
	names []string,
	waitFor string,
	limit time.Duration,
) (string, bool) {
	if len(names) == 1 {
		snapshot, err := manager.Status(names[0])
		if err != nil || !snapshot.IsLive() {
			return "", false
		}

		return fmt.Sprintf(
			"note: the wait gave up after %s, and the job is still running.",
			util.CompactDuration(limit),
		), true
	}

	quantity := "any watched job"
	if waitFor == waitForAll {
		quantity = "all watched jobs"
	}

	return fmt.Sprintf(
		"note: the wait gave up after %s before %s ended.",
		util.CompactDuration(limit),
		quantity,
	), true
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
