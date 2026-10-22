package commands

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/table"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/tasks"
)

const pruneTaskAction = "prune"

var namedTaskActions = []string{"status", "output", "stop", "restart", "discard"}

type Tasks struct {
	List          func() []tasks.Snapshot
	Status        func(name string) (tasks.Snapshot, error)
	Output        func(name string) (tasks.Snapshot, error)
	Stop          func(name string, reason string) error
	Restart       func(name string) (tasks.Snapshot, error)
	Discard       func(name string) (tasks.Snapshot, error)
	PruneFinished func() []string
	DescribeCall  func(snapshot tasks.Snapshot) string
}

func (self Tasks) isConfigured() bool {
	return self.List != nil && self.Status != nil && self.Output != nil && self.Stop != nil &&
		self.Restart != nil && self.Discard != nil && self.PruneFinished != nil && self.DescribeCall != nil
}

func taskCommands(managedTasks Tasks) []slash.Command {
	return []slash.Command{
		tasksCommand(managedTasks),
		taskCommand(managedTasks),
	}
}

func tasksCommand(managedTasks Tasks) slash.Command {
	return slash.Command{
		Name:        "tasks",
		Description: "list this session's background tasks",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			if arguments.Text != "" {
				return slash.Usage()
			}
			listing := managedTasks.List()
			if len(listing) == 0 {
				context.Notice("No background tasks.")
				return nil
			}
			context.PlainNoticeListing(formatTasks(listing, managedTasks.DescribeCall))

			return nil
		},
	}
}

func taskCommand(managedTasks Tasks) slash.Command {
	return slash.Command{
		Name:        "task",
		Description: "inspect, stop, restart, or discard a background task, or prune finished ones",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			action, name, isNamed := strings.Cut(arguments.Text, " ")
			name = strings.TrimSpace(name)
			if action == pruneTaskAction {
				if name != "" {
					return slash.Usage()
				}
				return pruneEveryFinishedTask(context, managedTasks)
			}
			if !isNamed || name == "" {
				return slash.Usage()
			}

			return runTaskAction(context, managedTasks, action, name)
		},
	}.
		WithArgumentUsage("{status|output|stop|restart|discard} <name> | prune").
		WithArgumentCompletion(func(writtenArguments []string, partial string) []string {
			switch {
			case len(writtenArguments) == 0:
				return slash.MatchingPrefixes(partial, append(slices.Clone(namedTaskActions), pruneTaskAction))
			case len(writtenArguments) == 1 && slices.Contains(namedTaskActions, writtenArguments[0]):
				return slash.MatchingPrefixes(partial, taskNames(managedTasks.List()))
			default:
				return nil
			}
		}).
		WithCompletionUsage("<action>")
}

func taskNames(listing []tasks.Snapshot) []string {
	names := make([]string, 0, len(listing))
	for _, snapshot := range listing {
		names = append(names, snapshot.ID)
	}
	return names
}

func runTaskAction(context slash.Context, managedTasks Tasks, action string, name string) error {
	switch action {
	case "status":
		snapshot, err := managedTasks.Status(name)
		if err != nil {
			return err
		}
		context.Notice(describeTask(snapshot))

		return nil

	case "output":
		snapshot, err := managedTasks.Output(name)
		if err != nil {
			return err
		}
		context.Notice(withTaskOutput(snapshot))

		return nil

	case "stop":
		snapshot, err := managedTasks.Status(name)
		if err != nil {
			return err
		}
		if !isLiveTask(snapshot) {
			context.Notice(describeTask(snapshot))

			return nil
		}
		if err := managedTasks.Stop(name, caps.TaskStopReason(caps.TaskStoppedByUserEvent(name))); err != nil {
			return err
		}
		context.Emit(caps.TaskStoppedByUserEvent(name))

		return nil

	case "restart":
		snapshot, err := managedTasks.Restart(name)
		if err != nil {
			return err
		}
		context.Success(fmt.Sprintf("Restarted %s as run %d.", snapshot.ID, snapshot.Run))

		return nil

	case "discard":
		snapshot, err := managedTasks.Status(name)
		if err != nil {
			return err
		}
		discardedTask, err := managedTasks.Discard(name)
		if err != nil {
			return err
		}
		if isLiveTask(snapshot) {
			context.Emit(caps.TaskStoppedByUserEvent(name))
		}
		context.Success("Discarded " + describeTask(discardedTask) + ".")

		return nil

	default:
		return slash.Usage()
	}
}

