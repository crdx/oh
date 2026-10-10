package wait

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"crdx.org/oh/internal/stop"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/bash"
)

const Name = "wait"

const (
	Any = "any"
	All = "all"
)

const Limit = bash.CommandTimeout

type Source interface {
	Kind() string
	Mention(name string) string
	Knows(name string) bool
	Hold(name string) (<-chan struct{}, func(), error)
	Ended(name string) string
	Running(name string) string
}

type Args struct {
	Names   []string `json:"names"`
	Until   string   `json:"until,omitempty"`
	Seconds int      `json:"seconds,omitempty"`
}

type target struct {
	source Source
	name   string
}

func New(sources []Source, arrival func(context.Context) <-chan struct{}) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        Name,
			Description: describeTool(sources),
			Schema: tool.Schema{
				tool.StringArray("names", "what to wait for"),
				tool.Enum("until", "whether to return once any or all of them have ended (default any)", Any, All).Optional(),
				tool.Integer("seconds", fmt.Sprintf("the longest to wait — max %s (default)", util.CompactDuration(Limit))).Optional(),
			},
		},
		func(args Args) tool.CallRendering { return Describe(sources, args) },
	).
		Validate(validate).
		TakesAtMost(getLimit).
		Plain(func(ctx context.Context, args Args) (string, error) {
			targets, err := resolve(sources, args.Names)
			if err != nil {
				return "", err
			}
			return waitFor(ctx, targets, getUntil(args), getLimit(args), arrival(ctx))
		})
}

func describeTool(sources []Source) string {
	kinds := make([]string, 0, len(sources))
	for _, source := range sources {
		kinds = append(kinds, source.Kind()+"s")
	}
	description := "wait for any or all of the named " + strings.Join(kinds, " and ") +
		" to end, and return their reports; a message from the user ends the wait early"
	if len(sources) > 1 {
		description += "; a name that more than one kind knows takes its kind as a prefix, such as " +
			sources[0].Kind() + ":<name>"
	}
	return description
}

func Describe(sources []Source, args Args) tool.CallRendering {
	separator := " || "
	if getUntil(args) == All {
		separator = " && "
	}
	rendering := tool.CallRendering{
		Kind:     Name,
		Subject:  strings.Join(args.Names, separator),
		Mentions: mentions(sources, args.Names),
	}
	if args.Seconds > 0 {
		rendering.Qualifier = "for up to " + util.CompactDuration(getLimit(args))
	}
	return rendering
}

func mentions(sources []Source, names []string) []string {
	var mentionNames []string
	for _, name := range names {
		if source, bare, isPrefixed := cutKind(sources, name); isPrefixed {
			mentionNames = append(mentionNames, source.Mention(bare))
			continue
		}
		for _, source := range sources {
			mentionNames = append(mentionNames, source.Mention(name))
		}
	}
	return mentionNames
}

func cutKind(sources []Source, name string) (Source, string, bool) {
	for _, source := range sources {
		if bare, isPrefixed := strings.CutPrefix(name, source.Kind()+":"); isPrefixed {
			return source, bare, true
		}
	}
	return nil, "", false
}

func validate(args Args) error {
	if len(args.Names) == 0 {
		return errors.New("names must name at least one thing to wait for")
	}
	for index, name := range args.Names {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("names[%d] is empty", index)
		}
		if slices.Contains(args.Names[:index], name) {
			return fmt.Errorf("%s is named twice", name)
		}
	}
	if !slices.Contains([]string{Any, All}, getUntil(args)) {
		return errors.New("until must be any or all")
	}
	if args.Seconds < 0 {
		return errors.New("seconds must be positive")
	}
	return nil
}

func getUntil(args Args) string {
	if args.Until == "" {
		return Any
	}
	return args.Until
}

func getLimit(args Args) time.Duration {
	if args.Seconds <= 0 {
		return Limit
	}
	return min(time.Duration(args.Seconds)*time.Second, Limit)
}

func resolve(sources []Source, names []string) ([]target, error) {
	targets := make([]target, 0, len(names))
	for _, name := range names {
		found, err := resolveOne(sources, name)
		if err != nil {
			return nil, err
		}
		targets = append(targets, found)
	}
	return targets, nil
}

func resolveOne(sources []Source, name string) (target, error) {
	if source, bare, isPrefixed := cutKind(sources, name); isPrefixed {
		if !source.Knows(bare) {
			return target{}, fmt.Errorf("there is no %s named %s", source.Kind(), bare)
		}
		return target{source: source, name: bare}, nil
	}

	var found []target
	for _, source := range sources {
		if source.Knows(name) {
			found = append(found, target{source: source, name: name})
		}
	}
	switch len(found) {
	case 0:
		return target{}, fmt.Errorf("nothing is named %s", name)
	case 1:
		return found[0], nil
	}
	var prefixedNames []string
	for _, candidate := range found {
		prefixedNames = append(prefixedNames, candidate.source.Kind()+":"+name)
	}
	return target{}, fmt.Errorf("%s names more than one thing; name it as %s", name, strings.Join(prefixedNames, " or "))
}

func waitFor(
	ctx context.Context,
	targets []target,
	until string,
	limit time.Duration,
	arrival <-chan struct{},
) (string, error) {
	overs := make([]<-chan struct{}, len(targets))
	for index, current := range targets {
		over, release, err := current.source.Hold(current.name)
		if err != nil {
			return "", err
		}
		defer release()
		overs[index] = over
	}

	deadline := time.NewTimer(limit)
	defer deadline.Stop()

	cases := make([]reflect.SelectCase, 0, len(overs)+3)
	for _, over := range overs {
		cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(over)})
	}
	cases = append(
		cases,
		reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ctx.Done())},
		reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(deadline.C)},
		reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(arrival)},
	)
	stopIndex, expiryIndex, arrivalIndex := len(overs), len(overs)+1, len(overs)+2

	for !isSatisfied(overs, until) {
		chosenIndex, _, _ := reflect.Select(cases)
		switch chosenIndex {
		case stopIndex:
			return "", stop.Error(ctx, "")
		case expiryIndex:
			return report(targets, overs, "the wait gave up after "+util.CompactDuration(limit)), nil
		case arrivalIndex:
			return report(targets, overs, "the user sent a message, so the wait ended early"), nil
		}
		cases[chosenIndex].Chan = reflect.ValueOf((<-chan struct{})(nil))
	}

	return report(targets, overs, ""), nil
}

func isSatisfied(overs []<-chan struct{}, until string) bool {
	endedCount := 0
	for _, over := range overs {
		if hasEnded(over) {
			endedCount++
		}
	}
	if until == All {
		return endedCount == len(overs)
	}
	return endedCount > 0
}

func hasEnded(over <-chan struct{}) bool {
	select {
	case <-over:
		return true
	default:
		return false
	}
}

func report(targets []target, overs []<-chan struct{}, reason string) string {
	reports := make([]string, 0, len(targets))
	var runningNames []string
	for index, current := range targets {
		if hasEnded(overs[index]) {
			reports = append(reports, current.source.Ended(current.name))
			continue
		}
		runningNames = append(runningNames, current.name)
		if reason != "" {
			reports = append(reports, current.source.Running(current.name))
		}
	}
	text := strings.Join(reports, "\n\n")
	if reason == "" || len(runningNames) == 0 {
		return text
	}
	return text + fmt.Sprintf("\n\nnote: %s, and %s %s still running.", reason, strings.Join(runningNames, ", "), pluralIs(len(runningNames)))
}

func pluralIs(count int) string {
	if count == 1 {
		return "is"
	}
	return "are"
}
