package job

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"crdx.org/io/internal/file"
	"crdx.org/io/internal/jobs"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/internal/stop"
	"crdx.org/io/internal/util"
	"crdx.org/io/pkg/tool"
	"crdx.org/io/pkg/toolbox/bash"
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
	Names       []string `json:"names,omitempty"`
	WaitFor     string   `json:"wait_for,omitempty"`
	WaitSeconds int      `json:"wait_seconds,omitempty"`
	Command     string   `json:"command"`
}

func New(
	manager *jobs.Manager,
	root *file.Root,
	buildPolicy func(context.Context) (sandbox.Policy, error),
	doesWake bool,
) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "job",
			Description: description(doesWake),
			Schema: tool.Schema{
				tool.Enum("action", "what to do", actions...),
				tool.String("name", fmt.Sprintf("the job name; 1–%d characters from [a-z0-9-] (for all actions except 'list', 'prune')", jobs.NameLengthLimit)).Optional(),
				tool.StringArray("names", "the job names to watch for wait").Optional(),
				tool.Enum("wait_for", "whether wait returns after any or all watched jobs end", waitForAny, waitForAll).Optional(),
				tool.Integer("wait_seconds", fmt.Sprintf("how many seconds to wait at most — max %d (default)", int(waitLimit.Seconds()))).Optional(),
				tool.String("command", "the command line (for action 'start'); if omitted, re-runs previous job by name").Optional(),
			},
		},
		Describe,
	).
		Validate(validate).
		ContinuesWith(describeContinuation).
		TakesAtMost(getTimeLimit).
		Exec(func(ctx context.Context, args Args) (string, tool.ToolCallMetrics, error) {
			return run(ctx, manager, root, buildPolicy, args)
		})
}

func description(doesWake bool) string {
	firstSentence := "run a shell command in the background."
	if doesWake {
		return firstSentence + " You will be notified automatically when it finishes."
	}

	return firstSentence + " You will not be notified automatically when it finishes."
}

func Describe(args Args) (string, string) {
	if args.Action == actionStart && strings.TrimSpace(args.Command) != "" {
		return args.Name, ""
	}
	if args.Action == actionWait {
		return strings.Join(getWaitNames(args), ", "), args.Action
	}

	return args.Name, args.Action
}

func describeContinuation(args Args) []tool.CallRendering {
	if args.Action != actionStart || strings.TrimSpace(args.Command) == "" {
		return nil
	}

	return []tool.CallRendering{bash.DescribeCommand(args.Command)}
}

func validate(args Args) error {
	if !slices.Contains(actions, args.Action) {
		return fmt.Errorf("action must be %s (got %q)", actionChoices, args.Action)
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
	args Args,
) (string, tool.ToolCallMetrics, error) {
	report, err := act(ctx, manager, root, buildPolicy, args)
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

		return snapshot.Describe(), nil

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
