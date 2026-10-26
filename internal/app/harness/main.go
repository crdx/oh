package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"crdx.org/oh/internal/money"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/internal/sandbox/keeper"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/internal/util/diskutil"
	"crdx.org/oh/internal/util/pathutil"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/tool/command"
	"crdx.org/oh/pkg/tool/middleware/async"
	"crdx.org/oh/pkg/tool/middleware/truncate"
	"crdx.org/oh/pkg/toolbox/bash"
	"crdx.org/oh/pkg/toolbox/fetch"
	"crdx.org/oh/pkg/toolbox/forward"
	"crdx.org/oh/pkg/toolbox/lookup"
	"crdx.org/oh/pkg/toolbox/subagent"
	"crdx.org/oh/pkg/toolbox/task"
	"crdx.org/oh/pkg/toolbox/title"

	"crdx.org/oh/internal/app/backend"
	"crdx.org/oh/internal/app/background"
	"crdx.org/oh/internal/app/bar"
	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/cli"
	"crdx.org/oh/internal/app/commands"
	"crdx.org/oh/internal/app/conditions"
	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/contextsource"
	"crdx.org/oh/internal/app/ctl"
	"crdx.org/oh/internal/app/cycle"
	"crdx.org/oh/internal/app/demo"
	"crdx.org/oh/internal/app/diagnostics"
	"crdx.org/oh/internal/app/editor"
	"crdx.org/oh/internal/app/environment"
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
	"crdx.org/oh/internal/app/painter"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/pathref"
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
	"crdx.org/oh/internal/app/stash"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/terminal"
	"crdx.org/oh/internal/app/textsizing"
	"crdx.org/oh/internal/app/toolresult"
	"crdx.org/oh/internal/app/toolset"
	"crdx.org/oh/internal/app/trigger"
	"crdx.org/oh/internal/app/tty"
	"crdx.org/oh/internal/app/usage"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/pkg/ask"
)

const (
	approvalLimit               = permission.DefaultApprovalTimeout
	notificationWithdrawalGrace = 2 * time.Second
	sessionNameStaleAfter       = 5 * time.Second
)

type approval struct {
	label    string
	language string
	action   string
	outcome  string
}

var (
	hostNetworkApproval = approval{
		label:    "Run this command in the sandbox with host networking?",
		language: "bash",
		action:   "host-network access",
		outcome:  "command did not run",
	}
	lookupApproval = approval{
		label:   "Look this up on the web?",
		action:  "lookup",
		outcome: "lookup did not run",
	}
	fetchApproval = approval{
		label:   "Fetch this page?",
		action:  "fetch",
		outcome: "fetch did not run",
	}
)

func customToolApproval(name string) approval {
	return approval{
		label:   "Run the " + name + " tool?",
		action:  name,
		outcome: name + " did not run",
	}
}

func (self approval) ask(
	ctx context.Context,
	broker *ask.Broker,
	setting permission.Setting,
	subject string,
) error {
	return self.askFor(ctx, broker, setting, self.confirmation(subject))
}

func (self approval) askFor(
	ctx context.Context,
	broker *ask.Broker,
	setting permission.Setting,
	confirmation ask.Confirmation,
) error {
	if setting.IsAllowed() {
		return nil
	}

	return self.confirmWith(ctx, broker, confirmation, setting.TimeoutOr(approvalLimit))
}

func (self approval) confirmation(subject string) ask.Confirmation {
	return ask.Confirmation{
		Label:    self.label,
		Detail:   subject,
		Language: self.language,
	}
}

func (self approval) confirmArguments(
	ctx context.Context,
	broker *ask.Broker,
	subject string,
	arguments tool.Arguments,
	timeout time.Duration,
) error {
	schema := arguments.Schema()
	fields := make([]ask.Field, 0, len(schema))
	if parameter := schema.Find(subject); parameter != nil && arguments.IsPresent(subject) {
		fields = append(fields, argumentField(arguments, *parameter, true))
	}
	for _, parameter := range schema {
		if parameter.Name == subject || !arguments.IsPresent(parameter.Name) {
			continue
		}

		fields = append(fields, argumentField(arguments, parameter, false))
	}

	return self.confirmWith(ctx, broker, ask.Confirmation{Label: self.label, Fields: fields}, timeout)
}

func argumentField(arguments tool.Arguments, parameter tool.Parameter, isSubject bool) ask.Field {
	value := arguments.GetText(parameter.Name)
	if parameter.Type == tool.TypeArray {
		value = strings.Join(arguments.GetStrings(parameter.Name), ", ")
	}
	return ask.Field{Name: parameter.Name, Value: value, IsSubject: isSubject}
}

func (self approval) confirmWith(
	ctx context.Context,
	broker *ask.Broker,
	confirmation ask.Confirmation,
	timeout time.Duration,
) error {
	err := ask.ConfirmWithin(ctx, broker, confirmation, timeout)

	switch {
	case errors.Is(err, ask.ErrDenied):
		return errors.New(self.action + " refused; " + self.outcome)
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New(
			"approval timed out after " + util.CompactDuration(timeout) + "; " + self.outcome,
		)
	case errors.Is(err, ask.ErrUnavailable):
		return errors.New("approval unavailable; " + self.outcome)
	}

	return err
}

func approveHostNetwork(
	ctx context.Context,
	broker *ask.Broker,
	setting permission.Setting,
	command string,
	intent string,
) error {
	confirmation := hostNetworkApproval.confirmation(strings.Join(bash.Steps(command), "\n"))
	confirmation.Intent = intent
	return hostNetworkApproval.askFor(ctx, broker, setting, confirmation)
}

var completableToolNames = []string{
	"read",
	"ls",
	"find",
	"grep",
	"write",
	"edit",
	"bash",
	"task",
	title.Name,
	"lookup",
	"fetch",
}

func completionSources() cli.Sources {
	sources := cli.Sources{
		ModelCachePath: location.GetModelCachePath(),
		SessionsDir:    location.GetSessionsDir(),
		ToolNames:      slices.Clone(completableToolNames),
		IsLoggedIn: func(providerName string) bool {
			return backend.IsAvailable(providerName, backend.EndpointSettings{
				OverrideURL: os.Getenv(backend.EndpointVariable),
			})
		},
	}

	settings, err := config.Load(location.GetConfigFile())
	if err != nil {
		return sources
	}
	for _, name := range settings.CustomToolNames() {
		if !slices.Contains(sources.ToolNames, name) {
			sources.ToolNames = append(sources.ToolNames, name)
		}
	}
	if toolGroups, err := settings.ToolGroups(); err == nil {
		sources.CustomCapFlags = toolGroups.CustomFlags()
	}

	return sources
}

type terminationSignalError struct {
	signal os.Signal
}

