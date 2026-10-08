package slash

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"crdx.org/oh/internal/app/width"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/agent"
)

type usageError struct{}

func (usageError) Error() string {
	return "invalid command usage"
}

func Usage() error {
	return usageError{}
}

func IsUsageError(err error) bool {
	var target usageError
	return errors.As(err, &target)
}

func FormatError(invocation Invocation, err error) string {
	if IsUsageError(err) {
		return "Usage: " + invocation.Usage
	}

	return invocation.Name + ": " + strutil.Capitalise(err.Error())
}

type Context interface {
	Emit(event agent.Event)
	Send(message string)
	Notice(message string)
	NoticeIndented(message string, continuationIndent int)
	NoticeListing(message string)
	PlainNotice(message string)
	PlainNoticeIndented(message string, continuationIndent int)
	PlainNoticeListing(message string)
	Success(message string)
}

type Arguments struct {
	Fields []string
	Text   string
}

type Command struct {
	Name                  string
	Description           string
	Run                   func(Context, Arguments) error
	listArguments         func() []string
	completeArgument      func(writtenArguments []string, partial string) []ArgumentChoice
	isPathArgumentAfter   func(string) bool
	argumentUsage         string
	completionUsage       string
	takesAttachedArgument bool
	allowsManyArguments   bool
}

func (self Command) WithAttachedArgument(usage string) Command {
	self.takesAttachedArgument = true
	self.argumentUsage = usage
	return self
}

func (self Command) WithArguments(arguments ...string) Command {
	fixedArguments := append([]string(nil), arguments...)
	return self.WithListedArguments(func() []string { return slices.Clone(fixedArguments) })
}

func (self Command) WithListedArguments(list func() []string) Command {
	self.listArguments = list
	return self
}

func (self Command) WithArgumentCompletion(complete func(writtenArguments []string, partial string) []string) Command {
	return self.WithArgumentChoices(func(writtenArguments []string, partial string) []ArgumentChoice {
		return plainChoices(complete(writtenArguments, partial))
	})
}

func (self Command) WithArgumentChoices(complete func(writtenArguments []string, partial string) []ArgumentChoice) Command {
	self.completeArgument = complete
	return self
}

type ArgumentChoice struct {
	Text       string
	Detail     string
	TakesValue bool
}

func plainChoices(arguments []string) []ArgumentChoice {
	choices := make([]ArgumentChoice, len(arguments))
	for i, argument := range arguments {
		choices[i] = ArgumentChoice{Text: argument}
	}
	return choices
}

func (self Command) WithManyArguments() Command {
	self.allowsManyArguments = true
	return self
}

func (self Command) WithPathArgumentAfter(arguments ...string) Command {
	pathArguments := slices.Clone(arguments)
	return self.WithPathArgumentAfterMatching(func(argument string) bool {
		return slices.Contains(pathArguments, argument)
	})
}

func (self Command) WithPathArgumentAfterMatching(match func(string) bool) Command {
	self.isPathArgumentAfter = match
	return self
}

func (self Command) WithArgumentUsage(usage string) Command {
	self.argumentUsage = usage
	return self
}

func (self Command) WithCompletionUsage(usage string) Command {
	self.completionUsage = usage
	return self
}

func (self Command) takesArguments() bool {
	return self.argumentUsage != "" || self.completeArgument != nil || len(self.getArguments()) > 0
}

func (self Command) getArguments() []string {
	if self.listArguments == nil {
		return nil
	}
	return self.listArguments()
}

func (self Command) usage(prefix string) string {
	name := prefix + self.Name
	if self.argumentUsage != "" {
		return name + " " + self.argumentUsage
	}
	arguments := self.getArguments()
	if len(arguments) == 0 {
		return name
	}

	arguments = append([]string(nil), arguments...)
	slices.Sort(arguments)
	return name + " {" + strings.Join(arguments, "|") + "}"
}

type Invocation struct {
	Name      string
	Usage     string
	Command   *Command
	Arguments Arguments
}

type CommandSet struct {
	prefix   string
	commands map[string]*Command
	order    []string
}

func NewCommandSet(prefix string, commands ...Command) (CommandSet, error) {
	if err := validatePrefix(prefix); err != nil {
		return CommandSet{}, err
	}

	set := CommandSet{
		prefix:   prefix,
		commands: make(map[string]*Command, len(commands)),
		order:    make([]string, 0, len(commands)),
	}
	for i := range commands {
		command := &commands[i]
		if command.Name == "" || strings.ContainsRune(command.Name, '/') || strings.ContainsFunc(command.Name, unicode.IsSpace) {
			return CommandSet{}, fmt.Errorf("invalid command name %q", command.Name)
		}
		if command.Run == nil {
			return CommandSet{}, fmt.Errorf("command %q has no handler", prefix+command.Name)
		}
		if _, exists := set.commands[command.Name]; exists {
			return CommandSet{}, fmt.Errorf("command %q is already registered", prefix+command.Name)
		}
		set.commands[command.Name] = command
		set.order = append(set.order, command.Name)
	}

	return set, nil
}

