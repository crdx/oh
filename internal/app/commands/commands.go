package commands

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"crdx.org/oh/internal/app/column"
	"crdx.org/oh/internal/app/contextsource"
	"crdx.org/oh/internal/app/editor"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/prompt"
	"crdx.org/oh/internal/app/skill"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/table"
	"crdx.org/oh/internal/app/terminal"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/util"
)

const (
	systemCommandPrefix   = "/"
	sessionJournalName    = "session.jsonl"
	sessionTranscriptName = "chat.md"

	targetPlaceholder = "<target>"
)

var errHostCommandsUnavailable = errors.New("commands cannot run on the host here")

type Options struct {
	ConfigDir        string
	ConfigFile       string
	SystemPromptFile string
	SkillDirs        []string
	Workspace        *work.Space
	ScratchDir       string
	HomeDir          string
	Session          Session

	Editor            *editor.Config
	Output            io.Writer
	PathGrants        PathGrants
	Forwards          Forwards
	Jobs              Jobs
	GetInfo           func() (string, error)
	GetContextSources func() ContextSources
	GetModelChoices   func() []model.Choice
	StartSession      func(SessionStart) error
	StartHostCommand  func(directory string, command string) error
}

type Session struct {
	Name           string
	ID             string
	Directory      string
	IsPersisted    func() bool
	GetLastMessage func() (string, bool)
}

type SessionStart struct {
	ModelGlob         string
	SourceSessionName string
}

type ContextSources struct {
	SystemSources  []contextsource.Source
	ProjectSources []contextsource.Source
	SessionSources []contextsource.Source
}

type commandEnvironment struct {
	configDir        string
	configPath       string
	systemPromptPath string
	skillDirs        []string
	workspace        *work.Space
	scratchDir       string
	homeDir          string
	session          commandSession

	openEditor        func([]string) error
	openTarget        func([]string) error
	copyText          func([]string) error
	startHostCommand  func(directory string, command string) error
	pathGrants        PathGrants
	forwards          Forwards
	jobs              Jobs
	getInfo           func() (string, error)
	getContextSources func() ContextSources
	getModelChoices   func() []model.Choice
	startSession      func(SessionStart) error
}

type commandSession struct {
	name           string
	id             string
	directory      string
	isPersisted    func() bool
	getLastMessage func() (string, bool)
}

type commandTarget struct {
	resolveValues func() ([]string, error)
}

func New(options Options) (slash.CommandSet, error) {
	editorConfiguration := options.Editor
	if editorConfiguration == nil {
		editorConfiguration = editor.NewConfiguration(nil)
	}

	return buildCommands(commandEnvironment{
		configDir:        options.ConfigDir,
		configPath:       options.ConfigFile,
		systemPromptPath: options.SystemPromptFile,
		skillDirs:        options.SkillDirs,
		workspace:        options.Workspace,
		scratchDir:       options.ScratchDir,
		homeDir:          options.HomeDir,
		session: commandSession{
			name:           options.Session.Name,
			id:             options.Session.ID,
			directory:      options.Session.Directory,
			isPersisted:    options.Session.IsPersisted,
			getLastMessage: options.Session.GetLastMessage,
		},
		openEditor: func(paths []string) error {
			return editorConfiguration.Open(paths...)
		},
		openTarget: openDesktopTargets,
		copyText: func(values []string) error {
			return terminal.Copy(options.Output, strings.Join(values, "\n"))
		},
		startHostCommand:  options.StartHostCommand,
		pathGrants:        options.PathGrants,
		forwards:          options.Forwards,
		jobs:              options.Jobs,
		getInfo:           options.GetInfo,
		getContextSources: options.GetContextSources,
		getModelChoices:   options.GetModelChoices,
		startSession:      options.StartSession,
	})
}

