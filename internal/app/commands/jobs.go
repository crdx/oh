package commands

import (
	"strings"

	"crdx.org/oh/internal/app/call"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/table"
	"crdx.org/oh/internal/jobs"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/toolbox/bash"
)

type Jobs struct {
	List          func() []jobs.Snapshot
	Status        func(name string) (jobs.Snapshot, error)
	Output        func(name string) (string, jobs.Snapshot, error)
	Stop          func(name string) (agent.Event, error)
	Discard       func(name string) (jobs.Snapshot, error)
	PruneFinished func() []string
}

func (self Jobs) isConfigured() bool {
	return self.List != nil && self.Status != nil && self.Output != nil &&
		self.Stop != nil && self.Discard != nil && self.PruneFinished != nil
}

func jobCommands(managedJobs Jobs) []slash.Command {
	return []slash.Command{
		jobsCommand(managedJobs),
		jobCommand(managedJobs),
	}
}

func jobsCommand(managedJobs Jobs) slash.Command {
	return slash.Command{
		Name:        "jobs",
		Description: "list this session's background jobs",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			if arguments.Text != "" {
				return slash.Usage()
			}
			context.NoticeListing(formatJobs(managedJobs.List()))

			return nil
		},
	}
}

func jobCommand(managedJobs Jobs) slash.Command {
	return slash.Command{
		Name:        "job",
		Description: "inspect, stop, or discard a background job, or prune finished ones",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			action, name, isNamed := strings.Cut(arguments.Text, " ")
			name = strings.TrimSpace(name)
			if action == "prune" {
				return pruneEveryFinishedJob(context, managedJobs)
			}
			if !isNamed || name == "" {
				return slash.Usage()
			}

			return runJobAction(context, managedJobs, action, name)
		},
	}.
		WithArguments("status", "output", "stop", "discard", "prune").
		WithArgumentUsage("{status|output|stop|discard} <name> | prune").
		WithCompletionUsage("<action>")
}

func runJobAction(context slash.Context, managedJobs Jobs, action string, name string) error {
	switch action {
	case "status":
		snapshot, err := managedJobs.Status(name)
		if err != nil {
			return err
		}
		context.Notice(snapshot.Describe())

		return nil

	case "output":
		text, snapshot, err := managedJobs.Output(name)
		if err != nil {
			return err
		}
		context.Notice(withJobOutput(snapshot.Describe(), text))

		return nil

	case "discard":
		discardedJob, err := managedJobs.Discard(name)
		if err != nil {
			return err
		}
		context.Success(discardedJob.Describe() + ", and discarded.")

		return nil

	case "stop":
		event, err := managedJobs.Stop(name)
		if err != nil {
			return err
		}
		context.Emit(event)

		return nil

	default:
		return slash.Usage()
	}
}

func pruneEveryFinishedJob(context slash.Context, managedJobs Jobs) error {
	prunedNames := managedJobs.PruneFinished()
	if len(prunedNames) == 0 {
		context.Notice("No finished jobs to prune.")

		return nil
	}

	context.Success("Pruned " + strings.Join(prunedNames, ", ") + ".")

	return nil
}

func formatJobs(listing []jobs.Snapshot) string {
	if len(listing) == 0 {
		return "No background jobs."
	}

	rows := make([][]string, len(listing))
	for index, snapshot := range listing {
		rows[index] = []string{
			style.Subject(strutil.Flatten(snapshot.Name)),
			styleJobOutcome(snapshot),
			call.LabelForRendering(bash.DescribeCommand(snapshot.Command)).Render(),
		}
	}

	jobTable := table.New(
		table.Column{},
		table.Column{},
		table.Column{},
	).Fit(rows)
	lines := make([]string, len(rows))
	for index, row := range rows {
		lines[index] = "  " + jobTable.Row(row, 0)
	}

	return "Background jobs:\n" + strings.Join(lines, "\n")
}

func styleJobOutcome(snapshot jobs.Snapshot) string {
	outcome := strutil.Flatten(snapshot.Outcome())
	switch snapshot.State {
	case jobs.StateComplete:
		return style.Dim(outcome)
	case jobs.StateFailed:
		return style.Failure(outcome)
	case jobs.StateStarting, jobs.StateRunning, jobs.StateStopping, jobs.StateStopped, jobs.StateEnded:
		return style.Normal(outcome)
	}

	return style.Normal(outcome)
}

func withJobOutput(status string, text string) string {
	if strings.TrimSpace(text) == "" {
		return status + " with no output"
	}

	return status + "\n" + strings.TrimRight(text, "\n")
}