func (self terminationSignalError) Error() string {
	return self.signal.String()
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

	if cli.WriteCompletions(os.Stdout, os.Args[1:], completionSources) {
		return
	}

	style.Init(os.Stdout)
	if !isHelpOrVersion(os.Args[1:]) {
		if err := location.RequireCurrentDataLayout(); err != nil {
			fmt.Fprintln(os.Stderr, style.Error(err))
			os.Exit(1)
		}
	}
	startupConfig := readConfig()

	hooks := cycle.NewHooks(func(err error) {
		fmt.Fprintln(os.Stderr, style.Error(fmt.Errorf("session hook: %w", err)))
	})
	transition := cycle.Transition{}
	chosenSession, err := run(hooks, &transition, startupConfig)
	if err != nil {
		if interruption, isSignal := errors.AsType[terminationSignalError](err); isSignal {
			tty.Reraise(interruption.signal)
			return
		}
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
		arguments := append([]string{self}, cli.InheritedOptions(os.Args[1:], transition)...)
		arguments = append(arguments, transition.Arguments...)
		err = syscall.Exec(self, arguments, os.Environ()) //nolint:gosec // re-executing the binary itself
	}

	fmt.Fprintln(os.Stderr, style.Error(fmt.Errorf("could not open the session: %w", err)))
	os.Exit(1)
}

