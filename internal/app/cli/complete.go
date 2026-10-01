package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/sessions"
	"crdx.org/oh/internal/app/work"
)

const completeFlag = "--complete"

const (
	completeOption   = "option"
	completeModel    = "model"
	completeEffort   = "effort"
	completeSession  = "session"
	completeProvider = "provider"
	completeCaps     = "caps"
	completeTool     = "tool"
)

func completionRequest(args []string) (string, string, bool) {
	if len(args) == 0 || args[0] != completeFlag {
		return "", "", false
	}

	rest := args[1:]
	if len(rest) == 0 {
		return "", "", false
	}

	var word string
	if len(rest) > 1 {
		word = rest[1]
	}

	return rest[0], word, true
}

type Sources struct {
	ModelCachePath string
	SessionsDir    string
	ToolNames      []string
	CustomCapFlags string
	IsLoggedIn     func(providerName string) bool
}

func Complete(args []string, sources Sources) ([]string, bool) {
	kind, word, isWanted := completionRequest(args)
	if !isWanted {
		return nil, false
	}

	return completions(kind, word, sources), true
}

func WriteCompletions(out io.Writer, args []string, sources Sources) bool {
	completions, isWanted := Complete(args, sources)
	if !isWanted {
		return false
	}

	for _, completion := range completions {
		_, _ = fmt.Fprintln(out, completion)
	}
	return true
}

func completions(kind string, word string, sources Sources) []string {
	switch kind {
	case completeOption:
		return optionCompletions(word, usageOptions(usage))
	case completeModel:
		return model.SelectionsMatching(word, model.SignedInChoices(sources.ModelCachePath, sources.IsLoggedIn))
	case completeEffort:
		return effortCompletions(word, model.SignedInChoices(sources.ModelCachePath, sources.IsLoggedIn))
	case completeSession:
		return withPrefix(word, sessionNames(sources.SessionsDir))
	case completeProvider:
		return withPrefix(word, model.LoginProviderNames())
	case completeCaps:
		return withPrefix(word, capsCompletions(sources.CustomCapFlags))
	case completeTool:
		return withPrefix(word, sources.ToolNames)
	default:
		return nil
	}
}

func withPrefix(word string, candidates []string) []string {
	var matches []string

	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, word) {
			matches = append(matches, candidate)
		}
	}

	return matches
}

func optionCompletions(word string, options []string) []string {
	if word == "" {
		return withPrefix("--", options)
	}

	return withPrefix(word, options)
}

func usageOptions(text string) []string {
	var options []string

	for line := range strings.SplitSeq(text, "\n") {
		for field := range strings.FieldsSeq(line) {
			name := strings.TrimSuffix(field, ",")
			if !strings.HasPrefix(name, "-") || name == "-" {
				break
			}
			options = append(options, name)
		}
	}

	slices.Sort(options)

	return options
}

func effortCompletions(word string, choices []model.Choice) []string {
	modelQuery, effortQuery, _ := strings.Cut(word, "@")

	var efforts []string

	for _, choice := range model.RankedChoices(modelQuery, choices) {
		for _, effort := range model.EffortsMatching(effortQuery, choice.EffortLevels) {
			if !slices.Contains(efforts, effort) {
				efforts = append(efforts, effort)
			}
		}
	}

	return efforts
}

func sessionNames(directory string) []string {
	workspace, err := work.Current()
	if err != nil {
		return nil
	}

	names, err := sessions.NamesInWorkspace(directory, workspace)
	if err != nil {
		return nil
	}

	return names
}

func capsCompletions(customFlags ...string) []string {
	flags := caps.AllFlags
	if len(customFlags) > 0 {
		flags += customFlags[0]
	}
	sets := make([]string, 0, len(flags))

	for i := range flags {
		sets = append(sets, flags[:i+1])
	}

	return sets
}
