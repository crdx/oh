package onboarding

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"crdx.org/oh/pkg/agent"

	"crdx.org/oh/internal/app/backend"
	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/escape"
	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/menu"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/tty"
	"crdx.org/oh/internal/browser"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/pkg/provider/anthropic"
	"crdx.org/oh/pkg/provider/codex"
	"crdx.org/oh/pkg/provider/opencodego"
)

var ErrCancelled = menu.ErrCancelled

var ErrNobodyToAsk = errors.New(
	"there is no model to print with yet; choose one with -m, or run oh once without -p to set one up",
)

const (
	chatGPTName    = "ChatGPT"
	anthropicName  = "Anthropic"
	openCodeGoName = "OpenCode Go"
	simulatorName  = "Simulator"

	simulationIdentifier = "simulation"

	validationModel           = "validation"
	validationEffort          = "none"
	validationMaxOutputTokens = 1
)

const (
	stepMark    = "→"
	successMark = "✓"
	failureMark = "✗"
	noteGap     = "  "
	openingRule = "─"
)

const (
	typingInterval = 32 * time.Millisecond
	drowsyInterval = 110 * time.Millisecond
	ruleInterval   = 16 * time.Millisecond
	clauseRest     = 180 * time.Millisecond
	sentenceRest   = 360 * time.Millisecond
	wakingRest     = 600 * time.Millisecond
)

const (
	greeting         = "Oh, hello."
	greetingAside    = "*yawns*"
	introduction     = "Yes? Oh, right. Let's get you signed in."
	farewell         = "Thanks. Transferring you…"
	providerPrompt   = "Choose your provider:"
	modelPrompt      = "Choose a model:"
	openCodeGoPrompt = "OpenCode Go API key: "
	pasteHint        = "If the redirect breaks, paste the full redirect URL here instead."
	browserHint      = "Visit the URL above to continue."
	openingBrowser   = "Opening your browser to shake hands with %s…"
	signedIn         = "Signed in to %s"
	signInFailure    = "Unable to sign in: %s"
)

type provider struct {
	name       string
	identifier string
	note       string
}

var providers = []provider{
	{name: chatGPTName, identifier: model.CodexProvider, note: "OAuth"},
	{name: anthropicName, identifier: model.AnthropicProvider, note: "OAuth"},
	{name: openCodeGoName, identifier: model.OpencodeGoProvider, note: "Key"},
}

var simulation = provider{name: simulatorName, identifier: simulationIdentifier, note: "The doctor will see you now"}

type Options struct {
	Input          *os.File
	Output         io.Writer
	EndpointURL    string
	RequestedModel string
	ResumedSession string
	ConfigSources  []config.Source
	IsPrinting     bool
}

func PrepareConfig(options Options) (config.Config, bool, error) {
	configPath := location.GetConfigFile()
	configSources := options.ConfigSources
	if len(configSources) == 0 {
		configSources = []config.Source{{Path: configPath}}
	}
	settings, err := config.LoadSources(configSources...)
	if err != nil || !isRequired(options, settings.Model.RoundRobin) {
		return settings, false, err
	}

	if options.IsPrinting {
		return config.Config{}, false, ErrNobodyToAsk
	}

	modelCachePath := location.GetModelCachePath()
	seenModelsPath := location.GetSeenModelsPath()
	pause, stopPausing, err := typingPause(options.Input, options.Output)
	if err != nil {
		return settings, false, err
	}
	harry := wizard{
		isSimulationOffered: true,
		defaults:            settings.Model.GetDefaults(),

		output:      options.Output,
		pause:       pause,
		stopPausing: stopPausing,
		choose: func(prompt string, labels []string) (int, error) {
			return menu.ChooseIndex(options.Input, options.Output, prompt, labels)
		},
		login:       login(options.Input, options.Output),
		openBrowser: browser.Open,
		refreshModels: func() error {
			endpoints := backend.EndpointSettings{
				OverrideURL: options.EndpointURL,
				OllamaHost:  settings.Provider.Ollama.Host,
			}

			return model.Update(io.Discard, options.EndpointURL, modelCachePath, seenModelsPath,
				func(ctx context.Context, providerName string) ([]agent.Model, error) {
					return backend.ListModels(ctx, providerName, endpoints)
				}, false)
		},
		getModels: func() []model.Choice { return model.Choices(modelCachePath) },
		setInitialModel: func(selection string) error {
			_, err := setInitialModel(configPath, selection)
			return err
		},
	}

	if err := harry.castSpell(); err != nil {
		return config.Config{}, false, err
	}
	if harry.isSimulationChosen {
		return config.Config{}, true, nil
	}

	settings, err = config.LoadSources(configSources...)
	return settings, false, err
}