func (self CommandSet) Usages() []string {
	usages := make([]string, 0, len(self.order))
	for _, name := range self.order {
		usages = append(usages, self.commands[name].usage(self.prefix))
	}
	return usages
}

const (
	HelpIndent = "  "
	HelpWidth  = 78
)

type HelpEntry struct {
	Usage       string
	Description string
}

func FormatHelp(entries []HelpEntry) []string {
	usageWidth := 0
	for _, entry := range entries {
		usageWidth = max(usageWidth, width.Of(entry.Usage))
	}

	var lines []string
	for _, entry := range entries {
		if entry.Description == "" {
			lines = append(lines, HelpIndent+entry.Usage)
			continue
		}

		padding := strings.Repeat(" ", usageWidth-width.Of(entry.Usage))
		prefix := HelpIndent + entry.Usage + padding + HelpIndent
		descriptionLines := width.Wrap(entry.Description, HelpWidth-width.Of(prefix))
		lines = append(lines, prefix+descriptionLines[0])
		continuation := strings.Repeat(" ", width.Of(prefix))
		for _, line := range descriptionLines[1:] {
			lines = append(lines, continuation+line)
		}
	}
	return lines
}

func (self CommandSet) GetHelpEntries() []HelpEntry {
	entries := make([]HelpEntry, 0, len(self.order))
	for _, name := range self.order {
		command := self.commands[name]
		entries = append(entries, HelpEntry{
			Usage:       command.usage(self.prefix),
			Description: command.Description,
		})
	}
	return entries
}

type Registry struct {
	sets []CommandSet
}

func NewRegistry(sets ...CommandSet) (Registry, error) {
	prefixes := make(map[string]struct{}, len(sets))
	for _, set := range sets {
		if err := validatePrefix(set.prefix); err != nil {
			return Registry{}, err
		}
		if _, exists := prefixes[set.prefix]; exists {
			return Registry{}, fmt.Errorf("command prefix %q is already registered", set.prefix)
		}
		prefixes[set.prefix] = struct{}{}
	}

	registeredSets := append([]CommandSet(nil), sets...)
	slices.SortStableFunc(registeredSets, func(left, right CommandSet) int {
		return len(right.prefix) - len(left.prefix)
	})
	return Registry{sets: registeredSets}, nil
}

func (self Registry) ReplaceCommandSet(replacement CommandSet) error {
	if err := validatePrefix(replacement.prefix); err != nil {
		return err
	}
	for i, set := range self.sets {
		if set.prefix == replacement.prefix {
			self.sets[i] = replacement
			return nil
		}
	}
	return fmt.Errorf("command prefix %q is not registered", replacement.prefix)
}

func validatePrefix(prefix string) error {
	if prefix == "" || strings.ContainsFunc(prefix, unicode.IsSpace) {
		return fmt.Errorf("invalid command prefix %q", prefix)
	}
	return nil
}

func (self Registry) Find(message string) (Invocation, bool) {
	text := strings.TrimLeftFunc(message, unicode.IsSpace)
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return Invocation{}, false
	}

	set, isFound := self.getSet(fields[0])
	if !isFound {
		return Invocation{}, false
	}

	bareName := strings.TrimPrefix(fields[0], set.prefix)
	command, isFound := set.commands[bareName]
	if !isFound {
		if command, isFound = set.attachedCommand(bareName); !isFound {
			return Invocation{}, false
		}
	}

	name := set.prefix + command.Name

	return Invocation{
		Name:      name,
		Usage:     command.usage(set.prefix),
		Command:   command,
		Arguments: argumentsAfter(text, name),
	}, true
}

func (self CommandSet) attachedCommand(bareName string) (*Command, bool) {
	var longest *Command
	for _, name := range self.order {
		command := self.commands[name]
		if !command.takesAttachedArgument || !strings.HasPrefix(bareName, name) {
			continue
		}
		if longest == nil || len(name) > len(longest.Name) {
			longest = command
		}
	}

	return longest, longest != nil
}

func argumentsAfter(text string, name string) Arguments {
	argument := strings.TrimSpace(strings.TrimPrefix(text, name))

	return Arguments{Fields: strings.Fields(argument), Text: argument}
}

