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
	PlainNotice(message string)
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
	argumentUsage         string
	takesAttachedArgument bool
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

func (self Command) WithArgumentUsage(usage string) Command {
	self.argumentUsage = usage
	return self
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

func (self Registry) getSet(name string) (CommandSet, bool) {
	for _, set := range self.sets {
		if strings.HasPrefix(name, set.prefix) {
			return set, true
		}
	}
	return CommandSet{}, false
}

type Completion struct {
	matches []string
	current string
	index   int
}

func (self *Completion) Next(registry Registry, prefix string) (string, bool) {
	if prefix == self.current && len(self.matches) > 0 {
		self.index = (self.index + 1) % len(self.matches)
		self.current = self.matches[self.index]
		return self.current, true
	}

	self.matches = registry.completions(prefix)
	self.index = 0
	if len(self.matches) == 0 {
		self.current = ""
		return "", false
	}

	self.current = self.matches[0]
	return self.current, true
}

func (self *Completion) Reset() {
	self.matches = nil
	self.current = ""
	self.index = 0
}

func (self Registry) completions(prefix string) []string {
	if strings.ContainsAny(prefix, "\t\r\n") {
		return nil
	}

	name, argumentPrefix, hasArgument := strings.Cut(prefix, " ")
	set, isFound := self.getSet(name)
	if !isFound {
		return nil
	}

	bareName := strings.TrimPrefix(name, set.prefix)
	if !hasArgument {
		matches := matchingPrefixes(bareName, set.commandNames())
		for i := range matches {
			matches[i] = set.prefix + matches[i]
		}
		return matches
	}
	if strings.Contains(argumentPrefix, " ") {
		return nil
	}

	command, isFound := set.commands[bareName]
	if !isFound {
		return nil
	}

	arguments := matchingPrefixes(argumentPrefix, command.getArguments())
	for i := range arguments {
		arguments[i] = name + " " + arguments[i]
	}
	return arguments
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

func matchingPrefixes(prefix string, candidates []string) []string {
	var matches []string
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, prefix) {
			matches = append(matches, candidate)
		}
	}
	slices.Sort(matches)
	return matches
}
