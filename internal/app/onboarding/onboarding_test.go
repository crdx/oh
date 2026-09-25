package onboarding

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/menu"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/style"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestGoldenFirstRunOnboardingMatchesTheGolden(t *testing.T) {
	var output bytes.Buffer
	restoreStyle := style.Init(&output)
	t.Cleanup(restoreStyle)

	choices := []int{0, 0}
	choiceIndex := 0
	var savedSelection string
	wereModelsRefreshed := false

	onboarding := wizard{
		output: &output,
		choose: func(prompt string, labels []string) (int, error) {
			chosen := choices[choiceIndex]
			choiceIndex++
			_, err := output.WriteString(menu.RenderMenu(prompt, labels, chosen))
			return chosen, err
		},
		login: func(chosen provider, presentAddress func(string)) error {
			if chosen.identifier != model.CodexProvider {
				t.Errorf("got provider %q", chosen.identifier)
			}
			presentAddress("https://example.test/authorise")
			return nil
		},
		refreshModels: func() error {
			wereModelsRefreshed = true
			return nil
		},
		getModels: func() []model.Choice {
			if !wereModelsRefreshed {
				t.Error("models were read before they were refreshed")
			}
			return []model.Choice{
				{Provider: model.CodexProvider, ID: "gpt-5.6-sol", Name: "GPT-5.6 Sol", EffortLevels: []string{"low", "medium", "high"}},
				{Provider: model.CodexProvider, ID: "gpt-5.6-luna", Name: "GPT-5.6 Luna", EffortLevels: []string{"medium", "high"}},
				{Provider: model.CodexProvider, ID: "gpt-5.6-terra", Name: "GPT-5.6 Terra", EffortLevels: []string{"medium", "high"}},
				{Provider: model.AnthropicProvider, ID: "claude-opus-5", Name: "Claude Opus 5", EffortLevels: []string{"high"}},
			}
		},
		setInitialModel: func(selection string) error {
			savedSelection = selection
			return nil
		},
	}

	if err := onboarding.castSpell(); err != nil {
		t.Fatal(err)
	}
	if savedSelection != "codex/gpt-5.6-sol@high" {
		t.Errorf("saved %q", savedSelection)
	}

	assertScreenGolden(t, "first-run", output.String())
}

func TestOnboardingSavesTheConfiguredModelDefaults(t *testing.T) {
	var savedSelection string
	choices := []int{0, 0}
	choiceIndex := 0

	onboarding := wizard{
		output: &bytes.Buffer{},
		choose: func(_ string, _ []string) (int, error) {
			chosen := choices[choiceIndex]
			choiceIndex++
			return chosen, nil
		},
		login:         func(provider, func(string)) error { return nil },
		refreshModels: func() error { return nil },
		getModels: func() []model.Choice {
			return []model.Choice{{
				Provider:     model.CodexProvider,
				ID:           "gpt-5.6-sol",
				EffortLevels: []string{"low", "medium", "high", "xhigh"},
			}}
		},
		setInitialModel: func(selection string) error {
			savedSelection = selection
			return nil
		},
		defaults: model.Defaults{Effort: "xhigh", IsFast: true},
	}

	if err := onboarding.castSpell(); err != nil {
		t.Fatal(err)
	}
	if savedSelection != "codex/gpt-5.6-sol@xhigh+fast" {
		t.Errorf("saved %q", savedSelection)
	}
}

func TestOnboardingWritesASelectionThatOrdinaryStartupCanLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	choices := []int{0, 0}
	choiceIndex := 0

	onboarding := wizard{
		output: &bytes.Buffer{},
		choose: func(_ string, _ []string) (int, error) {
			chosen := choices[choiceIndex]
			choiceIndex++
			return chosen, nil
		},
		login:         func(provider, func(string)) error { return nil },
		refreshModels: func() error { return nil },
		getModels: func() []model.Choice {
			return []model.Choice{{
				Provider:     model.CodexProvider,
				ID:           "gpt-5.6-sol",
				EffortLevels: []string{"low", "medium", "high"},
			}}
		},
		setInitialModel: func(selection string) error {
			_, err := setInitialModel(path, selection)
			return err
		},
	}

	if err := onboarding.castSpell(); err != nil {
		t.Fatal(err)
	}

	settings, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"codex/gpt-5.6-sol@high"}
	if !slices.Equal(settings.Model.RoundRobin, want) {
		t.Errorf("got model rotation %v, want %v", settings.Model.RoundRobin, want)
	}
}

