package commands

import (
	"slices"
	"strings"

	"crdx.org/oh/internal/app/cli"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/util/strutil"
)

const (
	sessionArgumentUsage   = "[-m <model>] [-c <flags>] [-t <tool>]... [--yolo] [-f <session>]"
	sessionCompletionUsage = "[<options>]"
	optionMarker           = "-"
)

type sessionOptionKind int

const (
	modelOption sessionOptionKind = iota
	capsOption
	toolOption
	yoloOption
	sourceOption
)

type sessionOption struct {
	kind  sessionOptionKind
	names []string
}

var sessionOptions = []sessionOption{
	{kind: modelOption, names: []string{"-m", "--model"}},
	{kind: capsOption, names: []string{"-c", "--caps"}},
	{kind: toolOption, names: []string{"-t", "--tool"}},
	{kind: yoloOption, names: []string{"--yolo"}},
	{kind: sourceOption, names: []string{"-f", "--from"}},
}

func (self sessionOption) takesValue() bool {
	return self.kind != yoloOption
}

func (self sessionOption) isRepeatable() bool {
	return self.kind == toolOption
}

func (self sessionOption) isUsedBy(start SessionStart) bool {
	switch self.kind {
	case modelOption:
		return start.ModelGlob != ""
	case capsOption:
		return start.CapFlags != ""
	case yoloOption:
		return start.IsYolo
	case sourceOption:
		return start.SourceSessionName != ""
	case toolOption:
		return false
	}

	return false
}

func (self sessionOption) nameMatching(partial string) (string, bool) {
	for _, name := range self.names {
		if strings.HasPrefix(name, partial) {
			return name, true
		}
	}

	return "", false
}

func (self SessionStart) withValue(option sessionOption, value string) SessionStart {
	switch option.kind {
	case modelOption:
		self.ModelGlob = value
	case capsOption:
		self.CapFlags = value
	case toolOption:
		self.Tools = append(slices.Clone(self.Tools), value)
	case yoloOption:
		self.IsYolo = true
	case sourceOption:
		self.SourceSessionName = value
	}

	return self
}

func findSessionOption(word string) (sessionOption, bool) {
	for _, option := range sessionOptions {
		if slices.Contains(option.names, word) {
			return option, true
		}
	}

	return sessionOption{}, false
}

func sessionCommand(
	name string,
	description string,
	environment commandEnvironment,
	startSession func(SessionStart) error,
) slash.Command {
	return slash.Command{
		Name:        name,
		Description: description,
		Run: func(_ slash.Context, arguments slash.Arguments) error {
			start, err := parseSessionStart(arguments.Fields)
			if err != nil {
				return err
			}
			return startSession(start)
		},
	}.
		WithArgumentUsage(sessionArgumentUsage).
		WithCompletionUsage(sessionCompletionUsage).
		WithArgumentChoices(func(writtenArguments []string, partial string) []slash.ArgumentChoice {
			return completeSessionArguments(environment, writtenArguments, partial)
		})
}

func parseSessionStart(fields []string) (SessionStart, error) {
	var start SessionStart
	for i := 0; i < len(fields); i++ {
		option, isOption := findSessionOption(fields[i])
		if !isOption {
			if strings.HasPrefix(fields[i], optionMarker) || start.ModelGlob != "" {
				return SessionStart{}, slash.Usage()
			}
			start.ModelGlob = fields[i]
			continue
		}
		if option.isUsedBy(start) && !option.isRepeatable() {
			return SessionStart{}, slash.Usage()
		}
		value := ""
		if option.takesValue() {
			i++
			if i == len(fields) || strings.HasPrefix(fields[i], optionMarker) {
				return SessionStart{}, slash.Usage()
			}
			value = fields[i]
		}
		start = start.withValue(option, value)
	}

	return start, nil
}

func completeSessionArguments(
	environment commandEnvironment,
	writtenArguments []string,
	partial string,
) []slash.ArgumentChoice {
	if count := len(writtenArguments); count > 0 {
		if option, isOption := findSessionOption(writtenArguments[count-1]); isOption && option.takesValue() {
			start, err := parseSessionStart(writtenArguments[:count-1])
			if err != nil || option.isUsedBy(start) {
				return nil
			}
			return sessionValueChoices(environment, option, start, partial)
		}
	}

	start, err := parseSessionStart(writtenArguments)
	if err != nil {
		return nil
	}

	var choices []slash.ArgumentChoice
	for _, option := range sessionOptions {
		if option.isUsedBy(start) && !option.isRepeatable() || option.kind == yoloOption && environment.isYoloInherited {
			continue
		}
		if option.kind == toolOption && len(unlistedTools(environment, start)) == 0 {
			continue
		}
		if name, isMatching := option.nameMatching(partial); isMatching {
			choices = append(choices, slash.ArgumentChoice{
				Text:       name,
				Detail:     strutil.Uncapitalise(cli.OptionDescription(name)),
				TakesValue: option.takesValue(),
			})
		}
	}
	return choices
}

func sessionValueChoices(
	environment commandEnvironment,
	option sessionOption,
	start SessionStart,
	partial string,
) []slash.ArgumentChoice {
	var values []string
	switch option.kind {
	case modelOption:
		values = model.SelectionsMatching(partial, environment.getModelChoices())
	case capsOption:
		values = cli.CapsCompletions(partial, environment.getCustomCapFlags(), start.IsYolo || environment.isYoloInherited)
	case yoloOption:
		return nil
	case sourceOption:
		if environment.getSessionNames == nil {
			return nil
		}
		values = slash.MatchingPrefixes(partial, environment.getSessionNames())
	case toolOption:
		values = slash.MatchingPrefixes(partial, unlistedTools(environment, start))
	}

	choices := make([]slash.ArgumentChoice, len(values))
	for i, value := range values {
		choices[i] = slash.ArgumentChoice{Text: value}
	}
	return choices
}

func unlistedTools(environment commandEnvironment, start SessionStart) []string {
	return slices.DeleteFunc(slices.Clone(environment.getToolNames()), func(name string) bool {
		return slices.Contains(start.Tools, name)
	})
}