func buildCommands(environment commandEnvironment) (slash.CommandSet, error) {
	if environment.startHostCommand == nil {
		environment.startHostCommand = func(string, string) error { return errHostCommandsUnavailable }
	}
	if environment.getContextSources == nil {
		environment.getContextSources = func() ContextSources { return ContextSources{} }
	}
	if environment.getModelChoices == nil {
		environment.getModelChoices = func() []model.Choice { return nil }
	}
	openConfiguration := func(additionalPaths []string) error {
		paths := []string{environment.configDir}
		paths = append(paths, additionalPaths...)
		paths = append(paths, environment.configPath)
		return environment.openEditor(paths)
	}

	targets := locationTargets(environment)
	targetNames := slices.Sorted(maps.Keys(targets))

	var set slash.CommandSet
	var help slash.Command
	help = helpCommand(func() string {
		return helpText(set.Usages(), systemCommandPrefix+help.Name, targetNames)
	})
	commands := []slash.Command{
		shellCommand(environment),
		editorCommand(
			"conf",
			"edit the config and system prompt",
			configTarget(environment),
			openConfiguration,
		),
		targetCommand(
			"copy",
			"copy a target to the clipboard",
			copyTargets(environment, targets),
			targetNames,
			environment.copyText,
			copyConfirmation,
		),
		contextCommand(environment.getContextSources),
		targetCommand("edit", "open a target in your editor", targets, targetNames, environment.openEditor, nil),
		infoCommand(environment.getInfo),
		targetCommand("open", "open a target with its default application", targets, targetNames, environment.openTarget, nil),
		sessionCommand(
			"new",
			"start a new session",
			environment.getModelChoices,
			func(modelGlob string) error {
				return environment.startSession(SessionStart{ModelGlob: modelGlob})
			},
		),
	}
	if environment.pathGrants.isConfigured() {
		commands = append(commands, pathGrantCommands(
			environment.pathGrants,
			environment.forwards,
		)...)
	}
	if environment.jobs.isConfigured() {
		commands = append(commands, jobCommands(environment.jobs)...)
	}
	commands = append(commands, help)
	commands = append(commands, commandsRequiringPersistedSession(
		environment.session.isPersisted,
		sessionCommand(
			"fork",
			"fork this session",
			environment.getModelChoices,
			func(modelGlob string) error {
				return environment.startSession(SessionStart{
					ModelGlob:         modelGlob,
					SourceSessionName: environment.session.name,
				})
			},
		),
	)...)

	var err error
	set, err = slash.NewCommandSet(systemCommandPrefix, commands...)
	return set, err
}

func summariseTargets(argumentNames []string, targetNames []string) string {
	var others []string
	for _, argument := range argumentNames {
		if !slices.Contains(targetNames, argument) {
			others = append(others, argument)
		}
	}

	if len(argumentNames)-len(others) < len(targetNames) {
		return "{" + strings.Join(argumentNames, "|") + "}"
	}
	if len(others) == 0 {
		return targetPlaceholder
	}

	return "{" + strings.Join(append(others, targetPlaceholder), "|") + "}"
}

func copyTargets(environment commandEnvironment, targets map[string]commandTarget) map[string]commandTarget {
	copiedTargets := maps.Clone(targets)
	copiedTargets["last-message"] = lastMessageTarget(environment.session.getLastMessage)
	copiedTargets["session-chat"] = textFileTarget(
		"Session chat",
		filepath.Join(environment.session.directory, sessionTranscriptName),
	)
	copiedTargets["session-name"] = staticTarget(environment.session.name)
	copiedTargets["session-id"] = staticTarget(environment.session.id)
	return copiedTargets
}

func configTarget(environment commandEnvironment) commandTarget {
	prepareDirectory := prepareConfigDir(environment)
	return commandTarget{
		resolveValues: func() ([]string, error) {
			if err := prepareDirectory(); err != nil {
				return nil, err
			}

			var paths []string
			switch _, err := os.Stat(environment.systemPromptPath); {
			case errors.Is(err, fs.ErrNotExist):
			case err != nil:
				return nil, fmt.Errorf("could not inspect System prompt: %w", err)
			default:
				paths = append(paths, environment.systemPromptPath)
			}
			return paths, nil
		},
	}
}

func prepareConfigDir(environment commandEnvironment) func() error {
	return func() error {
		return os.MkdirAll(environment.configDir, 0o700)
	}
}

func locationTargets(environment commandEnvironment) map[string]commandTarget {
	prepareDirectory := prepareConfigDir(environment)

	targets := map[string]commandTarget{
		"agents-file":        existingTarget("Project context", prompt.ProjectContextPaths(environment.workspace)...),
		"config-dir":         preparedTarget(prepareDirectory, environment.configDir),
		"config-file":        preparedTarget(prepareDirectory, environment.configPath),
		"system-prompt-file": preparedTarget(prepareDirectory, environment.systemPromptPath),
		"skills-dir":         existingTarget("Skills directory", environment.skillDirs...),
		"workspace-dir":      staticTarget(environment.workspace.GetDir()),
		"scratch-dir":        staticTarget(environment.scratchDir),
		"home-dir":           staticTarget(environment.homeDir),
		"session-dir":        existingTarget("Session directory", environment.session.directory),
		"session-log-file": existingTarget(
			"Session log",
			filepath.Join(environment.session.directory, sessionJournalName),
		),
		"session-chat-file": existingTarget(
			"Session chat",
			filepath.Join(environment.session.directory, sessionTranscriptName),
		),
	}

	snippetsDirectory := filepath.Join(environment.configDir, "snippets")
	if info, err := os.Stat(snippetsDirectory); err == nil && info.IsDir() {
		targets["snippets-dir"] = staticTarget(snippetsDirectory)
	}
	return targets
}