func TestOnboardingOffersAnotherProviderAfterLoginFails(t *testing.T) {
	var output bytes.Buffer
	chosenProviders := []int{0, 1}
	providerChoices := 0
	loginAttempts := 0

	onboarding := wizard{
		output: &output,
		choose: func(prompt string, _ []string) (int, error) {
			if style.Plain(prompt) == modelPrompt {
				return 0, nil
			}
			chosen := chosenProviders[providerChoices]
			providerChoices++
			return chosen, nil
		},
		login: func(_ provider, _ func(string)) error {
			loginAttempts++
			if loginAttempts == 1 {
				return errors.New("authorisation was refused")
			}
			return nil
		},
		refreshModels: func() error { return nil },
		getModels: func() []model.Choice {
			return []model.Choice{{
				Provider:     model.AnthropicProvider,
				ID:           "claude-opus-5",
				EffortLevels: []string{"high"},
			}}
		},
		setInitialModel: func(string) error { return nil },
	}

	if err := onboarding.castSpell(); err != nil {
		t.Fatal(err)
	}
	if loginAttempts != 2 {
		t.Errorf("got %d login attempts", loginAttempts)
	}
	if !strings.Contains(output.String(), fmt.Sprintf(oauthSignIn.failure, "authorisation was refused")) {
		t.Errorf("failure was not shown in %q", output.String())
	}
}

func TestNamedLoginSkipsTheProviderPicker(t *testing.T) {
	var output bytes.Buffer
	var loggedInTo string
	harry := wizard{
		output: &output,
		choose: func(string, []string) (int, error) {
			t.Fatal("provider picker was shown")
			return 0, nil
		},
		login: func(chosen provider, _ func(string)) error {
			loggedInTo = chosen.identifier
			return nil
		},
	}

	if _, err := harry.chooseProvider(model.AnthropicProvider); err != nil {
		t.Fatal(err)
	}
	if loggedInTo != model.AnthropicProvider {
		t.Errorf("logged in to %q", loggedInTo)
	}
	welcome := fmt.Sprintf("%s %s\n\n", successMark, fmt.Sprintf(oauthSignIn.addition, anthropicName))
	if got, want := style.Plain(output.String()), welcome; got != want {
		t.Errorf("got output %q, want %q", got, want)
	}
}

func TestANamedExistingLoginCanBeRemoved(t *testing.T) {
	var output bytes.Buffer
	var removed string
	harry := wizard{
		output: &output,
		choose: func(prompt string, labels []string) (int, error) {
			if got := style.Plain(prompt); !strings.Contains(got, anthropicName) {
				t.Errorf("action prompt %q does not name the provider", got)
			}
			if !slices.Equal(labels, []string{oauthSignIn.renewal, oauthSignIn.removal, doNothing}) {
				t.Errorf("got actions %v", labels)
			}
			return slices.Index(labels, oauthSignIn.removal), nil
		},
		login: func(provider, func(string)) error {
			t.Fatal("sign-in was started")
			return nil
		},
		logout: func(chosen provider) error {
			removed = chosen.identifier
			return nil
		},
		isLoggedIn:            func(string) bool { return true },
		isManagingCredentials: true,
	}

	if _, err := harry.chooseProvider(model.AnthropicProvider); err != nil {
		t.Fatal(err)
	}
	if removed != model.AnthropicProvider {
		t.Errorf("removed %q", removed)
	}
	if got := style.Plain(output.String()); !strings.Contains(got, fmt.Sprintf(oauthSignIn.withdrawal, anthropicName)) {
		t.Errorf("sign-out was not confirmed in %q", got)
	}
}