func isRequired(options Options, configuredModels []string) bool {
	return options.EndpointURL == "" && options.RequestedModel == "" && options.ResumedSession == "" &&
		len(configuredModels) == 0
}

func Login(providerName string, terminal *os.File, output io.Writer) error {
	harry := wizard{
		output: output,
		choose: func(prompt string, labels []string) (int, error) {
			return menu.ChooseIndex(terminal, output, prompt, labels)
		},
		login:       login(terminal, output),
		openBrowser: browser.Open,
	}

	_, err := harry.chooseProvider(providerName)
	return err
}

type wizard struct {
	output          io.Writer
	choose          func(string, []string) (int, error)
	login           func(provider, func(string)) error
	openBrowser     func(string) error
	pause           func(time.Duration) bool
	stopPausing     func()
	refreshModels   func() error
	getModels       func() []model.Choice
	setInitialModel func(string) error
	defaults        model.Defaults

	isOpeningSkipped    bool
	isSimulationOffered bool
	isSimulationChosen  bool
}

func (self *wizard) castSpell() error {
	if err := self.openScreen(); err != nil {
		return err
	}

	chosenProvider, err := self.chooseProvider("")
	if err != nil {
		return err
	}

	if chosenProvider.identifier == simulationIdentifier {
		self.isSimulationChosen = true
		return self.sayFarewell()
	}

	if err := self.refreshModels(); err != nil {
		return fmt.Errorf("find models: %w", err)
	}

	choices := choicesForProvider(self.getModels(), chosenProvider.identifier)
	if len(choices) == 0 {
		return fmt.Errorf("no models are available for %s", chosenProvider.name)
	}

	chosenIndex, err := self.choose(style.Prompt(modelPrompt), modelLabels(choices))
	if err != nil {
		return err
	}

	choice := choices[chosenIndex]
	effort := self.defaults.EffortFor(choice.EffortLevels)
	if effort == "" {
		return fmt.Errorf("model %s has no recognised effort levels", choice.ID)
	}

	selection := model.Selection{
		Provider: choice.Provider,
		Model:    choice.ID,
		Effort:   effort,
		IsFast:   self.defaults.IsFastFor(choice.Provider),
	}
	if err := self.setInitialModel(selection.String()); err != nil {
		return err
	}

	return self.sayFarewell()
}

func (self *wizard) sayFarewell() error {
	_, err := fmt.Fprintf(self.output, "\n%s %s\n", style.Success(successMark), style.Subtle(farewell))
	return err
}

func (self *wizard) openScreen() error {
	if self.stopPausing != nil {
		defer self.stopPausing()
	}

	width := max(style.Width(spoken(greeting, greetingAside)), style.Width(spoken(introduction, "")))

	if err := self.speakOut(greeting, greetingAside); err != nil {
		return err
	}

	self.rest(wakingRest)

	if err := self.speakOut(introduction, ""); err != nil {
		return err
	}

	if err := self.typeOut(style.Rule(strings.Repeat(openingRule, width)), ruleInterval); err != nil {
		return err
	}

	_, err := io.WriteString(self.output, "\n\n")
	return err
}

func spoken(text string, aside string) string {
	return util.JoinNonEmpty(text, aside)
}

func (self *wizard) speakOut(text string, aside string) error {
	if err := self.typeOut(text, typingInterval); err != nil {
		return err
	}

	if aside != "" {
		if _, err := io.WriteString(self.output, " "); err != nil {
			return err
		}
		if err := self.typeOut(style.Greeting(aside), drowsyInterval); err != nil {
			return err
		}
	}

	_, err := io.WriteString(self.output, "\n")
	return err
}

func restAfter(character rune) time.Duration {
	switch character {
	case ',', ';', ':':
		return clauseRest
	case '.', '?', '!':
		return sentenceRest
	default:
		return 0
	}
}

func (self *wizard) rest(interval time.Duration) {
	if !self.isOpeningSkipped && self.pause != nil && self.pause(interval) {
		self.isOpeningSkipped = true
	}
}