func helpCommand(getHelp func() string) slash.Command {
	return slash.Command{
		Name:        "help",
		Description: "list the commands and targets",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			if len(arguments.Fields) != 0 {
				return slash.Usage()
			}

			context.PlainNotice(getHelp())
			return nil
		},
	}
}

func contextCommand(getSources func() ContextSources) slash.Command {
	return slash.Command{
		Name:        "ctx",
		Description: "list sources contributing to model context",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			if len(arguments.Fields) != 0 {
				return slash.Usage()
			}

			context.NoticeListing(formatContextSources(getSources()))
			return nil
		},
	}
}

func formatContextSources(sources ContextSources) string {
	sections := []struct {
		label   string
		sources []contextsource.Source
	}{
		{label: "System", sources: sources.SystemSources},
		{label: "Project", sources: sources.ProjectSources},
		{label: "Session", sources: sources.SessionSources},
	}

	var rows [][]string
	for _, section := range sections {
		for _, source := range section.sources {
			rows = append(rows, []string{util.FormatEstimatedTokens(source.EstimatedTokens), source.DisplayName()})
		}
	}
	if len(rows) == 0 {
		return "No context sources."
	}

	contextTable := table.New(
		table.Column{Align: table.Right, Style: style.Dim},
		table.Column{},
	).Fit(rows)
	var listings []string
	for _, section := range sections {
		if len(section.sources) == 0 {
			continue
		}

		listing := []string{section.label + ":"}
		for _, source := range section.sources {
			listing = append(listing, "  "+contextTable.Row([]string{
				util.FormatEstimatedTokens(source.EstimatedTokens),
				paintSourceName(source),
			}, 0))
		}
		listings = append(listings, strings.Join(listing, "\n"))
	}

	return strings.Join(listings, "\n")
}

func paintSourceName(source contextsource.Source) string {
	name, isSkill := skill.NameFromPath(source.Path)
	if !isSkill {
		return source.DisplayName()
	}

	directory := filepath.Dir(source.Path)
	return filepath.Dir(directory) + string(filepath.Separator) + style.Skill(name) + string(filepath.Separator) + filepath.Base(source.Path)
}

func infoCommand(getInfo func() (string, error)) slash.Command {
	return slash.Command{
		Name:        "info",
		Description: "show every bar segment with its current value",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			if len(arguments.Fields) != 0 {
				return slash.Usage()
			}

			info, err := getInfo()
			if err != nil {
				return err
			}
			context.NoticeListing(info)
			return nil
		},
	}
}

func editorCommand(
	name string,
	description string,
	target commandTarget,
	openEditor func([]string) error,
) slash.Command {
	return slash.Command{
		Name:        name,
		Description: description,
		Run: func(_ slash.Context, arguments slash.Arguments) error {
			if len(arguments.Fields) != 0 {
				return slash.Usage()
			}

			values, err := target.resolveValues()
			if err != nil {
				return err
			}
			return openEditor(values)
		},
	}
}

func sessionCommand(
	name string,
	description string,
	getModelChoices func() []model.Choice,
	startSession func(string) error,
) slash.Command {
	return slash.Command{
		Name:        name,
		Description: description,
		Run: func(_ slash.Context, arguments slash.Arguments) error {
			if len(arguments.Fields) > 1 {
				return slash.Usage()
			}
			if len(arguments.Fields) == 0 {
				return startSession("")
			}
			return startSession(arguments.Fields[0])
		},
	}.
		WithArgumentUsage("[<model>]").
		WithArgumentCompletion(func(writtenArguments []string, partial string) []string {
			if len(writtenArguments) > 0 {
				return nil
			}
			return model.SelectionsMatching(partial, getModelChoices())
		})
}

func commandsRequiringPersistedSession(isSessionPersisted func() bool, commands ...slash.Command) []slash.Command {
	for i := range commands {
		run := commands[i].Run
		commands[i].Run = func(context slash.Context, arguments slash.Arguments) error {
			if !isSessionPersisted() {
				return errors.New("session does not exist yet")
			}
			return run(context, arguments)
		}
	}
	return commands
}