func isHelpOrVersion(arguments []string) bool {
	return slices.Contains(arguments, "-h") ||
		slices.Contains(arguments, "--help") ||
		slices.Contains(arguments, "-v") ||
		slices.Contains(arguments, "--version")
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

type initialConfig struct {
	observable config.Observable
	err        error
}

func readConfig() initialConfig {
	workspace, err := work.Current()
	if err != nil {
		return initialConfig{err: err}
	}
	observable, err := config.Read(getConfigSources(workspace.GetDir())...)
	if err == nil {
		style.ApplyTheme(observable.Config.Ui.Theme)
	}

	return initialConfig{observable: observable, err: err}
}

func configuredRotation(settings config.Config, isSimulated bool) []string {
	if isSimulated {
		return nil
	}

	return settings.Agent.Rotation()
}

func currentCustomCapFlags(sources []config.Source) string {
	settings, err := config.LoadSources(sources...)
	if err != nil {
		return ""
	}
	toolGroups, err := settings.ToolGroups()
	if err != nil {
		return ""
	}
	return toolGroups.CustomFlags()
}

type sessionStarter struct {
	toolboxTools      []tool.Tool
	getCustomCapFlags func() string
	isYoloInherited   bool
	getModelChoices   func() []model.Choice
	defaults          model.Defaults
}

func (self sessionStarter) transition(start commands.SessionStart) (cycle.Transition, error) {
	err := checkSessionStart(start, self.toolboxTools, self.getCustomCapFlags(), self.isYoloInherited)
	if err != nil {
		return cycle.Transition{}, err
	}

	options := cycle.SessionOptions{
		ModelGlob: start.ModelGlob,
		CapFlags:  start.CapFlags,
		Tools:     start.Tools,
		IsYolo:    start.IsYolo,
	}
	if start.SourceSessionName != "" {
		return cycle.ForkedSessionTransition(options, self.getModelChoices(), self.defaults, start.SourceSessionName)
	}
	return cycle.NewSessionTransition(options, self.getModelChoices(), self.defaults)
}

func checkSessionStart(
	start commands.SessionStart,
	toolboxTools []tool.Tool,
	customCapFlags string,
	isYoloInherited bool,
) error {
	if start.CapFlags != "" {
		if start.IsYolo || isYoloInherited {
			if err := cli.RefuseConfinedCaps(start.CapFlags); err != nil {
				return err
			}
		}
		if _, _, err := caps.ParseWithGroups(start.CapFlags, customCapFlags); err != nil {
			return err
		}
	}

	_, err := toolset.Reduce(toolboxTools, start.Tools)
	return err
}

func sessionDefaults(selection model.Selection) model.Defaults {
	return model.Defaults{Effort: model.Effort(selection.Effort), IsFast: selection.IsFast}
}

type sessionModelRequest struct {
	requestedSelection   model.Selection
	resumedSession       *store.Session
	settings             config.Config
	modelChoices         []model.Choice
	seenModelsPath       string
	modelCachePath       string
	currency             money.Currency
	isProviderAvailable  func(providerName string) bool
	keyboard             *os.File
	isSimulated          bool
	isEndpointOverridden bool
}

func chooseSessionModel(request sessionModelRequest) (model.Selection, model.Choice, error) {
	settings := request.settings
	configuredModels, err := model.ParseRoundRobin(
		request.modelChoices,
		request.seenModelsPath,
		configuredRotation(settings, request.isSimulated),
		settings.Defaults.ForSelections(),
	)
	if err != nil {
		return model.Selection{}, model.Choice{}, fmt.Errorf("%s: %w", settings.Agent.Setting(), err)
	}

	selectionTime := time.Now()
	selection, err := backend.ResolveAvailable(
		request.requestedSelection,
		sessions.ModelSelection(request.resumedSession),
		configuredModels,
		location.GetModelRoundRobinPath(),
		func(candidate model.Selection) bool {
			if model.IsGoQuotaExempt(request.modelChoices, candidate) {
				return true
			}
			return usage.IsSelectionAvailable(
				location.GetUsageCachePath(candidate.Provider, request.isEndpointOverridden),
				candidate.Model,
				selectionTime,
			)
		},
	)
	if err != nil {
		startup.Wait(func() {
			selection, err = model.ChooseWhenNoneSelected(
				err,
				request.modelCachePath,
				request.currency,
				request.isProviderAvailable,
				request.keyboard,
				os.Stdout,
				settings.Defaults.ForSelections(),
			)
		})
	}
	if err != nil {
		return model.Selection{}, model.Choice{}, err
	}
	choice, err := sessions.ModelChoice(request.resumedSession, request.modelChoices, request.seenModelsPath, selection)
	if err != nil {
		return model.Selection{}, model.Choice{}, err
	}
	return selection, choice, nil
}

func listedIgnoredChoices(isShowingIgnored bool, modelCachePath string, seenModelsPath string) ([]model.Choice, error) {
	if !isShowingIgnored {
		return nil, nil
	}

	return model.IgnoredChoices(modelCachePath, seenModelsPath)
}

func applyDefaultCaps(options *cli.Options, settings config.Config) error {
	if options.WereCapsChosen {
		return nil
	}

	defaultFlags := string(settings.Defaults.Caps) + settings.DefaultToolGroupFlags(options.Tools)
	grantedCaps, grantedGroups, err := settings.ParseCaps(defaultFlags)
	if err != nil {
		return err
	}
	options.Caps = grantedCaps
	options.GroupFlags = grantedGroups
	return nil
}

type preparedTools struct {
	registeredTools         []tool.Tool
	offeredTools            []tool.Tool
	compatibleNames         []string
	availabilityRestoration toolset.AvailabilityRestoration
}

func prepareFrozenTools(
	definitions []store.ToolDefinition,
	history []agent.Event,
	toolboxTools []tool.Tool,
	withheldTools []string,
) (preparedTools, error) {
	restoredTools := toolset.Restore(toolboxTools, store.RestoreTools(definitions), withheldTools)
	availabilityRestoration, err := toolset.RestoreAvailability(
		history,
		restoredTools.Availability,
		restoredTools.VersionChanges,
	)
	if err != nil {
		return preparedTools{}, err
	}
	return preparedTools{
		registeredTools:         restoredTools.RegisteredTools,
		offeredTools:            restoredTools.OfferedTools,
		compatibleNames:         restoredTools.CompatibleNames,
		availabilityRestoration: availabilityRestoration,
	}, nil
}

func prepareLegacyTools(
	resumedSession *store.Session,
	toolboxTools []tool.Tool,
	requestedNames []string,
	notices io.Writer,
) (preparedTools, error) {
	enabledNames := requestedNames
	if resumedSession != nil && len(resumedSession.Meta.Tools) > 0 {
		var absentNames []string
		enabledNames, absentNames = toolset.Partition(toolboxTools, resumedSession.Meta.Tools)
		if len(absentNames) > 0 {
			_, _ = fmt.Fprintln(notices, style.Change(
				"tools used by this conversation are no longer offered, so its prompt cache will be "+
					"rebuilt: "+strings.Join(absentNames, ", "),
			))
		}
	}
	if resumedSession == nil && len(enabledNames) > 0 && slices.ContainsFunc(toolboxTools, func(subject tool.Tool) bool {
		return subject.Name() == task.Name
	}) && slices.ContainsFunc(toolboxTools, func(subject tool.Tool) bool {
		return slices.Contains(enabledNames, subject.Name()) && subject.Schema().Find("background") != nil
	}) {
		enabledNames = append(slices.Clone(enabledNames), task.Name)
	}
	offeredTools, err := toolset.Reduce(toolboxTools, enabledNames)
	if err != nil {
		return preparedTools{}, err
	}
	return preparedTools{registeredTools: toolboxTools, offeredTools: offeredTools}, nil
}

func applyObservedTheme(current config.Config, latest config.Config) {
	if reflect.DeepEqual(current.Ui.Theme, latest.Ui.Theme) {
		return
	}

	style.ApplyTheme(latest.Ui.Theme)
}

func availableCurrency(initial initialConfig) money.Currency {
	if initial.err != nil {
		return money.Dollar()
	}

	return money.Load(location.GetExchangeRateCachePath(), currencyCode(initial.observable.Config.Ui.Currency))
}

func currencyCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func ensureCurrency(ctx context.Context, output io.Writer, code string, isSimulated bool) money.Currency {
	code = currencyCode(code)
	if code == "" || code == money.DollarCode || isSimulated {
		return money.Dollar()
	}

	currency, err := money.Ensure(ctx, "", location.GetExchangeRateCachePath(), code)
	if err != nil {
		_, _ = fmt.Fprintln(output, style.Change("exchange rate not refreshed: %s", err))
	}

	return currency
}

func sandboxPathsForMode(paths shell.Paths, isYolo bool) shell.Paths {
	if isYolo {
		paths.Deny = nil
	}
	return paths
}

func selectedGroupToolNames(configuredNames []string, offeredNames []string, groups caps.ToolGroups, isResumed bool) []string {
	selectedNames := slices.DeleteFunc(slices.Clone(configuredNames), func(name string) bool {
		return !toolset.Offers(offeredNames, name)
	})
	if isResumed {
		for _, names := range groups {
			for _, name := range names {
				if toolset.Offers(offeredNames, name) && !slices.Contains(selectedNames, name) {
					selectedNames = append(selectedNames, name)
				}
			}
		}
	}
	return selectedNames
}

func taskStartsAvailable(settings config.Config, selectedTools []string) bool {
	names := append([]string{"bash", "grep", "lookup"}, settings.CustomToolNames()...)
	return slices.ContainsFunc(names, func(name string) bool {
		return toolset.Offers(selectedTools, name) && settings.IsToolBackgroundable(name)
	})
}

type taskAccess struct {
	ToolName      string
	Group         string
	Policy        sandbox.Policy
	SearchPath    string
	IsHostNetwork bool
}

func taskPolicyFor(
	toolName string,
	group string,
	workspaceDir string,
	roots shell.Roots,
	pathAccess *shell.PathAccess,
	mode *caps.Mode,
	isYolo bool,
) func(context.Context, string) (any, error) {
	var shellPolicy func(context.Context) (sandbox.Policy, error)
	if toolName == "bash" {
		shellPolicy = shell.TaskPolicy(workspaceDir, roots, pathAccess, mode, isYolo)
	}
	return func(ctx context.Context, arguments string) (any, error) {
		access := taskAccess{ToolName: toolName, Group: group}
		if shellPolicy != nil {
			var err error
			access.Policy, err = shellPolicy(ctx)
			if err != nil {
				return nil, err
			}
			var call struct {
				Network string `json:"network"`
			}
			if json.Unmarshal([]byte(arguments), &call) == nil {
				access.IsHostNetwork = call.Network == "host"
			}
		}
		if toolName == "grep" {
			var call struct {
				Path string `json:"path"`
			}
			if json.Unmarshal([]byte(arguments), &call) == nil {
				searchPath := call.Path
				if searchPath == "" {
					searchPath = workspaceDir
				} else if !filepath.IsAbs(searchPath) {
					searchPath = filepath.Join(workspaceDir, searchPath)
				}
				access.SearchPath, _ = filepath.EvalSymlinks(searchPath)
			}
		}
		return access, nil
	}
}

func (self taskAccess) ForExecution(ctx context.Context) context.Context {
	if self.ToolName == "bash" {
		return shell.BindTaskPolicy(ctx, self.Policy)
	}
	return ctx
}

func applySimulationOptions(options *cli.Options) {
	options.Yolo = true
	options.Tools = demo.Tools()
}

//nolint:gocyclo // lol no
func run(hooks *cycle.Hooks, requestedTransition *cycle.Transition, initial initialConfig) (string, error) {
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
	modelCachePath := location.GetModelCachePath()
	seenModelsPath := location.GetSeenModelsPath()
	sessionsDir := location.GetSessionsDir()

	if inputArgs.Version {
		fmt.Println(cli.Version())
		return "", nil
	}

	if style.IsColourEnabled() {
		if terminalBackground, isKnown := background.Detect(keyboard, os.Stdout); isKnown {
			style.ReportBackground(terminalBackground)
		}
	}

	if inputArgs.Login {
		err := onboarding.Login(inputArgs.Provider, keyboard, os.Stdout)
		if errors.Is(err, onboarding.ErrCancelled) {
			return "", nil
		}
		return "", err
	}

	if endpointURL == "" && !inputArgs.IsDemoing {
		if err := backend.RefuseOutdatedLogins(); err != nil {
			return "", err
		}
	}

	if inputArgs.Usage {
		return "", usage.Show(ctx, os.Stdout, usage.Options{JSON: inputArgs.JSON})
	}

	if inputArgs.List {
		if initial.err != nil {
			return "", initial.err
		}
		settings := initial.observable.Config
		columns := 0
		if tty.Is(os.Stdout) {
			columns, _, _ = term.GetSize(int(os.Stdout.Fd()))
		}
		endpoints := backend.EndpointSettings{OverrideURL: endpointURL, OllamaHost: settings.Provider.Ollama.Host}
		ignoredChoices, err := listedIgnoredChoices(inputArgs.IsShowingIgnored, modelCachePath, seenModelsPath)
		if err != nil {
			return "", err
		}
		return "", model.List(
			os.Stdout, modelCachePath, ignoredChoices,
			money.Load(location.GetExchangeRateCachePath(), currencyCode(settings.Ui.Currency)),
			func(providerName string) bool { return backend.IsAvailable(providerName, endpoints) },
			settings.Defaults.ForSelections(), columns,
		)
	}

	workspace, err := work.Current()
	if err != nil {
		return "", err
	}

	workspaceDir := workspace.GetDir()

	if inputArgs.IsSessionPicker {
		return sessions.Choose(sessionsDir, workspace, availableCurrency(initial), keyboard, os.Stdout)
	}

	configPath := location.GetConfigFile()
	isSimulated := inputArgs.IsDemoing
	if initial.err != nil {
		return "", initial.err
	}

	if !isSimulated {
		isChosen, err := onboarding.PrepareConfig(onboarding.Options{
			Input:          keyboard,
			Output:         os.Stdout,
			EndpointURL:    endpointURL,
			RequestedModel: inputArgs.Model,
			ResumedSession: inputArgs.Session,
			Settings:       initial.observable.Config,
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
		modelCachePath = location.GetModelCachePath()
		seenModelsPath = location.GetSeenModelsPath()
		sessionsDir = location.GetSessionsDir()
	}

	settings, configObserver, err := initial.observable.Observe()
	if err != nil {
		return "", err
	}
	defer configObserver.Close()

	applyObservedTheme(initial.observable.Config, settings)
	editorConfiguration := editor.NewConfiguration(settings.Editor.Command)
	toolOutputLimit := truncate.NewLimit(settings.Defaults.ToolOutput.Bytes)
	experimentalToggles := experimental.New(settings.Experimental)

	endpoints := backend.EndpointSettings{
		OverrideURL:    endpointURL,
		OllamaHost:     settings.Provider.Ollama.Host,
		StreamTimeouts: streamTimeoutsFrom(experimentalToggles),
	}
	listProviderModels := func(ctx context.Context, providerName string) ([]agent.Model, error) {
		return backend.ListModels(ctx, providerName, endpoints)
	}
	isProviderAvailable := func(providerName string) bool {
		return backend.IsAvailable(providerName, endpoints)
	}

	if inputArgs.Update {
		return "", model.Update(os.Stdout, endpointURL, modelCachePath, seenModelsPath, listProviderModels, inputArgs.IsShowingIgnored)
	}

	modelChoices, err := model.Ensure(
		notices,
		endpointURL,
		modelCachePath,
		seenModelsPath,
		listProviderModels,
		isProviderAvailable,
	)
	if err != nil {
		return "", err
	}

	currency := ensureCurrency(ctx, notices, settings.Ui.Currency, isSimulated)

	if inputArgs.IsModelPicker {
		var chosenModel model.Selection
		var err error
		startup.Wait(func() {
			chosenModel, err = model.Choose(
				modelCachePath, currency, isProviderAvailable, keyboard, os.Stdout, settings.Defaults.ForSelections(),
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

	configuredToolGroups, err := settings.ToolGroups()
	if err != nil {
		return "", err
	}
	args, err := inputArgs.Parse(
		modelChoices,
		seenModelsPath,
		settings.Defaults.ForSelections(),
		configuredToolGroups.CustomFlags(),
	)
	if err != nil {
		return "", err
	}
	if err := applyDefaultCaps(&args, settings); err != nil {
		return "", err
	}

	if isSimulated {
		applySimulationOptions(&args)
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
	args.Yolo = sessions.ForkedConfinement(args.Yolo, forkSource)
	args.Caps, args.GroupFlags = sessions.ForkedCaps(
		args.Caps, args.GroupFlags, args.WereCapsChosen, configuredToolGroups.CustomFlags(), forkSource,
	)

	resumedSession, err := sessions.LoadForResume(sessionsDir, workspace, args.Session, args.IsForced)
	if err != nil {
		return "", err
	}

	args.Yolo, err = sessions.OpeningConfinement(args.Yolo, resumedSession)
	if err != nil {
		return "", err
	}

	args.Caps, err = sessions.OpeningCaps(args.Caps, args.WereCapsChosen, resumedSession, args.Yolo)
	if err != nil {
		return "", err
	}

	args.GroupFlags, err = sessions.OpeningToolGroups(args.GroupFlags, args.WereCapsChosen, resumedSession)
	if err != nil {
		return "", err
	}
	frozenToolGroups := configuredToolGroups
	if resumedSession != nil {
		_, recordedToolGroups, _ := caps.LastRecordedToolGroups(resumedSession.Events)
		frozenToolGroups = recordedToolGroups
	}
	enabledToolNamesForGroups := args.Tools
	if resumedSession != nil && len(resumedSession.Meta.Tools) > 0 {
		enabledToolNamesForGroups = resumedSession.Meta.Tools
	}
	if resumedSession == nil && len(enabledToolNamesForGroups) > 0 && taskStartsAvailable(settings, enabledToolNamesForGroups) {
		enabledToolNamesForGroups = append(slices.Clone(enabledToolNamesForGroups), task.Name)
	}
	selectedToolGroupNames := selectedGroupToolNames(
		settings.ToolGroupNames(), enabledToolNamesForGroups, frozenToolGroups, resumedSession != nil,
	)
	if resumedSession == nil {
		frozenToolGroups = frozenToolGroups.Only(selectedToolGroupNames)
	}
	activeToolGroups := frozenToolGroups.Only(selectedToolGroupNames)

	settings.Grants = sandboxPathsForMode(settings.Grants, args.Yolo)
	if err := startup.CheckDenyWorkspace(ctx, workspaceDir, settings.Grants.Deny, args.Caps.Has(caps.Shell), args.IsForced); err != nil {
		return "", err
	}

	if err := workspace.Validate(); err != nil {
		return "", err
	}

	if err := workspace.Open(); err != nil {
		return "", err
	}

	defer func() { _ = workspace.Close() }()

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

	selection, choice, err := chooseSessionModel(sessionModelRequest{
		requestedSelection:   args.Selection,
		resumedSession:       resumedSession,
		settings:             settings,
		modelChoices:         modelChoices,
		seenModelsPath:       seenModelsPath,
		modelCachePath:       modelCachePath,
		currency:             currency,
		isProviderAvailable:  isProviderAvailable,
		keyboard:             keyboard,
		isSimulated:          isSimulated,
		isEndpointOverridden: endpointURL != "",
	})
	if errors.Is(err, menu.ErrCancelled) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	childChoices := slices.DeleteFunc(slices.Clone(modelChoices), func(candidate model.Choice) bool {
		return !isProviderAvailable(candidate.Provider)
	})

	if resumedSession == nil && !args.WereCapsChosen &&
		!canStartChildren(choice, settings, childChoices) {
		args.Caps &^= caps.Subagents
	}

	mode := caps.NewModeWithGroups(args.Caps, args.GroupFlags, frozenToolGroups, activeToolGroups)

	settings.Grants, err = shell.PreparePathsForMode(settings.Grants, os.Stderr, args.Yolo)
	if err != nil {
		return "", err
	}

	builtinSkillDir := location.GetStateDir(skill.DirectoryName)
	if err := skill.Materialise(builtinSkillDir); err != nil {
		style.WriteWarningf(os.Stderr, "%v", err)
		builtinSkillDir = ""
	}

	globalSkillDirs := append(
		[]string{builtinSkillDir, location.GetConfigDir(skill.DirectoryName)},
		settings.Skills.Include...,
	)
	availableSkills, err := discoverSkills(workspace.GetDir(), globalSkillDirs, settings.Skills.Exclude, os.Stderr)
	if err != nil {
		return "", err
	}

	client, err := backend.Connect(choice, selection, endpoints)
	if err != nil {
		return "", err
	}

	meta := store.Meta{
		Model:          selection.Model,
		WorkspaceDir:   workspaceDir,
		Provider:       selection.Provider,
		Effort:         selection.Effort,
		IsFast:         selection.IsFast,
		ModelChoice:    &choice,
		Yolo:           args.Yolo,
		ShouldSkipWire: !settings.Debug.ShouldRecordWire,
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
	if settings.Debug.ShouldProfileCPU {
		profiler, err := diagnostics.Profile(sessionInfo.Directory)
		if err != nil {
			_, _ = fmt.Fprintln(notices, style.Change("the processor profile could not start: "+err.Error()))
		} else {
			defer profiler.Close()
		}
	}
	if settings.Debug.ShouldProfileHeap {
		defer diagnostics.ProfileHeap(sessionInfo.Directory).Close()
	}
	hooks.EmitSessionStarting(ctx, cycle.SessionStarting{Session: sessionInfo})
	client.ObserveHTTP(log.Observer())

	tmpDir, err := sessions.PrepareTemporaryDirectory(log.Name())
	if err != nil {
		return "", err
	}

	var state *agentState
	defer func() {
		if state != nil {
			state.Close()
		}
		if !log.IsPersisted() {
			_ = diskutil.RemoveAll(tmpDir)
		}
	}()
	roots, err := shell.OpenRoots(homeDir, tmpDir)
	if err != nil {
		return "", err
	}
	state, err = buildAgentState(agentOptions{
		workspace:        workspace,
		mode:             mode,
		paths:            settings.Grants,
		isUnconfined:     args.Yolo,
		roots:            roots,
		releaseRoots:     func() { _ = roots.Close() },
		refuseHomeWrite:  shell.RefuseHomeWrite(mode),
		skills:           availableSkills,
		sessionDirectory: sessionInfo.Directory,
		ensurePersisted:  log.EnsurePersisted,
		outputLimit:      toolOutputLimit,
	})
	if err != nil {
		return "", err
	}
	files, pathAccess, dropKeeper := state.files, state.access, state.drops

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

	sandboxRunner, keeperProcess, closeKeeper, keeperRefusal := openRunner(ctx, args.Yolo)
	defer closeKeeper()

	if keeperRefusal != nil {
		_, _ = fmt.Fprintln(notices, style.Change(
			"inter-command networking is unavailable: "+keeperRefusal.Error(),
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

	forwardsAddress := portgrant.AddressFor(log.Name())
	forwardsHostname := settings.GetHostname(log.Name(), forwardsAddress)
	wireLogPath := ""
	if log.RecordsWire() {
		wireLogPath = "debug/wire.http.zst"
	}
	currentEnvironment := environment.Capture(
		settings.Grants,
		availableSkills,
		pathutil.Exists(filepath.Join(workspace.GetDir(), ".git")),
		forwardsHostname,
		wireLogPath,
	)
	var createdEnvironment *environment.Snapshot
	if resumedSession != nil {
		createdEnvironment = resumedSession.Meta.Environment
	}
	restoredEnvironment, err := environment.Restore(createdEnvironment, recordedEvents, currentEnvironment)
	if err != nil {
		return "", err
	}
	if resumedSession != nil {
		restoredEnvironment, err = restoreLegacyWireLocation(restoredEnvironment, currentEnvironment, session.Dir(sessionsDir, log.Name()))
		if err != nil {
			return "", err
		}
	}

	var systemPrompt string
	var fixedContextSources staticContextSources
	var contextFiles []prompt.File
	if resumedSession != nil && resumedSession.Meta.SystemPrompt != "" {
		systemPrompt = resumedSession.Meta.SystemPrompt
		fixedContextSources = staticContextSources{
			systemFiles:    slices.Clone(resumedSession.Meta.SystemContextFiles),
			projectFiles:   slices.Clone(resumedSession.Meta.ProjectContextFiles),
			systemSources:  slices.Clone(resumedSession.Meta.SystemContextSources),
			sessionSources: slices.Clone(resumedSession.Meta.SessionContextSources),
		}
	} else {
		systemPrompt, contextFiles, err = prompt.Load(prompt.Config{
			GlobalPath:             location.GetGlobalContextPath(),
			Workspace:              workspace,
			SessionName:            log.Name(),
			SessionsDir:            sessionsDir,
			SessionDir:             filepath.Join(sessionsDir, log.Name()),
			ConfigFile:             location.GetConfigFile(),
			TmpDir:                 tmpDir,
			HomeDir:                homeDir,
			CurrentCaps:            args.Caps,
			ExtraPaths:             settings.Grants,
			DropsDirectory:         dropKeeper.GetDirectory(),
			Skills:                 availableSkills,
			OfferedTools:           args.Tools,
			Conditions:             currentConditions,
			TasksGranted:           canSuperviseTasks(args.IsPrinting, keyboard) && taskStartsAvailable(settings, args.Tools),
			BackgroundShellGranted: canSuperviseTasks(args.IsPrinting, keyboard),
			NetworkGranted:         args.Caps.Has(caps.Network),
			ToolGroups:             activeToolGroups,
			GroupStatus:            mode.Groups(),
			Yolo:                   args.Yolo,
			Notes:                  agent.NoteFormatOf(client.Client),
		})
		if err != nil {
			return "", err
		}
	}
	systemPrompt = prompt.WithDropsDirectory(systemPrompt, dropKeeper.GetDirectory())
	if resumedSession == nil {
		fixedContextSources = identifyStaticContextSources(systemPrompt, contextFiles, availableSkills)
	}

	pathGrants := pathgrant.New(workspace, pathAccess)
	var pathGrantRestoreResult pathgrant.RestoreResult
	if resumedSession != nil {
		if recordedGrants, found := pathgrant.LastRecorded(resumedSession.Events); found {
			pathGrants, pathGrantRestoreResult = pathgrant.NewRestored(workspace, pathAccess, recordedGrants)
		}
	}

	forwarder := newForwarder(ctx, keeperProcess, forwardsAddress)
	forwards := portgrant.NewForwards(forwarder, forwardsHostname)
	var forwardsRestoreResult portgrant.ForwardsRestoreResult
	if resumedSession != nil {
		if recordedPorts, found := portgrant.LastRecordedForwards(resumedSession.Events); found {
			forwards, forwardsRestoreResult = portgrant.NewRestoredForwards(
				forwarder, forwardsHostname, recordedPorts,
			)
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

	preparedChildren, err := prepareChildManager(childOptions{
		parentChoice:       choice,
		endpoints:          endpoints,
		workspace:          workspace,
		settings:           settings,
		sessionName:        log.Name(),
		sessionsDir:        sessionsDir,
		scratchParent:      tmpDir,
		runner:             sandboxRunner,
		isParentUnconfined: args.Yolo,
		recordsWire:        log.RecordsWire(),
		ensurePersisted:    log.EnsurePersisted,
		parentFiles:        files,
		parentCaps:         mode.Current,
		userHome:           userHome(),
		toolOutputLimit:    toolOutputLimit,
		skillDirectories:   globalSkillDirs,
		parentWritable: func() []string {
			return slices.Concat([]string{workspace.GetDir(), homeDir, tmpDir}, pathAccess.GetPaths().Write)
		},
	}, childChoices, args.IsPrinting)
	if err != nil {
		return "", err
	}
	childManager := preparedChildren.manager
	if childManager != nil {
		defer childManager.Close()
	}

	var toolsBeforeShell []tool.Tool
	if childManager != nil {
		toolsBeforeShell = append(toolsBeforeShell, subagent.New(childManager, func() bool { return mode.Current().Has(caps.Subagents) }))
	}
	askBroker := ask.New()
	permissionSet, err := settings.BuildPermissions()
	if err != nil {
		return "", err
	}
	permissions := permission.New(permissionSet)
	approveNetwork := func(ctx context.Context, command string, intent string) error {
		return approveHostNetwork(ctx, askBroker, permissions.Network(), command, intent)
	}
	shellTool := state.shell(approveNetwork, sandboxRunner)

	doesWake := !args.IsPrinting
	var taskManager *tasks.Manager
	if canSuperviseTasks(args.IsPrinting, keyboard) {
		maximumRunningTasks := experimentalToggles.GetWholeNumber(experimental.MaximumRunningTasks, tasks.DefaultMaximumRunning)
		taskManager = tasks.NewWithMaximumRunning(ctx, maximumRunningTasks)
		defer taskManager.Close()
		configureTaskManager(taskManager, experimentalToggles)
		toolsBeforeShell = append(toolsBeforeShell, task.New(taskManager))
		forwards.OnRevoke(func(route portgrant.Route) {
			if route.TaskName != "" {
				taskManager.ForgetPort(route.TaskName, route.Port)
			}
		})
	}

	var toolsAfterShell []tool.Tool
	if keeperProcess != nil {
		toolsAfterShell = append(toolsAfterShell, forward.New(forwards.ForModel()))
	}
	toolsAfterShell = append(toolsAfterShell, title.New())
	toolsAfterShell = append(
		toolsAfterShell,
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
		Workspace:      workspace.GetDir(),
		Scratch:        tmpDir,
		Session:        log.Name(),
		GroupForTool:   frozenToolGroups.GroupOf,
		IsGroupAllowed: mode.Allows,
		Approve: func(
			ctx context.Context,
			name string,
			subject string,
			arguments tool.Arguments,
			timeout time.Duration,
		) error {
			return customToolApproval(name).confirmArguments(ctx, askBroker, subject, arguments, timeout)
		},
	})
	if err != nil {
		return "", err
	}
	toolboxTools, err := state.tools(shellTool, agentFeatures{
		beforeShell: toolsBeforeShell,
		afterShell:  toolsAfterShell,
		custom:      customTools,
	})
	if err != nil {
		return "", err
	}
	toolboxTools, err = backgroundTools(toolboxTools, backgroundOptions{
		settings:         settings,
		manager:          taskManager,
		resumedSession:   resumedSession,
		isPrinting:       args.IsPrinting,
		isYolo:           args.Yolo,
		isForwardOffered: keeperProcess != nil && isOffered(resumedSession, args.Tools, forward.Name),
		policyFor: func(name string) func(context.Context, string) (any, error) {
			return taskPolicyFor(name, frozenToolGroups.GroupOf(name), workspace.GetDir(), roots, pathAccess, mode, args.Yolo)
		},
	})
	if err != nil {
		return "", err
	}
	var withheldTools []string
	if args.IsPrinting {
		withheldTools = append(toolset.WithheldFromHeadless(toolboxTools), subagent.Name, task.Name)
		toolboxTools = toolset.Headless(toolboxTools)
	}

	var preparedToolbox preparedTools
	hasFrozenDefinitions := resumedSession != nil && len(resumedSession.Meta.ToolDefinitions) > 0
	if hasFrozenDefinitions {
		preparedToolbox, err = prepareFrozenTools(
			resumedSession.Meta.ToolDefinitions, resumedSession.Events, toolboxTools, withheldTools,
		)
	} else {
		preparedToolbox, err = prepareLegacyTools(resumedSession, toolboxTools, args.Tools, notices)
	}
	if err != nil {
		return "", err
	}
	if hasFrozenDefinitions {
		mode.RestrictTools(preparedToolbox.compatibleNames)
	}
	registeredTools := preparedToolbox.registeredTools
	offeredTools := preparedToolbox.offeredTools
	availabilityRestoration := preparedToolbox.availabilityRestoration
	for _, offeredTool := range offeredTools {
		for _, current := range toolboxTools {
			if current.Name() == offeredTool.Name() && tool.IsCompatible(current, tool.TakeSnapshot(offeredTool)) {
				async.Bind(current, offeredTool.Schema())
			}
		}
	}

	if resumedSession == nil {
		meta.SystemPrompt = systemPrompt
		meta.SystemContextFiles = slices.Clone(fixedContextSources.systemFiles)
		meta.ProjectContextFiles = slices.Clone(fixedContextSources.projectFiles)
		meta.SystemContextSources = slices.Clone(fixedContextSources.systemSources)
		meta.SessionContextSources = slices.Clone(fixedContextSources.sessionSources)
		meta.Tools = toolset.Names(offeredTools)
		meta.ToolDefinitions = store.FreezeTools(offeredTools)
		meta.Conditions = &currentConditions
		meta.Environment = &currentEnvironment
		if err := log.SetMeta(meta); err != nil {
			return "", err
		}
	}

	toolContextBytes := client.ToolsSize(offeredTools)
	var toolContextSource *contextsource.Source
	if toolContextBytes > 0 {
		source := contextsource.CountedFromBytes(
			contextsource.ToolDefinitions,
			len(offeredTools),
			toolContextBytes,
		)
		toolContextSource = &source
	}

	if forkSource != nil {
		transcriptPath, err := forkSource.CopyChat(dropKeeper)
		if err != nil {
			return "", fmt.Errorf("copy the forked session chat: %w", err)
		}
		args.Message = forkSource.GetMessageWithChatAt(args.Message, transcriptPath)
	}

	isYoloInherited := cli.IsYoloInherited(os.Args[1:])
	starter := sessionStarter{
		toolboxTools:      toolboxTools,
		getCustomCapFlags: func() string { return currentCustomCapFlags(getConfigSources(workspaceDir)) },
		isYoloInherited:   isYoloInherited,
		getModelChoices:   func() []model.Choice { return model.Choices(modelCachePath) },
		defaults:          sessionDefaults(selection),
	}
	var sessionNames []string
	var sessionNamesListedAt time.Time
	getSessionNames := func() []string {
		if sessionNames == nil || time.Since(sessionNamesListedAt) > sessionNameStaleAfter {
			names, err := sessions.NamesInWorkspace(sessionsDir, workspace)
			if err != nil {
				return sessionNames
			}
			sessionNames = names
			sessionNamesListedAt = time.Now()
		}
		return sessionNames
	}
	systemCommands, err := commands.New(commands.Options{
		ConfigDir:        location.GetConfigDir(),
		ConfigFile:       configPath,
		SystemPromptFile: location.GetGlobalContextPath(),
		SkillDirs:        globalSkillDirs,
		Workspace:        workspace,
		ScratchDir:       tmpDir,
		HomeDir:          homeDir,
		OpenEditor: func(paths []string) error {
			return app.openEditor(paths)
		},
		ReplyInEditor: func(text string) error {
			return app.replyInEditor(text)
		},
		Output: os.Stdout,
		PathGrants: commands.PathGrants{
			DenyPatterns: settings.Grants.Deny,
			Grant:        pathGrants.Grant,
			Revoke: func(path string) (agent.Event, error) {
				event, err := pathGrants.Revoke(path)
				if err != nil {
					return event, err
				}
				app.stopTasksHoldingPath(event.Name)

				return event, nil
			},
			GetCurrent: pathGrants.GetCurrent,
			GetPermanent: func() []shell.ScopedPathGrant {
				grants := shell.PermanentGrants(
					workspace.GetDir(), homeDir, settings.Grants, mode.Current(), args.Yolo,
				)
				grants = append(grants, shell.SkillGrants(
					skill.EnabledDirectories(availableSkills),
					skill.GlobalDirectories(availableSkills),
					mode.Current(),
				)...)
				if dropKeeper.IsMounted() {
					grants = append(grants, shell.ScopedPathGrant{
						Path: dropKeeper.GetDirectory(), Access: shell.ReadAccess, Kind: shell.SessionDropsGrant,
					})
				}
				return grants
			},
			GetCurrentCaps: mode.Current,
		},
		Forwards: commands.Forwards{
			Forward:    forwards.Forward,
			Revoke:     forwards.Revoke,
			GetCurrent: forwards.GetCurrent,
			GetURL:     forwards.URL,
		},
		Tasks: managedTasks(ctx, taskManager, func(snapshot tasks.Snapshot) string { return app.describeTaskCall(snapshot) }),
		Subagents: commands.Subagents{
			List:     func() []commands.SubagentListing { return app.listSubagents() },
			Progress: func() []commands.SubagentProgress { return app.subagentProgress() },
			Show:     func(name string) error { return app.showSubagent(name) },
		},
		StartHostCommand: func(directory string, command string) error {
			return app.startHostCommand(directory, command)
		},
		GetInfo: func() (string, error) {
			return app.display.bar.RenderInfo(segment.Context{})
		},
		GetContextSources: func() commands.ContextSources {
			var events []agent.Event
			if app != nil {
				events = app.recordedEvents
			}
			return currentContextSources(fixedContextSources, toolContextSource, events)
		},
		Session: commands.Session{
			Name:           log.Name(),
			ID:             log.ID(),
			Directory:      filepath.Join(sessionsDir, log.Name()),
			IsPersisted:    log.IsPersisted,
			GetLastMessage: func() (string, bool) { return app.getLastMessage() },
		},
		GetModelChoices:   func() []model.Choice { return model.SignedInChoices(modelCachePath, isProviderAvailable) },
		GetSessionNames:   getSessionNames,
		GetToolNames:      func() []string { return toolset.Names(toolboxTools) },
		GetCustomCapFlags: starter.getCustomCapFlags,
		IsYoloInherited:   isYoloInherited,
		StartSession: func(start commands.SessionStart) error {
			transition, err := starter.transition(start)
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
	providerClient := guardUsage(ctx, client.Client, choice, selection, usageCachePath)

	draftStash, err := stash.Open(sessionInfo.Directory, log.EnsurePersisted)
	if err != nil {
		return "", err
	}

	app = &App{
		agent:    agent.NewWithEnabledTools(systemPrompt, providerClient, registeredTools, offeredTools),
		screen:   screen,
		terminal: terminal.New(os.Stdout, workspace),
		metrics: metrics.New(metrics.Settings{
			ContextWindowTokens: choice.ContextWindowTokens,
			Prices:              choice.Prices,
		}),
		recorder: record.New(log),
		children: childState{
			manager:   childManager,
			rotate:    preparedChildren.rotate,
			directory: session.ChildrenDir(sessionsDir, log.Name()),
		},
		editorConfig:     editorConfiguration,
		draftStash:       draftStash,
		toolOutputLimit:  toolOutputLimit,
		saveOverflow:     dropKeeper.SaveOutput,
		experimental:     experimentalToggles,
		workspace:        workspace,
		mode:             mode,
		conditions:       restoredConditions.State,
		environment:      restoredEnvironment.State,
		pathGrants:       pathGrants,
		forwards:         forwards,
		tasks:            taskManager,
		sessionDirectory: sessionInfo.Directory,
		tasksDoWake:      doesWake,
		configObserver:   configObserver,
		runMode:          runMode{isPrinting: args.IsPrinting, isYolo: args.Yolo, isSimulated: isSimulated},
		question:         questionState{broker: askBroker},
		completer: trigger.New(
			pathref.NewSource(pathref.NewIndex(workspace.GetDir(), files.ExcludedNames)),
			slash.NewSource(func() slash.Registry { return app.commands }),
			slash.NewPathSource(workspace.GetDir(), func() slash.Registry { return app.commands }),
		),
		startedAt: util.WallClock(time.Now()),
		keyboard:  keyboard,
	}
	if resumedSession == nil && model.SupportsFastMode(selection.Provider) {
		app.openingEvents = append(app.openingEvents, model.FastModeEvent(selection.IsFast))
	}
	if resumedSession != nil && restoredConditions.NeedsBaseline {
		app.openingEvents = append(app.openingEvents, restoredConditions.Change)
	}
	if resumedSession != nil && restoredEnvironment.NeedsBaseline {
		app.openingEvents = append(app.openingEvents, restoredEnvironment.Change)
	}
	if restoredConditions.IsChanged {
		app.pendingNotices.add(restoredConditions.Change)
	}
	if restoredEnvironment.IsChanged {
		app.pendingNotices.add(restoredEnvironment.Change)
	}
	if availabilityRestoration.IsChanged {
		app.pendingNotices.add(availabilityRestoration.Change)
	}
	app.onFailure = func(failure error) {
		_ = notification.SendTurnError(
			context.Background(), screen.WriteEscape, isTerminalFocused, workspace, failure,
		)
	}
	questionNotifications := notification.NewQuestions(screen.WriteEscape, isTerminalFocused, workspace)
	app.onQuestion = questionNotifications.Announce
	app.savePastedImage = dropKeeper.SaveImage
	if settings.Debug.ShouldRecordStalls {
		stallWatchdog := diagnostics.Watch(sessionInfo.Directory)
		defer stallWatchdog.Close()
		app.watchStalls = stallWatchdog.Begin
	}

	cellWidth, cellHeight, hasGraphics := graphics.Detect(keyboard, os.Stdout)
	if hasGraphics {
		app.display.pictures = pictures.Display{
			SessionDirectory: sessionInfo.Directory,
			ScratchDirectory: shadowedScratch(tmpDir, args.Yolo),
			CellWidth:        cellWidth,
			CellHeight:       cellHeight,
			IsLocal:          isTerminalLocal(),
		}

		state.storePictures(app.agent)
	}

	usageReporter, _ := client.Client.(agent.UsageReporter)

	barRegistry := bar.NewRegistry(bar.Options{
		Workspace:             workspace,
		Session:               sessionInfo,
		ModelEffortLevels:     choice.EffortLevels,
		IsFast:                selection.IsFast,
		IsSimulated:           isSimulated,
		IsUnconfined:          args.Yolo,
		UsageReporter:         usageReporter,
		UsageCachePath:        usageCachePath,
		UsagePage:             backend.UsagePage(selection.Provider, endpointURL),
		UsageIsSelfRefreshing: usageReporter != nil,
		UsageGauges:           usage.GaugesFor(os.Stdout, cellWidth, cellHeight, hasGraphics),
		GetCurrency: func() money.Currency {
			if app == nil {
				return currency
			}
			return app.getCurrency()
		},
		SandboxHostname: forwardsHostname,
		Sources:         app.getBarSources(),
	})
	liveConfig, err := settings.BuildLive(barRegistry)
	if err != nil {
		return "", err
	}
	commandRegistry, err := slash.NewRegistry(systemCommands, liveConfig.SnippetCommandSet)
	if err != nil {
		return "", err
	}
	app.commands = commandRegistry
	app.permissions = permissions
	app.notifyUnknownSettings(liveConfig.UnknownSettings)
	app.nudge = liveConfig.Nudge
	app.speedDial.Configure(liveConfig.SpeedDial)
	app.display.streamingMode = liveConfig.StreamingMode
	app.display.reasoningRendering = liveConfig.ReasoningRendering
	app.display.theme = liveConfig.Theme
	app.display.modelName = selection.Model
	app.display.tariff = painter.Tariff{Prices: choice.Prices, Currency: currency}
	app.currency = newCurrencyState(settings.Ui.Currency, currency, isSimulated)
	screen.SetGrouping(liveConfig.Grouping)
	app.display.bar = bar.NewConfiguration(barRegistry, liveConfig.SegmentLayout)

	if resumedSession != nil {
		app.restore(resumedSession)
	}
	if args.Yolo {
		app.unconfine()
	}
	if forkSource != nil {
		app.carryOver(forkSource)
	}
	for _, failure := range forwardsRestoreResult.Failures {
		correction, err := portgrant.ForwardChangeEvent(
			forwardsAddress,
			failure.Port,
			forwards.GetRoutes(),
		)
		if err != nil {
			return "", err
		}
		app.pendingNotices.add(correction)
		app.notifyFailure(fmt.Sprintf(
			"Port %d could not be forwarded again: %v", failure.Port, failure.Err,
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
		ReadyAt:       app.getNow(),
		PromptBytes:   len(systemPrompt),
		ProjectSkills: projectSkills,
		GlobalSkills:  globalSkills,
		Snippets:      len(settings.Snippets),
		ToolBytes:     toolContextBytes,
		LocalConfig:   localConfig,
	}
	if resumedSession == nil {
		app.notify(startup.NewEvent(startupElapsed, startupInfo))
	}

	hasStarted = true
	hooks.EmitSessionStarted(ctx, cycle.SessionStarted{Session: sessionInfo})
	transition := app.begin(args.Message)
	if err := app.endTasks(); err != nil {
		_, _ = fmt.Fprintln(notices, style.Error(fmt.Errorf("the final task state could not be stored: %w", err)))
	}
	app.endChildren()
	questionNotifications.WithdrawAll(notificationWithdrawalGrace)
	*requestedTransition = transition
	stopReason = transition.StopReason()
	hooks.EmitSessionStopping(ctx, cycle.SessionStopping{Session: sessionInfo, Reason: stopReason})

	if isSessionLeftToResume(log.IsPersisted(), isSimulated, transition.Kind) {
		_, _ = fmt.Fprintf(notices, "\n%s\n", style.Subtle(sessions.ResumeCommand(os.Args[0], log.Name())))
	}
	if app.termination.receivedSignal != nil {
		return "", terminationSignalError{signal: app.termination.receivedSignal}
	}

	return "", nil
}

func restoreLegacyWireLocation(restoration environment.Restoration, current environment.Snapshot, sessionDirectory string) (environment.Restoration, error) {
	if !restoration.NeedsBaseline || current.WireLogPath == "" {
		return restoration, nil
	}
	if _, err := os.Stat(filepath.Join(sessionDirectory, current.WireLogPath)); errors.Is(err, os.ErrNotExist) {
		return restoration, nil
	} else if err != nil {
		return restoration, err
	}
	previous := current
	previous.WireLogPath = ""
	change, err := environment.ChangeEvent(previous, current)
	if err != nil {
		return restoration, err
	}
	restoration.Change = change
	restoration.IsChanged = true
	return restoration, nil
}

func isSessionLeftToResume(isPersisted bool, isSimulated bool, kind cycle.TransitionKind) bool {
	return isPersisted && !isSimulated && kind == cycle.Quit
}

func openRunner(
	ctx context.Context, isYolo bool,
) (sandbox.Runner, *keeper.Keeper, func(), error) {
	if isYolo {
		runner := sandbox.Direct()
		return runner, nil, func() {}, nil
	}

	keeperProcess, err := keeper.Open(ctx)
	if err != nil {
		return sandbox.Direct(), nil, func() {}, err
	}

	runner := sandbox.Wrapped(keeperProcess)

	return runner, keeperProcess, func() { _ = keeperProcess.Close() }, nil
}

func newForwarder(
	ctx context.Context, keeperProcess *keeper.Keeper, host string,
) portgrant.Forwarder {
	if keeperProcess == nil {
		return portgrant.Forwarder{}
	}

	return portgrant.Forwarder{
		Forward: func(port uint16) error { return keeperProcess.OpenForward(ctx, host, port) },
		Revoke:  keeperProcess.CloseForward,
	}
}

func managedTasks(ctx context.Context, manager *tasks.Manager, describeCall func(tasks.Snapshot) string) commands.Tasks {
	if manager == nil {
		return commands.Tasks{}
	}

	return commands.Tasks{
		List:   manager.List,
		Status: manager.Status,
		Output: manager.Output,
		Stop:   manager.Stop,
		Restart: func(name string) (tasks.Snapshot, error) {
			return manager.Restart(ctx, name)
		},
		Discard: func(name string) (tasks.Snapshot, error) {
			return manager.Discard(ctx, name)
		},
		PruneFinished: manager.PruneFinished,
		DescribeCall:  describeCall,
	}
}