func (self Registry) CommandName(message string) (string, bool) {
	fields := strings.Fields(message)
	if len(fields) == 0 {
		return "", false
	}
	if _, found := self.getSet(fields[0]); !found {
		return "", false
	}
	if strings.Contains(strings.TrimLeft(fields[0], "/"), "/") {
		return "", false
	}
	return fields[0], true
}

type Completion struct {
	Text           string
	Label          string
	Description    string
	TakesArguments bool
	IsFinal        bool
}

type completionTarget struct {
	set              CommandSet
	command          *Command
	precedingText    string
	partial          string
	writtenArguments []string
}

func (self completionTarget) arguments() []ArgumentChoice {
	if self.command.completeArgument != nil {
		return self.command.completeArgument(self.writtenArguments, self.partial)
	}

	return plainChoices(MatchingPrefixes(self.partial, slices.DeleteFunc(slices.Clone(self.command.getArguments()), func(argument string) bool {
		return slices.Contains(self.writtenArguments, argument)
	})))
}

func (self Registry) Completes(prefix string) bool {
	target, isCompletable := self.completionTarget(prefix)
	if !isCompletable {
		return false
	}

	return target.command == nil || len(target.arguments()) > 0
}

func (self Registry) Completions(prefix string) []Completion {
	target, isCompletable := self.completionTarget(prefix)
	if !isCompletable {
		return nil
	}

	if target.command == nil {
		names := MatchingPrefixes(target.partial, target.set.commandNames())
		completions := make([]Completion, len(names))
		for i, name := range names {
			command := target.set.commands[name]
			text := target.set.prefix + name
			label := text
			if command.completionUsage != "" {
				label += " " + command.completionUsage
			} else if command.argumentUsage != "" {
				label = command.usage(target.set.prefix)
			}
			completions[i] = Completion{
				Text:           text,
				Label:          label,
				Description:    command.Description,
				TakesArguments: command.takesArguments(),
				IsFinal:        command.argumentUsage == "" && command.listArguments == nil && command.completeArgument == nil,
			}
		}
		return completions
	}

	arguments := target.arguments()
	completions := make([]Completion, len(arguments))
	for i, argument := range arguments {
		completions[i] = Completion{
			Text:           target.precedingText + argument.Text,
			Label:          argument.Text,
			Description:    argument.Detail,
			TakesArguments: argument.TakesValue,
		}
	}
	return completions
}

func (self Registry) completionTarget(prefix string) (completionTarget, bool) {
	if strings.ContainsAny(prefix, "\t\r\n") {
		return completionTarget{}, false
	}

	name, argumentPrefix, hasArgument := strings.Cut(prefix, " ")
	set, isFound := self.getSet(name)
	if !isFound {
		return completionTarget{}, false
	}

	bareName := strings.TrimPrefix(name, set.prefix)
	if !hasArgument {
		if strings.Contains(bareName, "/") || set.isOnlyAttached(bareName) {
			return completionTarget{}, false
		}
		return completionTarget{set: set, partial: bareName}, true
	}
	command, isFound := set.commands[bareName]
	if !isFound || command.listArguments == nil && command.completeArgument == nil {
		return completionTarget{}, false
	}

	partialStart := strings.LastIndex(argumentPrefix, " ") + 1
	if partialStart > 0 && !command.allowsManyArguments && command.completeArgument == nil {
		return completionTarget{}, false
	}

	partial := argumentPrefix[partialStart:]
	return completionTarget{
		set:              set,
		command:          command,
		precedingText:    strings.TrimSuffix(prefix, partial),
		partial:          partial,
		writtenArguments: strings.Fields(argumentPrefix[:partialStart]),
	}, true
}

func (self Registry) getSet(name string) (CommandSet, bool) {
	for _, set := range self.sets {
		if strings.HasPrefix(name, set.prefix) {
			return set, true
		}
	}
	return CommandSet{}, false
}

func (self CommandSet) isOnlyAttached(bareName string) bool {
	_, isAttached := self.attachedCommand(bareName)
	return isAttached && len(MatchingPrefixes(bareName, self.commandNames())) == 0
}

func (self CommandSet) commandNames() []string {
	names := make([]string, 0, len(self.commands))
	for name, command := range self.commands {
		if command.takesAttachedArgument {
			continue
		}
		names = append(names, name)
	}
	return names
}

func MatchingPrefixes(prefix string, candidates []string) []string {
	var matches []string
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, prefix) {
			matches = append(matches, candidate)
		}
	}
	slices.Sort(matches)
	return matches
}
