package commands

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/table"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/internal/util/strutil"
)

type Subagents struct {
	List     func() []SubagentListing
	Progress func() []SubagentProgress
	Show     func(name string) error
}

type SubagentProgress struct {
	Name       string
	State      string
	IsLive     bool
	Model      string
	IsTimed    bool
	Duration   time.Duration
	Intent     string
	ToolCalls  int
	LastIntent string
}

type SubagentListing struct {
	Name      string
	IsMissing bool
}

func (self Subagents) isConfigured() bool {
	return self.List != nil && self.Progress != nil && self.Show != nil
}

func subagentCommands(subagents Subagents) []slash.Command {
	return []slash.Command{
		subagentsCommand(subagents),
		subagentCommand(subagents),
	}
}

func subagentsCommand(subagents Subagents) slash.Command {
	return slash.Command{
		Name:        "subs",
		Description: "list this session's running and latest finished subagents",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			if arguments.Text != "" && arguments.Text != subagentsAllArgument {
				return slash.Usage()
			}
			progress := subagents.Progress()
			if len(progress) == 0 {
				context.Notice("No subagents.")
				return nil
			}
			shownFinished := shownFinishedSubagents
			if arguments.Text == subagentsAllArgument {
				shownFinished = len(progress)
			}
			context.PlainNoticeListing(formatSubagents(progress, shownFinished))
			return nil
		},
	}.
		WithArguments(subagentsAllArgument).
		WithArgumentUsage("[all]")
}

const catSubagentAction = "cat"

func subagentCommand(subagents Subagents) slash.Command {
	return slash.Command{
		Name:        "sub",
		Description: "read a subagent's conversation",
		Run: func(_ slash.Context, arguments slash.Arguments) error {
			action, name, isNamed := strings.Cut(arguments.Text, " ")
			name = strings.TrimSpace(name)
			if action != catSubagentAction || !isNamed || name == "" || strings.ContainsAny(name, " \t") {
				return slash.Usage()
			}
			if !slices.ContainsFunc(subagents.List(), func(child SubagentListing) bool { return child.Name == name }) {
				return fmt.Errorf("no subagent named %s", name)
			}
			return subagents.Show(name)
		},
	}.
		WithArgumentUsage(catSubagentAction + " <name>").
		WithArgumentCompletion(func(writtenArguments []string, partial string) []string {
			switch {
			case len(writtenArguments) == 0:
				return slash.MatchingPrefixes(partial, []string{catSubagentAction})
			case len(writtenArguments) == 1 && writtenArguments[0] == catSubagentAction:
				var names []string
				for _, child := range subagents.List() {
					if !child.IsMissing {
						names = append(names, child.Name)
					}
				}
				return slash.MatchingPrefixes(partial, names)
			default:
				return nil
			}
		}).
		WithCompletionUsage("<action>")
}

const (
	shownFinishedSubagents = 10
	subagentsAllArgument   = "all"
)

var finishedStates = []string{"done", "failed", "stopped", "ended"}

func formatSubagents(progress []SubagentProgress, shownFinished int) string {
	runningChildren := slices.DeleteFunc(slices.Clone(progress), func(child SubagentProgress) bool { return !child.IsLive })
	finishedChildren := slices.DeleteFunc(slices.Clone(progress), func(child SubagentProgress) bool { return child.IsLive })
	earlierChildren := finishedChildren[:max(0, len(finishedChildren)-shownFinished)]
	finishedChildren = finishedChildren[len(earlierChildren):]
	var lines []string
	if len(runningChildren) > 0 {
		lines = append(lines, style.Info("Running subagents:"))
		lines = append(lines, subagentRows(runningChildren)...)
	}
	if len(finishedChildren) > 0 {
		lines = append(lines, style.Info("Finished subagents:"))
		lines = append(lines, subagentRows(finishedChildren)...)
	}
	if summary := earlierSummary(earlierChildren); summary != "" {
		lines = append(lines, "  "+style.Dim(summary))
	}
	return strings.Join(lines, "\n")
}

func earlierSummary(earlierChildren []SubagentProgress) string {
	if len(earlierChildren) == 0 {
		return ""
	}
	counts := map[string]int{}
	for _, child := range earlierChildren {
		counts[child.State]++
	}
	var parts []string
	for _, state := range finishedStates {
		if counts[state] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[state], state))
		}
	}
	return fmt.Sprintf("and %d earlier: %s; /subs all lists them.", len(earlierChildren), strings.Join(parts, ", "))
}

func subagentRows(children []SubagentProgress) []string {
	rows := make([][]string, len(children))
	for index, child := range children {
		rows[index] = []string{
			style.Subject(strutil.Flatten(child.Name)),
			style.Normal(subagentOutcome(child)),
			style.Dim(strutil.Flatten(child.Model)),
			style.Reasoning(strutil.Flatten(child.Intent)),
			style.Dim(subagentActivity(child)),
		}
	}
	subagentTable := table.New(
		table.Column{},
		table.Column{},
		table.Column{},
		table.Column{},
		table.Column{},
	).Fit(rows)
	lines := make([]string, len(rows))
	for index, row := range rows {
		lines[index] = strings.TrimRight("  "+subagentTable.Row(row, 0), " ")
	}
	return lines
}

func subagentOutcome(child SubagentProgress) string {
	if !child.IsTimed {
		return child.State
	}
	duration := util.CompactDuration(child.Duration.Round(time.Second))
	if child.IsLive {
		return child.State + " for " + duration
	}
	return child.State + " after " + duration
}

func subagentActivity(child SubagentProgress) string {
	if !child.IsTimed {
		return ""
	}
	activity := util.Plural(child.ToolCalls, "tool call")
	if child.IsLive && child.LastIntent != "" {
		activity += ", last: " + strutil.Flatten(child.LastIntent)
	}
	return activity
}
