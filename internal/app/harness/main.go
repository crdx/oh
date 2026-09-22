package harness

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/jobs"
	"crdx.org/oh/internal/money"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/internal/sandbox/keeper"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/tool/command"
	"crdx.org/oh/pkg/tool/middleware/truncate"
	"crdx.org/oh/pkg/toolbox"
	"crdx.org/oh/pkg/toolbox/bash"
	"crdx.org/oh/pkg/toolbox/expose"
	"crdx.org/oh/pkg/toolbox/fetch"
	"crdx.org/oh/pkg/toolbox/lookup"
	"crdx.org/oh/pkg/toolbox/notify"
	"crdx.org/oh/pkg/toolbox/title"

	"crdx.org/oh/internal/app/backend"
	"crdx.org/oh/internal/app/bar"
	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/cli"
	"crdx.org/oh/internal/app/commands"
	"crdx.org/oh/internal/app/conditions"
	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/ctl"
	"crdx.org/oh/internal/app/cycle"
	"crdx.org/oh/internal/app/demo"
	"crdx.org/oh/internal/app/drops"
	"crdx.org/oh/internal/app/editor"
	"crdx.org/oh/internal/app/experimental"
	"crdx.org/oh/internal/app/graphics"
	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/menu"
	"crdx.org/oh/internal/app/metrics"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/notification"
	"crdx.org/oh/internal/app/onboarding"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/permission"
	"crdx.org/oh/internal/app/pictures"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/prompt"
	"crdx.org/oh/internal/app/record"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/sessions"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/skill"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/app/startup"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/terminal"
	"crdx.org/oh/internal/app/textsizing"
	"crdx.org/oh/internal/app/toolresult"
	"crdx.org/oh/internal/app/toolset"
	"crdx.org/oh/internal/app/tty"
	"crdx.org/oh/internal/app/usage"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/pkg/ask"
)

const approvalLimit = time.Minute

type approval struct {
	label    string
	language string
	action   string
	outcome  string
	advice   string
}

var (
	hostNetworkApproval = approval{
		label:    "Run this command with host networking?",
		language: "bash",
		action:   "host-network access",
		outcome:  "command did not run",
		advice:   "retry without it or choose another approach",
	}
	lookupApproval = approval{
		label:   "Look this up on the web?",
		action:  "lookup",
		outcome: "lookup did not run",
		advice:  "choose another approach",
	}
	fetchApproval = approval{
		label:   "Fetch this page?",
		action:  "fetch",
		outcome: "fetch did not run",
		advice:  "choose another approach",
	}
)

func customToolApproval(name string) approval {
	return approval{
		label:    "Run the " + name + " tool?",
		language: "bash",
		action:   name,
		outcome:  name + " did not run",
		advice:   "choose another approach",
	}
}

func (self approval) ask(
	ctx context.Context,
	broker *ask.Broker,
	rule permission.Rule,
	subject string,
) error {
	if rule == permission.Allow {
		return nil
	}

	return self.confirm(ctx, broker, subject)
}

func (self approval) confirm(ctx context.Context, broker *ask.Broker, subject string) error {
	questionContext, cancel := context.WithTimeout(ctx, approvalLimit)
	defer cancel()

	err := ask.Confirm(questionContext, broker, ask.Confirmation{
		Label:    self.label,
		Detail:   subject,
		Language: self.language,
	})

	switch {
	case errors.Is(err, ask.ErrDenied):
		return errors.New(self.action + " refused; " + self.outcome + "; " + self.advice)
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New(
			"approval timed out after " + util.CompactDuration(approvalLimit) + "; " + self.outcome,
		)
	case errors.Is(err, ask.ErrUnavailable):
		return errors.New("approval unavailable; " + self.outcome)
	}

	return err
}

func approveHostNetwork(
	ctx context.Context,
	broker *ask.Broker,
	rule permission.Rule,
	command string,
) error {
	return hostNetworkApproval.ask(ctx, broker, rule, strings.Join(bash.Steps(command), "\n"))
}

var completableToolNames = []string{
	"read",
	"ls",
	"find",
	"grep",
	"write",
	"edit",
	"bash",
	"job",
	"notify",
	title.Name,
	"lookup",
	"fetch",
}

func completableTools() []string {
	names := slices.Clone(completableToolNames)

	settings, err := config.Load(location.GetConfigFile())
	if err != nil {
		return names
	}

	for _, name := range settings.CustomToolNames() {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}

	return names
}