func TestAKeyIsReplacedAndRemovedRatherThanSignedInto(t *testing.T) {
	var output bytes.Buffer
	var removed string
	harry := wizard{
		output: &output,
		choose: func(_ string, labels []string) (int, error) {
			if !slices.Equal(labels, []string{apiKey.renewal, apiKey.removal, doNothing}) {
				t.Errorf("got actions %v", labels)
			}
			return slices.Index(labels, apiKey.removal), nil
		},
		login: func(provider, func(string)) error {
			t.Error("a key was asked for")
			return nil
		},
		logout: func(chosen provider) error {
			removed = chosen.identifier
			return nil
		},
		isLoggedIn:            func(string) bool { return true },
		isManagingCredentials: true,
	}

	if _, err := harry.chooseProvider(model.OpencodeGoProvider); err != nil {
		t.Fatal(err)
	}
	if removed != model.OpencodeGoProvider {
		t.Errorf("removed %q", removed)
	}
	got := style.Plain(output.String())
	if !strings.Contains(got, fmt.Sprintf(apiKey.withdrawal, openCodeGoName)) {
		t.Errorf("the removal was not confirmed in %q", got)
	}
	if strings.Contains(strings.ToLower(got), "sign") {
		t.Errorf("a key was described as a sign-in in %q", got)
	}
}

func TestChoosingNothingLeavesANamedLoginAlone(t *testing.T) {
	var output bytes.Buffer
	harry := wizard{
		output: &output,
		choose: func(_ string, labels []string) (int, error) {
			return slices.Index(labels, doNothing), nil
		},
		login: func(provider, func(string)) error {
			t.Error("sign-in was started")
			return nil
		},
		logout: func(provider) error {
			t.Error("a provider was signed out of")
			return nil
		},
		isLoggedIn:            func(string) bool { return true },
		isManagingCredentials: true,
	}

	if _, err := harry.chooseProvider(model.AnthropicProvider); !errors.Is(err, ErrCancelled) {
		t.Errorf("got %v, want the login to be cancelled", err)
	}
}

func TestExistingLoginsAreNamedInTheProviderPicker(t *testing.T) {
	harry := wizard{
		isManagingCredentials: true,
		isLoggedIn: func(providerName string) bool {
			return providerName == model.AnthropicProvider
		},
	}

	candidates := harry.candidateProviders()
	for _, candidate := range candidates {
		if candidate.identifier == model.AnthropicProvider && !strings.Contains(candidate.note, candidate.credential.presence) {
			t.Errorf("signed-in provider has note %q", candidate.note)
		}
		if candidate.identifier != model.AnthropicProvider && strings.Contains(candidate.note, candidate.credential.presence) {
			t.Errorf("signed-out provider has note %q", candidate.note)
		}
	}
}

func TestTheSimulationIsOfferedOnlyWhereItCanBeStarted(t *testing.T) {
	firstRun := wizard{isSimulationOffered: true}
	offered := firstRun.candidateProviders()
	if len(offered) != len(providers)+1 || offered[len(offered)-1].identifier != simulationIdentifier {
		t.Errorf("a first run was offered %v", offered)
	}

	signingIn := wizard{}
	for _, candidate := range signingIn.candidateProviders() {
		if candidate.identifier == simulationIdentifier {
			t.Error("signing in offered the simulation")
		}
	}
}

func TestTheSimulationIsNotAProviderToSignInTo(t *testing.T) {
	harry := wizard{}
	if _, err := harry.chooseProvider(simulationIdentifier); err == nil ||
		!strings.Contains(err.Error(), "unknown provider") {
		t.Errorf("got %v", err)
	}
}

func TestNamedLoginRejectsAnUnknownProvider(t *testing.T) {
	harry := wizard{}
	if _, err := harry.chooseProvider("somewhere"); err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Errorf("got %v", err)
	}
}