func (self *wizard) typeOut(text string, interval time.Duration) error {
	if self.isOpeningSkipped {
		_, err := io.WriteString(self.output, text)
		return err
	}

	runes := []rune(text)

	for at := 0; at < len(runes); {
		end := at
		for end < len(runes) && runes[end] == '\x1b' {
			end = escape.GetEnd(runes, end)
		}

		hasCharacter := end < len(runes)
		if hasCharacter {
			end++
		}

		if _, err := io.WriteString(self.output, string(runes[at:end])); err != nil {
			return err
		}
		if hasCharacter {
			self.rest(interval + restAfter(runes[end-1]))
		}
		if self.isOpeningSkipped {
			_, err := io.WriteString(self.output, string(runes[end:]))
			return err
		}

		at = end
	}

	return nil
}

func typingPause(input *os.File, output io.Writer) (func(time.Duration) bool, func(), error) {
	if !tty.Is(input) || !tty.Is(output) {
		return nil, func() {}, nil
	}

	restoreEcho, err := tty.SuppressEcho(input)
	if err != nil {
		return nil, nil, err
	}
	pause, stopReading := enterPause(input)
	stop := func() {
		stopReading()
		restoreEcho()
	}

	return pause, stop, nil
}

func enterPause(input *os.File) (func(time.Duration) bool, func()) {
	reader := tty.NewReader(input)
	enterPress := make(chan struct{})
	readingCompletion := make(chan struct{})

	go func() {
		defer close(readingCompletion)
		var character [1]byte
		for {
			count, err := reader.Read(character[:])
			if err != nil {
				return
			}
			if count == 1 && (character[0] == '\r' || character[0] == '\n') {
				close(enterPress)
				return
			}
		}
	}()

	pause := func(interval time.Duration) bool {
		timer := time.NewTimer(interval)
		defer timer.Stop()

		select {
		case <-enterPress:
			return true
		case <-timer.C:
			return false
		}
	}
	stop := func() {
		reader.Stop()
		<-readingCompletion
		reader.Close()
	}

	return pause, stop
}

func modelLabels(choices []model.Choice) []string {
	labels := make([]string, len(choices))
	notes := make([]string, len(choices))

	for i, choice := range choices {
		labels[i] = choice.Name
		if labels[i] == "" {
			labels[i] = choice.ID
		} else {
			notes[i] = choice.ID
		}
	}

	return alignedLabels(labels, notes)
}

func alignedLabels(labels []string, notes []string) []string {
	column := 0
	for i, label := range labels {
		if notes[i] != "" {
			column = max(column, style.Width(label))
		}
	}

	alignedLabels := make([]string, len(labels))
	for i, label := range labels {
		alignedLabels[i] = label
		if notes[i] != "" {
			padding := strings.Repeat(" ", column-style.Width(label))
			alignedLabels[i] = label + padding + noteGap + style.Qualifier(notes[i])
		}
	}

	return alignedLabels
}

func (self *wizard) chooseProvider(providerName string) (provider, error) {
	if providerName != "" {
		chosenProvider, found := providerNamed(providerName)
		if !found {
			return provider{}, fmt.Errorf(
				"unknown provider %q: choose one of %s",
				providerName,
				strings.Join(model.LoginProviderNames(), ", "),
			)
		}
		return chosenProvider, self.authenticate(chosenProvider, false)
	}

	candidates := self.candidateProviders()
	labels := providerLabels(candidates)

	for {
		chosenIndex, err := self.choose(style.Prompt(providerPrompt), labels)
		if err != nil {
			return provider{}, err
		}

		chosenProvider := candidates[chosenIndex]
		if chosenProvider.identifier == simulationIdentifier {
			return chosenProvider, nil
		}

		if err := self.authenticate(chosenProvider, true); err == nil {
			return chosenProvider, nil
		} else if _, writeErr := fmt.Fprintf(
			self.output,
			"%s\n\n",
			style.Failure(failureMark+" "+signInFailure, err),
		); writeErr != nil {
			return provider{}, writeErr
		}
	}
}

func (self *wizard) candidateProviders() []provider {
	if !self.isSimulationOffered {
		return providers
	}

	return append(append([]provider(nil), providers...), simulation)
}

func providerLabels(candidates []provider) []string {
	labels := make([]string, len(candidates))
	notes := make([]string, len(candidates))

	for i, candidate := range candidates {
		labels[i] = candidate.name
		if candidate.identifier == simulationIdentifier {
			labels[i] = style.Simulation(candidate.name)
		}
		notes[i] = candidate.note
	}

	return alignedLabels(labels, notes)
}

