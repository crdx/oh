package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"crdx.org/oh/internal/app/backend"
	"crdx.org/oh/internal/app/bar"
	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/conditions"
	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/cycle"
	"crdx.org/oh/internal/app/editor"
	"crdx.org/oh/internal/app/experimental"
	"crdx.org/oh/internal/app/harness"
	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/metrics"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/record"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/sessions"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/terminal"
	"crdx.org/oh/internal/app/textsizing"
	"crdx.org/oh/internal/app/toolset"
	"crdx.org/oh/internal/app/tty"
	"crdx.org/oh/internal/app/usage"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/app/world"
	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/money"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/ask"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/tool/command"
	"crdx.org/oh/pkg/tool/middleware/truncate"
	"crdx.org/oh/pkg/toolbox"
	"crdx.org/oh/pkg/toolbox/notify"
	"crdx.org/oh/pkg/toolbox/title"
)

const (
	scratchPrefix   = "ohw-"
	toolOutputBytes = 100_000
	approvalLimit   = time.Minute

	usageLine = "usage: ohw [-m <model>] [--state <dir>] <world.toml> [<prompt>...]"
)

func main() {
	style.Init(os.Stdout)

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, style.Error(err))
		os.Exit(1)
	}
}

type arguments struct {
	world   string
	state   string
	model   string
	message string
}

var valueFlags = map[string]func(*arguments) *string{
	"--state": func(input *arguments) *string { return &input.state },
	"--model": func(input *arguments) *string { return &input.model },
	"-m":      func(input *arguments) *string { return &input.model },
}

func parse(words []string) (arguments, error) {
	var input arguments

	for at := 0; at < len(words); at++ {
		word := words[at]
		name, value, isJoined := strings.Cut(word, "=")

		field, isValueFlag := valueFlags[name]
		switch {
		case isValueFlag && isJoined:
			*field(&input) = value
		case isValueFlag:
			at++
			if at == len(words) {
				return arguments{}, fmt.Errorf("%s: requires a value", name)
			}
			*field(&input) = words[at]
		case strings.HasPrefix(word, "-") && input.world == "":
			return arguments{}, fmt.Errorf("unknown option: %s", word)
		case input.world == "":
			input.world = word
		default:
			input.message = strings.Join(words[at:], " ")
			at = len(words)
		}
	}

	if input.world == "" {
		return arguments{}, errors.New(usageLine)
	}

	return input, nil
}

func stateDirectory(writtenState string) (string, func(), error) {
	if writtenState != "" {
		path, err := filepath.Abs(writtenState)
		if err != nil {
			return "", nil, fmt.Errorf("could not resolve %s: %w", writtenState, err)
		}
		if err := os.MkdirAll(path, 0o700); err != nil {
			return "", nil, fmt.Errorf("could not prepare %s: %w", path, err)
		}

		return path, func() {}, nil
	}

	path, err := os.MkdirTemp("", scratchPrefix)
	if err != nil {
		return "", nil, fmt.Errorf("could not prepare a state directory: %w", err)
	}

	return path, func() { _ = os.RemoveAll(path) }, nil
}

func run() error {
	args, err := parse(os.Args[1:])
	if err != nil {
		return err
	}

	lastingWorld, err := world.Load(args.world)
	if err != nil {
		return err
	}

	endpointURL := os.Getenv(backend.EndpointVariable)
	hasOwnCaches := endpointURL != ""
	modelCachePath := location.GetModelCachePath(hasOwnCaches)

	stateDir, releaseState, err := stateDirectory(args.state)
	if err != nil {
		return err
	}
	defer releaseState()

	keyboard, releaseKeyboard := tty.Keyboard(os.Stdin)
	defer releaseKeyboard()

	settings, err := config.Load(location.GetConfigFile())
	if err != nil {
		return err
	}

	selection, err := chooseModel(args.model, settings, modelCachePath, hasOwnCaches)
	if err != nil {
		return err
	}
	choice, err := model.Chosen(modelCachePath, selection.Provider, selection.Model)
	if err != nil {
		return err
	}
	client, err := backend.Connect(choice, selection, backend.EndpointSettings{OverrideURL: endpointURL})
	if err != nil {
		return err
	}

	usageCachePath := location.GetUsageCachePath(selection.Provider, hasOwnCaches)
	providerClient := usage.Guard(context.Background(), client.Client, usage.GuardSettings{
		ProviderName: model.ProviderName(selection.Provider),
		ModelName:    selection.Model,
		CachePath:    usageCachePath,
		Now:          time.Now,
	})
	besideWorld := filepath.Dir(lastingWorld.Path)
	workspace := work.At(besideWorld)

	sessionsDir := filepath.Join(stateDir, "sessions")
	log, err := sessions.OpenWriter(sessionsDir, nil, store.Meta{
		Model:        selection.Model,
		WorkspaceDir: besideWorld,
		Provider:     selection.Provider,
		Effort:       selection.Effort,
	})
	if err != nil {
		return err
	}
	defer func() { _ = log.Close() }()
	client.UseSession(log.ID())

	mode := caps.NewLimitedMode(worldCaps(lastingWorld), caps.Read|caps.Write)
	askBroker := ask.New()

	screen := output.New(os.Stdout).LinkPathsUnder(link.Roots{Workspace: besideWorld})
	screen.SetTextSizingSupported(textsizing.Detect(keyboard, os.Stdout))

	var app *harness.App
	isTerminalFocused := func() bool { return app != nil && app.IsFocused() }

	tools, err := buildTools(lastingWorld, mode, askBroker, screen.WriteEscape, isTerminalFocused)
	if err != nil {
		return err
	}

	currentConditions := conditions.Probe(true, true)
	restoredConditions, err := conditions.Restore(nil, nil, currentConditions)
	if err != nil {
		return err
	}

	outputLimit := truncate.NewLimit(toolOutputBytes)

	app = harness.New(harness.Options{
		Agent:           agent.New(lastingWorld.Prompt.Text, providerClient, truncate.Tools(tools, outputLimit)),
		Screen:          screen,
		Terminal:        terminal.New(os.Stdout, workspace),
		Metrics:         metrics.New(metrics.Settings{ContextWindowTokens: choice.ContextWindowTokens, Prices: choice.Prices}),
		Recorder:        record.New(log),
		Workspace:       workspace,
		Mode:            mode,
		Conditions:      restoredConditions.State,
		EditorConfig:    editor.NewConfiguration(nil),
		ToolOutputLimit: outputLimit,
		Experimental:    experimental.New(nil),
		Questions:       askBroker,
		Keyboard:        keyboard,
	})

	usageReporter, _ := client.Client.(agent.UsageReporter)

	err = app.UseBar(bar.Options{
		Workspace:             workspace,
		Session:               cycle.Session{Model: selection.Model, Effort: selection.Effort},
		ModelEffortLevels:     choice.EffortLevels,
		IsFast:                selection.IsFast,
		UsageReporter:         usageReporter,
		UsageCachePath:        usageCachePath,
		UsageIsSelfRefreshing: usageReporter != nil,
		UsageGauges:           usage.TerminalGauges(keyboard, os.Stdout),
		Currency:              money.Dollar(),
	}, worldLayout)
	if err != nil {
		return err
	}
	app.UseModelName(selection.Model)

	app.Begin(args.message)

	return nil
}