func TestTheRuleIsDrawnOneCharacterAtATime(t *testing.T) {
	var writes []string
	pauses := 0

	painted := style.Rule(strings.Repeat(openingRule, 4))
	harry := wizard{
		output: writerFunc(func(piece []byte) (int, error) {
			writes = append(writes, string(piece))
			return len(piece), nil
		}),
		pause: func(time.Duration) bool {
			pauses++
			return false
		},
	}

	if err := harry.typeOut(painted, ruleInterval); err != nil {
		t.Fatal(err)
	}

	if got := strings.Join(writes, ""); got != painted {
		t.Errorf("drawing wrote %q, want %q", got, painted)
	}
	if pauses != 4 {
		t.Errorf("waited %d times, want one wait for each of the 4 characters", pauses)
	}
	if got := style.Plain(writes[0]); got != openingRule {
		t.Errorf("first write carried %q, want the opening sequences and %q", got, openingRule)
	}
	if last := writes[len(writes)-1]; style.Plain(last) != "" {
		t.Errorf("last write was %q, want the closing sequences alone", last)
	}
}

func TestTheOpeningIsTypedInTheRhythmOfSomebodyWakingUp(t *testing.T) {
	var output bytes.Buffer
	var waits []time.Duration

	harry := wizard{
		output: &output,
		pause: func(interval time.Duration) bool {
			waits = append(waits, interval)
			return false
		},
	}

	if err := harry.openScreen(); err != nil {
		t.Fatal(err)
	}

	rule := strings.Repeat(openingRule, style.Width(spoken(introduction, "")))
	lines := strings.Split(style.Plain(output.String()), "\n")
	if want := []string{spoken(greeting, greetingAside), introduction, rule, "", ""}; !slices.Equal(lines, want) {
		t.Errorf("the opening drew %q, want %q", lines, want)
	}

	want := slices.Concat(
		typingWaits(greeting, typingInterval),
		typingWaits(greetingAside, drowsyInterval),
		[]time.Duration{wakingRest},
		typingWaits(introduction, typingInterval),
		typingWaits(rule, ruleInterval),
	)
	if !slices.Equal(waits, want) {
		t.Errorf("the opening waited\n%v\nwant\n%v", waits, want)
	}
}

func TestEnterEndsTheOpeningAnimationAtOnce(t *testing.T) {
	var output bytes.Buffer
	pauses := 0

	harry := wizard{
		output: &output,
		pause: func(time.Duration) bool {
			pauses++
			return true
		},
	}

	if err := harry.openScreen(); err != nil {
		t.Fatal(err)
	}

	rule := strings.Repeat(openingRule, style.Width(spoken(introduction, "")))
	lines := strings.Split(style.Plain(output.String()), "\n")
	if want := []string{spoken(greeting, greetingAside), introduction, rule, "", ""}; !slices.Equal(lines, want) {
		t.Errorf("the completed opening drew %q, want %q", lines, want)
	}
	if pauses != 1 {
		t.Errorf("the completed opening paused %d times, want 1", pauses)
	}
}

func TestEnterIsConsumedBeforeTheMenuStarts(t *testing.T) {
	input, sent, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = input.Close()
		_ = sent.Close()
	})

	pause, stop := enterPause(input)
	if _, err := sent.WriteString("\nq"); err != nil {
		t.Fatal(err)
	}
	if !pause(time.Second) {
		t.Fatal("Enter did not end the opening pause")
	}
	stop()

	var remaining [1]byte
	if _, err := input.Read(remaining[:]); err != nil {
		t.Fatal(err)
	}
	if remaining[0] != 'q' {
		t.Errorf("the menu received %q after Enter, want q", remaining[0])
	}
}