func Main() {
	if len(os.Args) > 1 && os.Args[1] == ctl.Flag {
		os.Exit(ctl.Run(os.Args[2:]))
	}

	sandbox.Init()

	if request, isRequested, err := toolresult.ParseRequest(os.Args[1:]); isRequested {
		style.Init(os.Stdout)
		if err == nil {
			err = toolresult.Show(request.URL, request.ShouldPage)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, style.Error(err))
			os.Exit(1)
		}
		return
	}

	if cli.WriteCompletions(os.Stdout, os.Args[1:], cli.Sources{
		ModelCachePath: location.GetModelCachePath(os.Getenv(backend.EndpointVariable) != ""),
		SessionsDir:    location.GetSessionsDir(),
		ToolNames:      completableTools(),
	}) {
		return
	}

	style.Init(os.Stdout)

	hooks := cycle.NewHooks(func(err error) {
		fmt.Fprintln(os.Stderr, style.Error(fmt.Errorf("session hook: %w", err)))
	})
	transition := cycle.Transition{}
	chosenSession, err := run(hooks, &transition)
	if err != nil {
		fmt.Fprintln(os.Stderr, style.Error(err))
		os.Exit(1)
	}

	if chosenSession != "" {
		transition = cycle.Transition{Kind: cycle.ResumeSession, Arguments: []string{"-r", chosenSession}}
	}
	if transition.Kind == cycle.Quit {
		return
	}

	terminal.ResetScrollback(os.Stdout)

	self, err := os.Executable()
	if err == nil {
		arguments := append([]string{self}, cli.InheritedOptions(os.Args[1:], transition.Kind)...)
		arguments = append(arguments, transition.Arguments...)
		err = syscall.Exec(self, arguments, os.Environ()) //nolint:gosec // re-executing the binary itself
	}

	fmt.Fprintln(os.Stderr, style.Error(fmt.Errorf("could not open the session: %w", err)))
	os.Exit(1)
}

func isTerminalLocal() bool {
	return os.Getenv("SSH_CLIENT") == "" && os.Getenv("SSH_TTY") == "" && os.Getenv("SSH_CONNECTION") == ""
}

func shadowedScratch(tmpDir string, isYolo bool) string {
	if isYolo {
		return ""
	}

	return tmpDir
}

func getConfigSources(workspaceDir string) []config.Source {
	return []config.Source{
		{Path: location.GetConfigFile()},
		{Path: filepath.Join(workspaceDir, "oh.toml"), IsOverride: true},
	}
}

func configuredRotation(settings config.Config, isSimulated bool) []string {
	if isSimulated {
		return nil
	}

	return settings.Model.RoundRobin
}

func sessionDefaults(selection model.Selection) model.Defaults {
	return model.Defaults{Effort: model.Effort(selection.Effort), IsFast: selection.IsFast}
}

func applyDefaultCaps(options *cli.Options, settings config.Config) {
	if !options.WereCapsChosen {
		options.Caps = caps.Set(settings.Caps.Default)
	}
}

func ensureCurrency(ctx context.Context, output io.Writer, code string, isSimulated bool) money.Currency {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" || code == money.DollarCode || isSimulated {
		return money.Dollar()
	}

	path := location.GetExchangeRateCachePath()

	if err := money.Ensure(ctx, "", path, code); err != nil {
		_, _ = fmt.Fprintln(output, style.Change("exchange rate not refreshed: %s", err))
	}

	return money.Load(path, code)
}

var unsandboxedToolNames = []string{"bash", "job"}

func requireDenyEnforcement(isYolo bool, offeredTools []string, patterns []string) error {
	if !isYolo || len(patterns) == 0 {
		return nil
	}

	for _, name := range unsandboxedToolNames {
		if toolset.Offers(offeredTools, name) {
			return errors.New("sandbox.deny cannot be enforced for the " + name + " tool under --yolo")
		}
	}

	return nil
}

func applySimulationOptions(options *cli.Options) {
	options.Yolo = true
	options.Tools = demo.Tools()
}