func providerNamed(identifier string) (provider, bool) {
	for _, candidate := range providers {
		if candidate.identifier == identifier {
			return candidate, true
		}
	}
	return provider{}, false
}

func (self *wizard) authenticate(chosenProvider provider, shouldSeparate bool) error {
	if shouldSeparate {
		if _, err := fmt.Fprintln(self.output); err != nil {
			return err
		}
	}

	err := self.login(chosenProvider, func(address string) {
		_, _ = fmt.Fprintf(
			self.output,
			"%s %s\n\n%s\n\n",
			style.Info(stepMark),
			fmt.Sprintf(openingBrowser, style.Subject(chosenProvider.name)),
			link.RenderURL(style.Link(address), address),
		)

		if self.openBrowser != nil {
			if err := self.openBrowser(address); err != nil {
				_, _ = fmt.Fprintf(self.output, "%s\n", style.Failure("%s", err))
				_, _ = fmt.Fprintf(self.output, "%s\n\n", style.Subtle(browserHint))
			}
		}

		_, _ = fmt.Fprintf(self.output, "%s\n\n", style.Subtle(pasteHint))
	})
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(
		self.output,
		"%s %s\n\n",
		style.Success(successMark),
		fmt.Sprintf(signedIn, style.Subject(chosenProvider.name)),
	)
	return err
}

func choicesForProvider(choices []model.Choice, providerName string) []model.Choice {
	matchingChoices := make([]model.Choice, 0, len(choices))
	for _, choice := range choices {
		if choice.Provider == providerName {
			matchingChoices = append(matchingChoices, choice)
		}
	}
	return matchingChoices
}

func login(terminal *os.File, output io.Writer) func(provider, func(string)) error {
	return func(chosenProvider provider, presentAddress func(string)) error {
		switch chosenProvider.identifier {
		case model.CodexProvider:
			return loginWithRedirect(terminal, func(ctx context.Context, redirects <-chan string) error {
				return codex.LoginWithRedirect(ctx, presentAddress, redirects)
			})
		case model.AnthropicProvider:
			return loginWithRedirect(terminal, func(ctx context.Context, redirects <-chan string) error {
				return anthropic.LoginWithRedirect(ctx, presentAddress, redirects)
			})
		case model.OpencodeGoProvider:
			return loginOpenCodeGo(terminal, output)
		default:
			return fmt.Errorf("unknown provider %q", chosenProvider.identifier)
		}
	}
}

func loginWithRedirect(
	input *os.File,
	authenticate func(context.Context, <-chan string) error,
) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	redirects := make(chan string, 1)
	go func() {
		redirect, err := tty.ReadLine(ctx, input)
		if err == nil {
			redirects <- redirect
		}
	}()

	return authenticate(ctx, redirects)
}

func loginOpenCodeGo(terminal *os.File, output io.Writer) error {
	return storeOpenCodeGoKey(terminal, output, opencodego.CredentialsPath(), validateOpenCodeGoKey)
}

func storeOpenCodeGoKey(
	input io.Reader,
	output io.Writer,
	path string,
	validateKey func(string) error,
) error {
	key, err := readOpenCodeGoKey(input, output)
	if err != nil {
		return err
	}
	if err := validateKey(key); err != nil {
		return fmt.Errorf("validate OpenCode Go API key: %w", err)
	}
	return opencodego.SaveKeyAt(path, key)
}

func validateOpenCodeGoKey(key string) error {
	return validateOpenCodeGoKeyAt(key, opencodego.UsageEndpointURL)
}

func validateOpenCodeGoKeyAt(key string, usageURL string) error {
	client, err := opencodego.New(opencodego.EndpointURL, key, validationModel, validationEffort, validationMaxOutputTokens)
	if err != nil {
		return err
	}
	client.UsageURL = usageURL
	_, err = client.UsageWindows(context.Background())
	return err
}

func readOpenCodeGoKey(input io.Reader, output io.Writer) (string, error) {
	if _, err := fmt.Fprint(output, style.Prompt(openCodeGoPrompt)); err != nil {
		return "", err
	}

	line, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read API key: %w", err)
	}

	key := strings.TrimSpace(line)
	if key == "" {
		return "", errors.New("OpenCode Go API key is empty")
	}

	return key, nil
}