func TestEnterDoesNotEchoWhileEndingTheOpeningPause(t *testing.T) {
	terminal, input := onboardingPTY(t)
	pause, stop, err := typingPause(input, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := terminal.WriteString("\r"); err != nil {
		t.Fatal(err)
	}
	if !pause(time.Second) {
		t.Fatal("Enter did not end the opening pause")
	}
	stop()

	descriptors := []unix.PollFd{{Fd: int32(terminal.Fd()), Events: unix.POLLIN}} //nolint:gosec // Unix file descriptors fit PollFd
	ready, err := unix.Poll(descriptors, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ready != 0 {
		var terminalOutput [16]byte
		count, err := terminal.Read(terminalOutput[:])
		if err != nil {
			t.Fatal(err)
		}
		t.Errorf("Enter echoed %q into the opening", terminalOutput[:count])
	}
}

func onboardingPTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()

	terminal, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal to test against: %v", err)
	}
	t.Cleanup(func() { _ = terminal.Close() })

	if err := unix.IoctlSetPointerInt(int(terminal.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(int(terminal.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close() })

	return terminal, input
}

func typingWaits(text string, interval time.Duration) []time.Duration {
	waits := make([]time.Duration, 0, len([]rune(text)))
	for _, character := range text {
		waits = append(waits, interval+restAfter(character))
	}

	return waits
}

func TestTypingRestsLongerAtTheEndOfASentenceThanWithinOne(t *testing.T) {
	if restAfter('.') <= restAfter(',') || restAfter(',') <= restAfter('o') {
		t.Errorf("rests do not lengthen with the pause a reader takes: %v, %v, %v",
			restAfter('o'), restAfter(','), restAfter('.'))
	}
}

func TestOnlyTheAsideIsDrawnInItalics(t *testing.T) {
	var output bytes.Buffer
	harry := wizard{output: &output}

	if err := harry.speakOut(greeting, greetingAside); err != nil {
		t.Fatal(err)
	}

	drawn := output.String()
	if !strings.HasPrefix(drawn, greeting+" ") {
		t.Errorf("the greeting %q painted the words, want the terminal's own colour", drawn)
	}

	italics := strings.Index(drawn, "\x1b[3m")
	if italics < 0 {
		t.Fatalf("the greeting %q drew no italics", drawn)
	}
	if style.Plain(strings.TrimSuffix(drawn[italics:], "\n")) != greetingAside {
		t.Errorf("the greeting %q put more than the aside in italics", drawn)
	}
}

func TestTypingWaitsForNobodyWhereTheScreenIsNotATerminal(t *testing.T) {
	pause, stop, err := typingPause(nil, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	if pause != nil {
		t.Error("expected no waiting when the typing cannot be watched")
	}
}

func TestOnboardingIsOnlyRequiredWithoutAnotherModelSelection(t *testing.T) {
	if !isRequired(Options{}, nil) {
		t.Error("expected a first run to need onboarding")
	}

	tests := map[string]struct {
		options          Options
		configuredModels []string
	}{
		"alternate endpoint": {options: Options{EndpointURL: "http://localhost"}},
		"requested model":    {options: Options{RequestedModel: "codex/model"}},
		"resumed session":    {options: Options{ResumedSession: "session"}},
		"configured model":   {configuredModels: []string{"codex/model@high"}},
	}
	for name, test := range tests {
		if isRequired(test.options, test.configuredModels) {
			t.Errorf("%s unexpectedly needed onboarding", name)
		}
	}
}

func visibleTranscript(rendered string) string {
	rendered = link.Plain(rendered)
	rendered = strings.ReplaceAll(rendered, "\r\n", "\n")

	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}

type writerFunc func([]byte) (int, error)

func (self writerFunc) Write(piece []byte) (int, error) {
	return self(piece)
}

func TestALocalModelOverrideAvoidsFirstRunOnboarding(t *testing.T) {
	directory := t.TempDir()
	overridePath := filepath.Join(directory, "oh.toml")
	if err := os.WriteFile(overridePath, []byte("[model]\nround_robin = [\"anthropic/local\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	settings, _, err := PrepareConfig(Options{
		ConfigSources: []config.Source{
			{Path: filepath.Join(directory, "missing.toml")},
			{Path: overridePath, IsOverride: true},
		},
		IsPrinting: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(settings.Model.RoundRobin, []string{"anthropic/local"}) {
		t.Errorf("got model rotation %#v", settings.Model.RoundRobin)
	}
}

func TestAPrintedFirstRunIsRefusedRatherThanAsked(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = reader.Close()
		_ = writer.Close()
	})

	var shown strings.Builder
	_, _, err = PrepareConfig(Options{Input: reader, Output: &shown, IsPrinting: true})

	if !errors.Is(err, ErrNobodyToAsk) {
		t.Fatalf("got %v, want a refusal to ask", err)
	}
	if shown.String() != "" {
		t.Errorf("a printed first run drew %q", shown.String())
	}
}