//nolint:gocyclo // lol no
func run(hooks *cycle.Hooks, requestedTransition *cycle.Transition) (string, error) {
	ctx := context.Background()
	var sessionInfo cycle.Session
	hasStarted := false
	stopReason := cycle.StoppedByFailure
	defer func() {
		if hasStarted {
			hooks.EmitSessionStopped(ctx, cycle.SessionStopped{Session: sessionInfo, Reason: stopReason})
		}
	}()

	inputArgs := cli.Bind()

	isPromptPiped := !tty.Is(os.Stdin)

	if err := inputArgs.Check(isPromptPiped); err != nil {
		return "", err
	}

	keyboard, releaseKeyboard := tty.Keyboard(os.Stdin)
	defer releaseKeyboard()

	notices := io.Writer(os.Stdout)
	if inputArgs.IsPrinting {
		notices = os.Stderr
	}

	endpointURL := os.Getenv(backend.EndpointVariable)
	modelCachePath := location.GetModelCachePath(endpointURL != "")
	sessionsDir := location.GetSessionsDir()

	if inputArgs.Version {
		fmt.Println(cli.Version())
		return "", nil
	}

	if inputArgs.Login {
		err := onboarding.Login(inputArgs.Provider, keyboard, os.Stdout)
		if errors.Is(err, onboarding.ErrCancelled) {
			return "", nil
		}
		return "", err
	}

	if inputArgs.Usage {
		return "", usage.Show(ctx, os.Stdout, usage.Options{JSON: inputArgs.JSON})
	}

	if inputArgs.List {
		return "", model.List(os.Stdout, modelCachePath)
	}

	workspace, err := work.Current()
	if err != nil {
		return "", err
	}

	workspaceDir := workspace.GetDir()

	if inputArgs.IsSessionPicker {
		return sessions.Choose(sessionsDir, workspace, keyboard, os.Stdout)
	}

	configSources := getConfigSources(workspaceDir)
	configPath := configSources[0].Path
	isSimulated := inputArgs.IsDemoing

	if !isSimulated {
		_, isChosen, err := onboarding.PrepareConfig(onboarding.Options{
			Input:          keyboard,
			Output:         os.Stdout,
			EndpointURL:    endpointURL,
			RequestedModel: inputArgs.Model,
			ResumedSession: inputArgs.Session,
			ConfigSources:  configSources,
			IsPrinting:     inputArgs.IsPrinting,
		})
		if err != nil {
			if errors.Is(err, onboarding.ErrCancelled) {
				return "", nil
			}

			return "", err
		}

		isSimulated = isChosen
	}

	if isSimulated {
		simulation, err := demo.Start()
		if err != nil {
			return "", err
		}
		defer simulation.Close()

		endpointURL = simulation.EndpointURL
		inputArgs.Model = simulation.Selection
		modelCachePath = location.GetModelCachePath(true)
		sessionsDir = location.GetSessionsDir()
	}

	settings, configObserver, err := config.ObserveSources(configSources...)
	if err != nil {
		return "", err
	}
	defer configObserver.Close()

	style.ApplyTheme(settings.Ui.Theme)
	editorConfiguration := editor.NewConfiguration(settings.Editor.Command)
	toolOutputLimit := truncate.NewLimit(settings.Tool.Output.Bytes)
	experimentalToggles := experimental.New(settings.Experimental)

	endpoints := backend.EndpointSettings{
		OverrideURL: endpointURL,
		OllamaHost:  settings.Provider.Ollama.Host,
	}
	listProviderModels := func(ctx context.Context, providerName string) ([]agent.Model, error) {
		return backend.ListModels(ctx, providerName, endpoints)
	}

	if inputArgs.Update {
		return "", model.Update(os.Stdout, endpointURL, modelCachePath, listProviderModels, inputArgs.IsShowingIgnored)
	}

	if err := model.Ensure(notices, endpointURL, modelCachePath, listProviderModels); err != nil {
		return "", err
	}

	currency := ensureCurrency(ctx, notices, settings.Ui.Currency, isSimulated)

	if inputArgs.IsModelPicker {
		var chosenModel model.Selection
		var err error
		startup.Wait(func() {
			chosenModel, err = model.Choose(
				modelCachePath, currency, backend.IsLoggedIn, keyboard, os.Stdout, settings.Model.GetDefaults(),
			)
		})
		if errors.Is(err, menu.ErrCancelled) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		inputArgs.Model = chosenModel.String()
	}

	args, err := inputArgs.Parse(modelCachePath, settings.Model.GetDefaults())
	if err != nil {
		return "", err
	}
	applyDefaultCaps(&args, settings)

	if isSimulated {
		applySimulationOptions(&args)
	}

	if err := sessions.ValidateStoredFormats(sessionsDir); err != nil {
		return "", err
	}

	if err := sessions.RefreshListing(sessionsDir, notices, args.Session); err != nil {
		return "", err
	}

	forkSource, err := sessions.GetForkSource(sessionsDir, workspace, args.SourceSession, args.Message)
	if err != nil {
		return "", err
	}
	if forkSource != nil {
		args.Message = forkSource.GetInitialUserMessage(forkSource.DroppedChatName)
	}

	resumedSession, err := sessions.LoadForResume(sessionsDir, workspace, args.Session)
	if err != nil {
		return "", err
	}

	args.Caps, err = sessions.OpeningCaps(args.Caps, args.WereCapsChosen, resumedSession)
	if err != nil {
		return "", err
	}

	args.Yolo, err = sessions.OpeningConfinement(args.Yolo, resumedSession)
	if err != nil {
		return "", err
	}

	if err := workspace.Validate(); err != nil {
		return "", err
	}

	if err := workspace.Open(); err != nil {
		return "", err
	}

	defer func() { _ = workspace.Close() }()

	if err := requireDenyEnforcement(args.Yolo, args.Tools, settings.Sandbox.Deny); err != nil {
		return "", err
	}

	if !args.Yolo {
		if err := shell.RequireSandbox(ctx); err != nil {
			return "", err
		}
	}

	homeDir := location.GetShellHomeDir()
	if homeDir == "" {
		return "", errors.New("could not find a home for shell configuration")
	}

	homeDir, err = filepath.Abs(homeDir)
	if err != nil {
		return "", fmt.Errorf("could not resolve the shell home path: %w", err)
	}

	if err := os.MkdirAll(homeDir, 0o700); err != nil {
		return "", fmt.Errorf("could not prepare the shell home: %w", err)
	}

	mode := caps.NewMode(args.Caps)

	files := file.New(workspace.GetRoot(), caps.RefuseWrite(mode))
	homeRoot, err := shell.MountHomeDirectory(files, homeDir, mode)
	if err != nil {
		return "", err
	}
	defer func() { _ = homeRoot.Close() }()

	configuredModels, err := model.ParseRoundRobin(
		modelCachePath, configuredRotation(settings, isSimulated), settings.Model.GetDefaults(),
	)
	if err != nil {
		return "", err
	}
	settings.Sandbox, err = shell.PreparePaths(settings.Sandbox, os.Stderr)
	if err != nil {
		return "", err
	}
	pathAccess, err := shell.NewPathAccess(files, mode, settings.Sandbox)
	if err != nil {
		return "", err
	}
	defer pathAccess.Close()

	builtinSkillDir := location.GetStateDir(skill.DirectoryName)
	if err := skill.Materialise(builtinSkillDir); err != nil {
		util.WriteWarningf(os.Stderr, "%v", err)
		builtinSkillDir = ""
	}

	globalSkillDirs := append(
		[]string{builtinSkillDir, location.GetConfigDir(skill.DirectoryName)},
		settings.Skills.Include...,
	)
	availableSkills, err := skill.Discover(workspace.GetDir(), globalSkillDirs, os.Stderr)
	if err != nil {
		return "", err
	}

	availableSkills = skill.ExcludeGlobal(availableSkills, settings.Skills.Exclude)

	skillRoots, err := skill.MountGlobalSkills(files, availableSkills)
	if err != nil {
		return "", err
	}
	defer skill.Close(skillRoots)

	selectionTime := time.Now()
	selection, err := backend.ResolveAvailable(
		args.Selection,
		sessions.ModelSelection(resumedSession),
		configuredModels,
		location.GetModelRoundRobinPath(),
		func(candidate model.Selection) bool {
			return usage.IsSelectionAvailable(
				location.GetUsageCachePath(candidate.Provider, endpointURL != ""),
				candidate.Model,
				selectionTime,
			)
		},
	)
	if err != nil {
		startup.Wait(func() {
			selection, err = model.ChooseWhenNoneSelected(
				err, modelCachePath, currency, backend.IsLoggedIn, keyboard, os.Stdout, settings.Model.GetDefaults(),
			)
		})
	}
	if errors.Is(err, menu.ErrCancelled) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	choice, err := model.Chosen(modelCachePath, selection.Provider, selection.Model)
	if err != nil {
		return "", err
	}

	client, err := backend.Connect(choice, selection, endpoints)
	if err != nil {
		return "", err
	}

	meta := store.Meta{
		Model:        selection.Model,
		WorkspaceDir: workspaceDir,
		Provider:     selection.Provider,
		Effort:       selection.Effort,
		IsFast:       selection.IsFast,
		Yolo:         args.Yolo,
	}

	log, err := sessions.OpenWriter(sessionsDir, resumedSession, meta)
	if err != nil {
		return "", err
	}
	client.UseSession(log.ID())

	sessionInfo = cycle.Session{
		Name:         log.Name(),
		ID:           log.ID(),
		Directory:    filepath.Join(sessionsDir, log.Name()),
		WorkspaceDir: workspaceDir,
		Provider:     selection.Provider,
		Model:        selection.Model,
		Effort:       selection.Effort,
	}
	defer func() { _ = log.Close() }()
	hooks.EmitSessionStarting(ctx, cycle.SessionStarting{Session: sessionInfo})
	client.ObserveHTTP(log.Observer())

	tmpDir, err := sessions.PrepareTemporaryDirectory(log.Name())
	if err != nil {
		return "", err
	}

	if err := shell.PrepareHomeMappings(
		workspace.GetDir(), homeDir, tmpDir, settings.Sandbox, mode.Current(),
	); err != nil {
		return "", err
	}
	cacheRoot, err := shell.MountHomeCache(files, homeDir)
	if err != nil {
		return "", err
	}
	defer func() { _ = cacheRoot.Close() }()

	dropKeeper, err := drops.Open(files, sessionInfo.Directory, log.EnsurePersisted)
	if err != nil {
		return "", err
	}
	defer func() { _ = dropKeeper.Close() }()

	defer func() {
		if !log.IsPersisted() {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	if isPromptPiped {
		pipedPrompt, err := startup.ReadPipedPrompt(os.Stdin)
		if err != nil {
			return "", err
		}
		args.Message = startup.JoinPipedPrompt(args.Message, pipedPrompt)
	}

	if len(args.AddedFiles) > 0 {
		initialFilesMessage, err := startup.PrepareInitialFiles(args.AddedFiles, tmpDir)
		if err != nil {
			return "", err
		}
		args.Message = startup.JoinPrompt(initialFilesMessage, args.Message)
	}

	sandboxRunner, jobManager, keeperProcess, closeKeeper, keeperRefusal := openRunner(ctx, args.Yolo)
	defer closeKeeper()

	if jobManager != nil {
		defer func() { _ = jobManager.Close() }()
	}
	if keeperRefusal != nil {
		_, _ = fmt.Fprintln(notices, style.Change(
			"background jobs and inter-command networking are unavailable: "+keeperRefusal.Error(),
		))
	}

	currentConditions := conditions.Probe(args.Yolo, !args.IsPrinting)
	var createdConditions *conditions.Conditions
	var recordedEvents []agent.Event
	if resumedSession != nil {
		createdConditions = resumedSession.Meta.Conditions
		recordedEvents = resumedSession.Events
	}
	restoredConditions, err := conditions.Restore(createdConditions, recordedEvents, currentConditions)
	if err != nil {
		return "", err
	}

	var systemPrompt string
	if resumedSession != nil && resumedSession.Meta.SystemPrompt != "" {
		systemPrompt = resumedSession.Meta.SystemPrompt
	} else {
		systemPrompt, _, err = prompt.Load(prompt.Config{
			GlobalPath:     location.GetGlobalContextPath(),
			Workspace:      workspace,
			SessionName:    log.Name(),
			SessionsDir:    sessionsDir,
			SessionDir:     filepath.Join(sessionsDir, log.Name()),
			ConfigFile:     location.GetConfigFile(),
			TmpDir:         tmpDir,
			HomeDir:        homeDir,
			CurrentCaps:    args.Caps,
			ExtraPaths:     settings.Sandbox,
			DropsDirectory: dropKeeper.GetDirectory(),
			Skills:         availableSkills,
			OfferedTools:   args.Tools,
			Conditions:     currentConditions,
			JobsGranted:    jobManager != nil,
			NetworkGranted: args.Caps.Has(caps.Network),
			Yolo:           args.Yolo,
		})
		if err != nil {
			return "", err
		}
	}
	systemPrompt = prompt.WithDropsDirectory(systemPrompt, dropKeeper.GetDirectory())

	tmpRoot, err := shell.MountTemporaryDirectory(files, tmpDir)
	if err != nil {
		return "", err
	}
	defer func() { _ = tmpRoot.Close() }()

	pathGrants := pathgrant.New(workspace, pathAccess)
	var pathGrantRestoreResult pathgrant.RestoreResult
	if resumedSession != nil {
		if recordedGrants, found := pathgrant.LastRecorded(resumedSession.Events); found {
			pathGrants, pathGrantRestoreResult = pathgrant.NewRestored(workspace, pathAccess, recordedGrants)
		}
	}

	hostToSandboxAddress := portgrant.AddressFor(log.Name())
	hostToSandboxHostname := settings.Ports.GetHostname(log.Name(), hostToSandboxAddress)
	hostToSandboxExposer := newHostToSandboxExposer(ctx, keeperProcess, hostToSandboxAddress)
	var hostToSandbox *portgrant.HostToSandbox
	sandboxToHostExposer := newSandboxToHostExposer(ctx, keeperProcess)
	sandboxToHost := portgrant.NewSandboxToHost(sandboxToHostExposer, func() []uint16 {
		if hostToSandbox == nil {
			return nil
		}
		return hostToSandbox.GetCurrent()
	})
	var sandboxToHostRestoreResult portgrant.SandboxToHostRestoreResult
	if resumedSession != nil {
		if recordedPorts, found := portgrant.LastRecordedSandboxToHost(resumedSession.Events); found {
			sandboxToHost, sandboxToHostRestoreResult = portgrant.NewRestoredSandboxToHost(
				sandboxToHostExposer,
				func() []uint16 {
					if hostToSandbox == nil {
						return nil
					}
					return hostToSandbox.GetCurrent()
				},
				recordedPorts,
			)
		}
	}

	hostToSandbox = portgrant.NewHostToSandbox(hostToSandboxExposer, hostToSandboxHostname)
	hostToSandbox.SetSandboxToHostPorts(sandboxToHost.GetCurrent)
	var hostToSandboxRestoreResult portgrant.HostToSandboxRestoreResult
	if resumedSession != nil {
		if recordedPorts, found := portgrant.LastRecordedHostToSandbox(resumedSession.Events); found {
			hostToSandbox, hostToSandboxRestoreResult = portgrant.NewRestoredHostToSandbox(
				hostToSandboxExposer, hostToSandboxHostname, recordedPorts,
			)
			hostToSandbox.SetSandboxToHostPorts(sandboxToHost.GetCurrent)
		}
	}

	screen := output.New(os.Stdout).LinkPathsUnder(link.Roots{
		Workspace: workspace.GetDir(),
		Scratch:   shadowedScratch(tmpDir, args.Yolo),
	})
	if args.IsPrinting {
		screen.AppendOnly()
	} else {
		screen.SetTextSizingSupported(textsizing.Detect(keyboard, os.Stdout))
	}

	var app *App
	isTerminalFocused := func() bool { return app != nil && app.terminal.IsFocused() }

	snapshots := file.NewSnapshots()
	toolboxTools := toolbox.Rummage(files, snapshots)
	askBroker := ask.New()
	permissionSet, err := settings.BuildPermissions()
	if err != nil {
		return "", err
	}
	permissions := permission.New(permissionSet)
	approveNetwork := func(ctx context.Context, command string) error {
		return approveHostNetwork(ctx, askBroker, permissions.Network(), command)
	}
	shellTool := shell.New(
		workspace.GetDir(), homeDir, tmpDir, pathAccess, mode, files, args.Yolo, approveNetwork, sandboxRunner,
	)

	toolboxTools = append(toolboxTools, shellTool)

	doesWake := !args.IsPrinting

	if jobManager != nil {
		toolboxTools = append(toolboxTools, shell.NewJob(
			jobManager,
			workspace.GetDir(),
			homeDir,
			tmpDir,
			pathAccess,
			mode,
			files,
			args.Yolo,
			doesWake,
		))
	}
	if keeperProcess != nil {
		toolboxTools = append(toolboxTools, expose.New(hostToSandbox.ForModel()))
	}
	if notify.IsAvailable() {
		toolboxTools = append(toolboxTools, notify.New(screen.WriteEscape, isTerminalFocused))
	}
	toolboxTools = append(toolboxTools, title.New())
	toolboxTools = append(
		toolboxTools,
		lookup.New(
			func() bool { return mode.Current().Has(caps.Lookup) },
			func(ctx context.Context, query string) error {
				return lookupApproval.ask(ctx, askBroker, permissions.Lookup(), query)
			},
			client.Search,
		),
		fetch.New(
			func() bool { return mode.Current().Has(caps.Network) },
			func(ctx context.Context, address string) error {
				return fetchApproval.ask(ctx, askBroker, permissions.Fetch(), address)
			},
			dropKeeper.SaveHTML,
		),
	)
	customTools, err := settings.BuildCustomTools(command.Options{
		Directory: workspace.GetDir(),
		Approve: func(ctx context.Context, name string, line string) error {
			return customToolApproval(name).confirm(ctx, askBroker, line)
		},
	})
	if err != nil {
		return "", err
	}
	if toolboxTools, err = toolset.Combine(toolboxTools, customTools); err != nil {
		return "", err
	}

	toolboxTools = truncate.Tools(toolboxTools, toolOutputLimit)

	enabledToolNames := args.Tools
	if resumedSession != nil && len(resumedSession.Meta.Tools) > 0 {
		var absentToolNames []string
		enabledToolNames, absentToolNames = toolset.Partition(toolboxTools, resumedSession.Meta.Tools)
		if len(absentToolNames) > 0 {
			_, _ = fmt.Fprintln(notices, style.Change(
				"tools used by this conversation are no longer offered, so its prompt cache will be "+
					"rebuilt: "+strings.Join(absentToolNames, ", "),
			))
		}
	}

	enabledTools, err := toolset.Reduce(toolboxTools, enabledToolNames)
	if err != nil {
		return "", err
	}

	if resumedSession == nil {
		meta.SystemPrompt = systemPrompt
		meta.Tools = toolset.Names(enabledTools)
		meta.Conditions = &currentConditions
		if err := log.SetMeta(meta); err != nil {
			return "", err
		}
	}

	if forkSource != nil {
		transcriptPath, err := forkSource.CopyChat(dropKeeper)
		if err != nil {
			return "", fmt.Errorf("copy the forked session chat: %w", err)
		}
		args.Message = forkSource.GetMessageWithChatAt(args.Message, transcriptPath)
	}

	systemCommands, err := commands.New(commands.Options{
		ConfigDir:        location.GetConfigDir(),
		ConfigFile:       configPath,
		SystemPromptFile: location.GetGlobalContextPath(),
		SkillDirs:        globalSkillDirs,
		Workspace:        workspace,
		ScratchDir:       tmpDir,
		HomeDir:          homeDir,
		Editor:           editorConfiguration,
		Output:           os.Stdout,
		PathGrants: commands.PathGrants{
			Grant: pathGrants.Grant,
			Revoke: func(path string) (agent.Event, error) {
				event, err := pathGrants.Revoke(path)
				if err != nil {
					return event, err
				}
				app.stopJobsHoldingPath(path)

				return event, nil
			},
			GetCurrent: pathGrants.GetCurrent,
		},
		HostToSandbox: commands.HostToSandbox{
			Hide:       hostToSandbox.Hide,
			GetCurrent: hostToSandbox.GetCurrent,
			GetURL:     hostToSandbox.URL,
		},
		SandboxToHost: commands.SandboxToHost{
			Expose:     sandboxToHost.Expose,
			Revoke:     sandboxToHost.Revoke,
			GetCurrent: sandboxToHost.GetCurrent,
		},
		Jobs:        managedJobs(jobManager),
		LimitOutput: func(output string) string { return app.withinToolOutputLimit(output) },
		GetInfo: func() (string, error) {
			return app.display.bar.RenderInfo(segment.Context{})
		},
		Session: commands.Session{
			Name:           log.Name(),
			ID:             log.ID(),
			Directory:      filepath.Join(sessionsDir, log.Name()),
			IsPersisted:    log.IsPersisted,
			GetLastMessage: func() (string, bool) { return app.getLastMessage() },
		},
		StartSession: func(start commands.SessionStart) error {
			var transition cycle.Transition
			var err error
			if start.SourceSessionName != "" {
				transition, err = cycle.ForkedSessionTransition(
					start.ModelGlob,
					model.Choices(modelCachePath),
					sessionDefaults(selection),
					start.SourceSessionName,
				)
			} else {
				transition, err = cycle.NewSessionTransition(
					start.ModelGlob,
					model.Choices(modelCachePath),
					sessionDefaults(selection),
				)
			}
			if err != nil {
				return err
			}
			return app.requestTransition(transition)
		},
	})
	if err != nil {
		return "", err
	}

	usageCachePath := location.GetUsageCachePath(selection.Provider, endpointURL != "")
	providerClient := usage.Guard(ctx, client.Client, usage.GuardSettings{
		ProviderName: model.ProviderName(selection.Provider),
		ModelName:    selection.Model,
		CachePath:    usageCachePath,
		Now:          time.Now,
	})

	app = &App{
		agent:    agent.NewWithEnabledTools(systemPrompt, providerClient, toolboxTools, enabledTools),
		screen:   screen,
		terminal: terminal.New(os.Stdout, workspace),
		metrics: metrics.New(metrics.Settings{
			ContextWindowTokens: choice.ContextWindowTokens,
			Prices:              choice.Prices,
		}),
		recorder:        record.New(log),
		editorConfig:    editorConfiguration,
		toolOutputLimit: toolOutputLimit,
		experimental:    experimentalToggles,
		workspace:       workspace,
		mode:            mode,
		conditions:      restoredConditions.State,
		pathGrants:      pathGrants,
		hostToSandbox:   hostToSandbox,
		sandboxToHost:   sandboxToHost,
		jobs: jobState{
			manager:  jobManager,
			doesWake: doesWake,
		},
		configObserver: configObserver,
		runMode:        runMode{isPrinting: args.IsPrinting, isYolo: args.Yolo, isSimulated: isSimulated},
		question:       questionState{broker: askBroker},
		startedAt:      util.WallClock(time.Now()),
		keyboard:       keyboard,
	}
	if resumedSession == nil && model.SupportsFastMode(selection.Provider) {
		app.openingEvents = []agent.Event{model.FastModeEvent(selection.IsFast)}
	}
	if restoredConditions.IsChanged {
		app.pendingNotices.add(restoredConditions.Change)
	}
	app.onFailure = func(failure error) {
		_ = notification.SendTurnError(
			context.Background(), screen.WriteEscape, isTerminalFocused, workspace, failure,
		)
	}
	app.onQuestion = func(question ask.Question) {
		go func() {
			_ = notification.SendQuestion(
				context.Background(), screen.WriteEscape, isTerminalFocused, workspace, question,
			)
		}()
	}
	app.savePastedImage = dropKeeper.SaveImage
	toolOutputLimit.SaveOverflowWith(dropKeeper.SaveOutput)

	if cellWidth, cellHeight, hasGraphics := graphics.Detect(keyboard, os.Stdout); hasGraphics {
		app.display.pictures = pictures.Display{
			SessionDirectory: sessionInfo.Directory,
			ScratchDirectory: shadowedScratch(tmpDir, args.Yolo),
			CellWidth:        cellWidth,
			CellHeight:       cellHeight,
			IsLocal:          isTerminalLocal(),
		}

		app.agent.StorePicturesWith(func(picture tool.Image) *agent.Picture {
			reference, err := pictures.Store(sessionInfo.Directory, log.EnsurePersisted, picture)
			if err != nil {
				return nil
			}

			return reference
		})
	}

	usageReporter, _ := client.Client.(agent.UsageReporter)

	barRegistry := bar.NewRegistry(bar.Options{
		Workspace:             workspace,
		Session:               sessionInfo,
		ModelEffortLevels:     choice.EffortLevels,
		IsFast:                selection.IsFast,
		IsSimulated:           isSimulated,
		UsageReporter:         usageReporter,
		UsageCachePath:        usageCachePath,
		UsageIsSelfRefreshing: usageReporter != nil,
		UsageGauges:           usage.TerminalGauges(keyboard, os.Stdout),
		Currency:              currency,
		SandboxHostname:       hostToSandboxHostname,
		Sources:               app.getBarSources(),
	})
	liveConfig, err := settings.BuildLive(barRegistry)
	if err != nil {
		return "", err
	}
	commandRegistry, err := slash.NewRegistry(systemCommands, liveConfig.SnippetCommandSet)
	if err != nil {
		return "", err
	}
	app.slash.commands = commandRegistry
	app.permissions = permissions
	app.notifyUnknownSettings(liveConfig.UnknownSettings)
	app.continueMessage = liveConfig.ContinueMessage
	app.display.streamingMode = liveConfig.StreamingMode
	app.display.reasoningRendering = liveConfig.ReasoningRendering
	app.display.theme = liveConfig.Theme
	app.display.modelName = selection.Model
	screen.SetGrouping(liveConfig.Grouping)
	app.display.bar = bar.NewConfiguration(barRegistry, liveConfig.SegmentLayout)

	if resumedSession != nil {
		app.restore(resumedSession)
	}
	for _, failure := range hostToSandboxRestoreResult.Failures {
		correction, err := portgrant.HostToSandboxChangeEvent(
			hostToSandboxAddress,
			failure.Port,
			hostToSandbox.GetCurrent(),
		)
		if err != nil {
			return "", err
		}
		app.pendingNotices.add(correction)
		app.notifyFailure(fmt.Sprintf(
			"Port %d could not be exposed again: %v", failure.Port, failure.Err,
		))
	}
	for _, failure := range sandboxToHostRestoreResult.Failures {
		correction, err := portgrant.SandboxToHostChangeEvent(failure.Port, sandboxToHost.GetCurrent())
		if err != nil {
			return "", err
		}
		app.pendingNotices.add(correction)
		app.notifyFailure(fmt.Sprintf(
			"Host loopback port %d could not be exposed again: %v", failure.Port, failure.Err,
		))
	}
	for _, failure := range pathGrantRestoreResult.Failures {
		correction, err := pathgrant.ChangeEvent(failure.Grant.Path, pathGrants.GetCurrent())
		if err != nil {
			return "", err
		}
		app.queuePathGrantChange(correction)
		app.notifyFailure(fmt.Sprintf(
			"Temporary access to %s could not be restored: %v",
			failure.Grant.Path,
			failure.Err,
		))
	}

	projectSkills, globalSkills := skill.Counts(availableSkills)
	startupElapsed := startup.Elapsed()
	configOverride, hasLocalConfig := settings.GetOverride()
	var localConfig *startup.LocalConfig
	if hasLocalConfig {
		localConfig = &startup.LocalConfig{
			Name:     filepath.Base(configOverride.Path),
			Settings: configOverride.Settings,
		}
	}
	startupInfo := startup.Info{
		Session:       log.Name(),
		PromptBytes:   len(systemPrompt),
		ProjectSkills: projectSkills,
		GlobalSkills:  globalSkills,
		Snippets:      len(settings.Snippets),
		ToolBytes:     client.ToolsSize(enabledTools),
		LocalConfig:   localConfig,
	}
	if resumedSession == nil {
		app.notify(startup.NewEvent(startupElapsed, startupInfo))
	}

	hasStarted = true
	hooks.EmitSessionStarted(ctx, cycle.SessionStarted{Session: sessionInfo})
	transition := app.begin(args.Message)
	*requestedTransition = transition
	stopReason = transition.StopReason()
	hooks.EmitSessionStopping(ctx, cycle.SessionStopping{Session: sessionInfo, Reason: stopReason})

	if isSessionLeftToResume(log.IsPersisted(), isSimulated, transition.Kind) {
		_, _ = fmt.Fprintf(notices, "\n%s\n", style.Subtle(sessions.ResumeCommand(os.Args[0], log.Name())))
	}

	return "", nil
}

func isSessionLeftToResume(isPersisted bool, isSimulated bool, kind cycle.TransitionKind) bool {
	return isPersisted && !isSimulated && kind == cycle.Quit
}

func openRunner(
	ctx context.Context, isYolo bool,
) (sandbox.Runner, *jobs.Manager, *keeper.Keeper, func(), error) {
	if isYolo {
		return sandbox.Direct(), nil, nil, func() {}, nil
	}

	keeperProcess, err := keeper.Open(ctx)
	if err != nil {
		return sandbox.Direct(), nil, nil, func() {}, err
	}

	runner := sandbox.Wrapped(keeperProcess)

	return runner, jobs.New(runner), keeperProcess, func() { _ = keeperProcess.Close() }, nil
}

func newHostToSandboxExposer(
	ctx context.Context, keeperProcess *keeper.Keeper, host string,
) portgrant.HostToSandboxExposer {
	if keeperProcess == nil {
		return portgrant.HostToSandboxExposer{}
	}

	return portgrant.HostToSandboxExposer{
		Expose: func(port uint16) error { return keeperProcess.OpenHostToSandbox(ctx, host, port) },
		Hide:   keeperProcess.CloseHostToSandbox,
	}
}

func newSandboxToHostExposer(ctx context.Context, keeperProcess *keeper.Keeper) portgrant.SandboxToHostExposer {
	if keeperProcess == nil {
		return portgrant.SandboxToHostExposer{}
	}

	return portgrant.SandboxToHostExposer{
		Expose: func(port uint16) error { return keeperProcess.OpenSandboxToHost(ctx, port) },
		Hide:   keeperProcess.CloseSandboxToHost,
	}
}

func managedJobs(manager *jobs.Manager) commands.Jobs {
	if manager == nil {
		return commands.Jobs{}
	}

	return commands.Jobs{
		List:   manager.List,
		Status: manager.Status,
		Output: manager.Output,
		Stop: func(name string) (agent.Event, error) {
			snapshot, err := manager.Stop(name)
			if err != nil {
				return agent.Event{}, err
			}

			return caps.JobStoppedByUserEvent(snapshot.Name), nil
		},
		Discard:       manager.Discard,
		PruneFinished: manager.PruneFinished,
	}
}
