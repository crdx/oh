package commands

import (
	"fmt"
	"slices"
	"strings"

	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/app/style"
)

type Subagents struct {
	List func() []SubagentListing
	Show func(name string) error
}

type SubagentListing struct {
	Name      string
	IsMissing bool
}

func (self Subagents) isConfigured() bool {
	return self.List != nil && self.Show != nil
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
		Description: "list this session's subagents",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			if arguments.Text != "" {
				return slash.Usage()
			}
			listing := subagents.List()
			if len(listing) == 0 {
				context.Notice("No subagents.")
				return nil
			}
			context.PlainNoticeListing(formatSubagents(listing))
			return nil
		},
	}
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

func formatSubagents(listing []SubagentListing) string {
	lines := make([]string, len(listing))
	for index, child := range listing {
		lines[index] = "  " + child.Name
		if child.IsMissing {
			lines[index] += " " + style.Subtle("(its conversation is gone)")
		}
	}
	return style.Info("Subagents:") + "\n" + strings.Join(lines, "\n")
}
