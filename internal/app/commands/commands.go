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
	"crdx.org/oh/internal/app/editor"
	"crdx.org/oh/internal/app/hostcommand"
	"crdx.org/oh/internal/app/prompt"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/terminal"
	"crdx.org/oh/internal/app/work"
)

const (
	systemCommandPrefix   = "/"
	sessionJournalName    = "session.jsonl"
	sessionTranscriptName = "chat.md"

	targetPlaceholder = "<target>"
)

type Options struct {
	ConfigDir        string
	ConfigFile       string
	SystemPromptFile string
	SkillDirs        []string
	Workspace        *work.Space
	ScratchDir       string
	HomeDir          string
	Session          Session

	Editor        *editor.Config
	Output        io.Writer
	PathGrants    PathGrants
	HostToSandbox HostToSandbox
	SandboxToHost SandboxToHost
	Jobs          Jobs
	GetInfo       func() (string, error)
	StartSession  func(SessionStart) error
	LimitOutput   func(string) string
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

type commandEnvironment struct {
	configDir        string
	configPath       string
	systemPromptPath string
	skillDirs        []string
	workspace        *work.Space
	scratchDir       string
	homeDir          string
	session          commandSession

	openEditor     func([]string) error
	openTarget     func([]string) error
	copyText       func([]string) error
	runHostCommand func(string, string) (hostcommand.Result, error)
	limitOutput    func(string) string
	pathGrants     PathGrants
	hostToSandbox  HostToSandbox
	sandboxToHost  SandboxToHost
	jobs           Jobs
	getInfo        func() (string, error)
	startSession   func(SessionStart) error
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
		runHostCommand: hostcommand.Run,
		limitOutput:    options.LimitOutput,
		pathGrants:     options.PathGrants,
		hostToSandbox:  options.HostToSandbox,
		sandboxToHost:  options.SandboxToHost,
		jobs:           options.Jobs,
		getInfo:        options.GetInfo,
		startSession:   options.StartSession,
	})
}

func buildCommands(environment commandEnvironment) (slash.CommandSet, error) {
	if environment.runHostCommand == nil {
		environment.runHostCommand = hostcommand.Run
	}
	if environment.limitOutput == nil {
		environment.limitOutput = func(output string) string { return output }
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
		editorCommand("conf", configTarget(environment), environment.openEditor),
		targetCommand("copy", copyTargets(environment, targets), targetNames, environment.copyText, copyConfirmation),
		targetCommand("edit", targets, targetNames, environment.openEditor, nil),
		infoCommand(environment.getInfo),
		targetCommand("open", targets, targetNames, environment.openTarget, nil),
		sessionCommand("new", func(modelGlob string) error {
			return environment.startSession(SessionStart{ModelGlob: modelGlob})
		}),
	}
	if environment.pathGrants.isConfigured() {
		commands = append(commands, pathGrantCommands(
			environment.pathGrants,
			environment.hostToSandbox,
			environment.sandboxToHost,
		)...)
	}
	if environment.jobs.isConfigured() {
		commands = append(commands, jobCommands(environment.jobs)...)
	}
	commands = append(commands, help)
	commands = append(commands, commandsRequiringPersistedSession(
		environment.session.isPersisted,
		sessionCommand("fork", func(modelGlob string) error {
			return environment.startSession(SessionStart{
				ModelGlob:         modelGlob,
				SourceSessionName: environment.session.name,
			})
		}),
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
	return preparedTarget(
		prepareConfigDir(environment),
		environment.configDir,
		environment.systemPromptPath,
		environment.configPath,
	)
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
		Name: "help",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			if len(arguments.Fields) != 0 {
				return slash.Usage()
			}

			context.PlainNotice(getHelp())
			return nil
		},
	}
}

func infoCommand(getInfo func() (string, error)) slash.Command {
	return slash.Command{
		Name: "info",
		Run: func(context slash.Context, arguments slash.Arguments) error {
			if len(arguments.Fields) != 0 {
				return slash.Usage()
			}

			info, err := getInfo()
			if err != nil {
				return err
			}
			context.PlainNotice(info)
			return nil
		},
	}
}

func editorCommand(name string, target commandTarget, openEditor func([]string) error) slash.Command {
	return slash.Command{
		Name: name,
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

func sessionCommand(name string, startSession func(string) error) slash.Command {
	return slash.Command{
		Name: name,
		Run: func(_ slash.Context, arguments slash.Arguments) error {
			if len(arguments.Fields) > 1 {
				return slash.Usage()
			}
			if len(arguments.Fields) == 0 {
				return startSession("")
			}
			return startSession(arguments.Fields[0])
		},
	}
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
	usesTargets := false
	for i, usage := range visibleCommandUsages {
		if usage == "/new" || usage == "/fork" {
			visibleCommandUsages[i] += " [model]"
		}
		usesTargets = usesTargets || strings.Contains(usage, targetPlaceholder)
	}
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
	targets map[string]commandTarget,
	targetNames []string,
	action func([]string) error,
	confirm func(targetName string, values []string) string,
) slash.Command {
	command := slash.Command{
		Name: name,
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
		WithArgumentUsage(summariseTargets(argumentNames, targetNames))
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