func worldLayout(registry segment.Registry) (segment.Layout, error) {
	defaults, err := config.Load("")
	if err != nil {
		return nil, err
	}

	return defaults.BuildLayout(registry)
}

func chooseModel(
	writtenModel string,
	settings config.Config,
	modelCachePath string,
	hasOwnCaches bool,
) (model.Selection, error) {
	defaults := settings.Model.GetDefaults()

	var requestedSelection model.Selection
	if strings.TrimSpace(writtenModel) != "" {
		var err error
		if requestedSelection, err = model.ParseSelection(modelCachePath, writtenModel, defaults); err != nil {
			return model.Selection{}, err
		}
	}

	configuredSelections, err := model.ParseRoundRobin(modelCachePath, settings.Model.RoundRobin, defaults)
	if err != nil {
		return model.Selection{}, err
	}

	selectionTime := time.Now()

	return backend.ResolveAvailable(
		requestedSelection,
		model.Selection{},
		configuredSelections,
		location.GetModelRoundRobinPath(),
		func(candidate model.Selection) bool {
			return usage.IsSelectionAvailable(
				location.GetUsageCachePath(candidate.Provider, hasOwnCaches),
				candidate.Model,
				selectionTime,
			)
		},
	)
}

func worldCaps(lastingWorld world.World) caps.Set {
	grantedCaps := caps.Read
	if lastingWorld.Files.IsWritable {
		grantedCaps |= caps.Write
	}

	return grantedCaps
}

func buildTools(
	lastingWorld world.World,
	mode *caps.Mode,
	askBroker *ask.Broker,
	writeEscape notify.EscapeWriter,
	isTerminalFocused func() bool,
) ([]tool.Tool, error) {
	declarations, err := lastingWorld.Declarations()
	if err != nil {
		return nil, err
	}

	tools := make([]tool.Tool, 0, len(lastingWorld.Tools))
	for _, declaration := range declarations {
		builtTool, err := command.New(declaration, command.Options{
			Directory: filepath.Dir(lastingWorld.Path),
			Approve: func(ctx context.Context, name string, line string) error {
				questionContext, cancel := context.WithTimeout(ctx, approvalLimit)
				defer cancel()

				return ask.Confirm(questionContext, askBroker, ask.Confirmation{
					Label:    "Run the " + name + " tool?",
					Detail:   line,
					Language: "bash",
				})
			},
		})
		if err != nil {
			return nil, err
		}
		tools = append(tools, builtTool)
	}

	referencedNames, err := referencedTools(lastingWorld, mode, writeEscape, isTerminalFocused)
	if err != nil {
		return nil, err
	}

	return toolset.Combine(referencedNames, tools)
}

func referencedTools(
	lastingWorld world.World,
	mode *caps.Mode,
	writeEscape notify.EscapeWriter,
	isTerminalFocused func() bool,
) ([]tool.Tool, error) {
	names := lastingWorld.ReferenceNames()
	if len(names) == 0 {
		return nil, nil
	}

	var offeredTools []tool.Tool

	if slices.ContainsFunc(names, func(name string) bool {
		return slices.Contains(toolbox.PathToolNames, name)
	}) {
		rootHandle, err := os.OpenRoot(lastingWorld.Files.Root)
		if err != nil {
			return nil, fmt.Errorf("files.root: %w", err)
		}

		offeredTools = append(offeredTools, toolbox.Rummage(
			file.New(rootHandle, caps.RefuseWrite(mode)), file.NewSnapshots(),
		)...)
	}

	offeredTools = append(offeredTools, title.New(), notify.New(writeEscape, isTerminalFocused))

	presentNames, absent := toolset.Partition(offeredTools, names)
	if len(absent) > 0 {
		return nil, fmt.Errorf("unknown tools: %s", strings.Join(absent, ", "))
	}

	return toolset.Reduce(offeredTools, presentNames)
}