func pruneEveryFinishedTask(context slash.Context, managedTasks Tasks) error {
	prunedNames := managedTasks.PruneFinished()
	if len(prunedNames) == 0 {
		context.Notice("No finished tasks to prune.")

		return nil
	}

	context.Success("Pruned " + strings.Join(prunedNames, ", ") + ".")

	return nil
}

func formatTasks(listing []tasks.Snapshot, describeCall func(tasks.Snapshot) string) string {
	rows := make([][]string, len(listing))
	for index, snapshot := range listing {
		rows[index] = []string{
			style.Subject(strutil.Flatten(snapshot.ID)),
			styleTaskOutcome(snapshot),
			describeCall(snapshot),
		}
	}

	taskTable := table.New(
		table.Column{},
		table.Column{},
		table.Column{},
	).Fit(rows)
	lines := make([]string, len(rows))
	for index, row := range rows {
		lines[index] = "  " + taskTable.Row(row, 0)
	}

	return style.Info("Background tasks:") + "\n" + strings.Join(lines, "\n")
}

func styleTaskOutcome(snapshot tasks.Snapshot) string {
	outcome := strutil.Flatten(taskOutcome(snapshot, false))
	switch snapshot.State {
	case tasks.Done, tasks.Stopped:
		return style.Dim(outcome)
	case tasks.Failed, tasks.TimedOut:
		return style.Failure(outcome)
	case tasks.Queued, tasks.Running, tasks.Stopping:
		return style.Normal(outcome)
	}

	return style.Normal(outcome)
}

func isLiveTask(snapshot tasks.Snapshot) bool {
	return snapshot.State == tasks.Queued || snapshot.State == tasks.Running || snapshot.State == tasks.Stopping
}

func describeTask(snapshot tasks.Snapshot) string {
	return snapshot.ID + ": " + taskOutcome(snapshot, false)
}

func taskOutcome(snapshot tasks.Snapshot, isSilent bool) string {
	stateWithDuration := strings.ReplaceAll(string(snapshot.State), "_", " ")
	switch {
	case snapshot.StartedAt.IsZero():
	case isLiveTask(snapshot):
		stateWithDuration += " for " + util.CompactDuration(time.Since(snapshot.StartedAt).Round(time.Second))
	case !snapshot.EndedAt.IsZero():
		stateWithDuration += " after " + util.CompactDuration(snapshot.EndedAt.Sub(snapshot.StartedAt).Round(time.Second))
	}
	if isSilent {
		stateWithDuration += " with no output"
	}

	parts := []string{stateWithDuration}
	if snapshot.Run > 1 {
		parts = append(parts, fmt.Sprintf("run %d", snapshot.Run))
	}
	if snapshot.IsRespawning {
		parts = append(parts, "respawns on exit")
	}
	if snapshot.IsRespawnLimitReached {
		parts = append(parts, tasks.RespawnLimitReason())
	}
	if snapshot.Failure != "" {
		parts = append(parts, snapshot.Failure)
	}

	return strings.Join(parts, ", ")
}

func withTaskOutput(snapshot tasks.Snapshot) string {
	lines := []string{}
	for _, previous := range snapshot.Previous {
		line := fmt.Sprintf("run %d: %s", previous.Run, previous.State)
		if previous.Failure != "" {
			line += ", " + previous.Failure
		}
		lines = append(lines, line)
		if text := strings.TrimRight(previous.Output, "\n"); strings.TrimSpace(text) != "" {
			lines = append(lines, text)
		}
	}
	text := strings.TrimRight(snapshot.Output, "\n")
	if strings.TrimSpace(text) == "" {
		return strings.Join(append([]string{snapshot.ID + ": " + taskOutcome(snapshot, true)}, lines...), "\n")
	}

	return strings.Join(append(append([]string{describeTask(snapshot)}, lines...), text), "\n")
}