func helpText(commandUsages []string, hiddenCommandUsage string, targetNames []string) string {
	visibleCommandUsages := slices.DeleteFunc(commandUsages, func(usage string) bool { return usage == hiddenCommandUsage })
	usesTargets := slices.ContainsFunc(visibleCommandUsages, func(usage string) bool {
		return strings.Contains(usage, targetPlaceholder)
	})
	sections := []string{style.Info("Commands:") + "\n" + slash.HelpIndent + strings.Join(visibleCommandUsages, "\n"+slash.HelpIndent)}
	if usesTargets {
		targetRows := column.Rows(targetNames, slash.HelpWidth-len(slash.HelpIndent))
		sections = append(sections, style.Info("Targets:")+"\n"+slash.HelpIndent+strings.Join(targetRows, "\n"+slash.HelpIndent))
	}
	return strings.Join(sections, "\n\n")
}

func staticTarget(values ...string) commandTarget {
	return commandTarget{
		resolveValues: func() ([]string, error) { return values, nil },
	}
}

func preparedTarget(prepare func() error, values ...string) commandTarget {
	return commandTarget{
		resolveValues: func() ([]string, error) {
			if err := prepare(); err != nil {
				return nil, err
			}
			return values, nil
		},
	}
}

func existingTarget(description string, paths ...string) commandTarget {
	return commandTarget{
		resolveValues: func() ([]string, error) {
			var present []string
			for _, path := range paths {
				switch _, err := os.Stat(path); {
				case errors.Is(err, fs.ErrNotExist):
					continue
				case err != nil:
					return nil, fmt.Errorf("could not inspect %s: %w", description, err)
				default:
					present = append(present, path)
				}
			}
			if len(present) == 0 {
				return nil, fmt.Errorf("%s does not exist yet", description)
			}
			return present, nil
		},
	}
}

func textFileTarget(description string, path string) commandTarget {
	return commandTarget{
		resolveValues: func() ([]string, error) {
			contents, err := os.ReadFile(path) //nolint:gosec // the path is selected by the command
			if errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("%s does not exist yet", description)
			}
			if err != nil {
				return nil, fmt.Errorf("could not read %s: %w", description, err)
			}
			return []string{string(contents)}, nil
		},
	}
}

func lastMessageTarget(getLastMessage func() (string, bool)) commandTarget {
	return commandTarget{
		resolveValues: func() ([]string, error) {
			if getLastMessage != nil {
				if message, found := getLastMessage(); found {
					return []string{message}, nil
				}
			}
			return nil, errors.New("no model message has been received yet")
		},
	}
}

func copyConfirmation(targetName string, values []string) string {
	description := strings.ReplaceAll(targetName, "-", " ")
	if targetName == "last-message" || targetName == "session-chat" {
		return fmt.Sprintf("Copied %s to clipboard", description)
	}
	return fmt.Sprintf("Copied %s to clipboard: %s", description, strings.Join(values, ", "))
}

func targetCommand(
	name string,
	description string,
	targets map[string]commandTarget,
	targetNames []string,
	action func([]string) error,
	confirm func(targetName string, values []string) string,
) slash.Command {
	command := slash.Command{
		Name:        name,
		Description: description,
		Run: func(context slash.Context, arguments slash.Arguments) error {
			if len(arguments.Fields) != 1 {
				return slash.Usage()
			}

			target, isFound := targets[arguments.Fields[0]]
			if !isFound {
				return slash.Usage()
			}

			values, err := target.resolveValues()
			if err != nil {
				return err
			}

			if err := action(values); err != nil {
				return err
			}
			if confirm != nil {
				context.Success(confirm(arguments.Fields[0], values))
			}
			return nil
		},
	}

	argumentNames := slices.Sorted(maps.Keys(targets))
	return command.
		WithArguments(argumentNames...).
		WithArgumentUsage(summariseTargets(argumentNames, targetNames)).
		WithCompletionUsage(targetPlaceholder)
}

func openDesktopTargets(paths []string) error {
	for _, path := range paths {
		//nolint:gosec,noctx // the opener is fixed, takes a path the command chose, and outlives this call
		command := exec.Command("xdg-open", path)
		if err := command.Start(); err != nil {
			return fmt.Errorf("could not open %s: %w", path, err)
		}

		go func() { _ = command.Wait() }()
	}

	return nil
}
