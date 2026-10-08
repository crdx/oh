package harness

import (
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"crdx.org/oh/internal/app/backend"
	"crdx.org/oh/internal/app/background"
	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/ptytest"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/sim"
)

const (
	interactiveColumns  = 100
	interactiveRows     = 30
	interactiveDeadline = 20 * time.Second
	interactiveQuiet    = 200 * time.Millisecond
	endOfTransmission   = "\x04"
	pressEnter          = "\r"
	pressEscape         = "\x1b"
	clearInput          = "\x15"
	readyBanner         = "ready in "
)

func TestAnInteractiveSessionAnswersWhatIsTypedAndGivesTheTerminalBack(t *testing.T) {
	rig := newInteractiveRig(t, "Interactive answer.", "Typed answer.")

	session := rig.start("--yolo", "-m", "opencode-go/fake", "first question")
	before := session.modes()

	session.waitFor("Interactive answer.")
	session.typeText("second question" + pressEnter)
	session.waitFor("Typed answer.")
	session.quit()

	if after := session.modes(); after.Lflag != before.Lflag || after.Iflag != before.Iflag || after.Oflag != before.Oflag {
		t.Errorf("the terminal was left in another mode: got %+v, want %+v", after, before)
	}
	if got := len(rig.storedSessions()); got != 1 {
		t.Errorf("got %d stored sessions, want the one that was answered", got)
	}
}

func TestAConversationChosenFromThePickerResumesWhereItLeftOff(t *testing.T) {
	rig := newInteractiveRig(t, "First answer.", "Resumed answer.")
	runTestBinary(t, rig.binary, rig.workspace, rig.environment, "-p", "--yolo", "-m", "opencode-go/fake", "first question")

	storedSessions := rig.storedSessions()
	if len(storedSessions) != 1 {
		t.Fatalf("got %d stored sessions, want one to choose", len(storedSessions))
	}

	session := rig.start("-r")
	session.waitFor(storedSessions[0].Name)
	session.typeText(pressEnter)
	session.waitFor("First answer.")
	session.typeText(pressEnter)
	session.waitForAnother("First answer.")
	session.typeText("second question" + pressEnter)
	session.waitFor("Resumed answer.")
	session.quit()

	if got := len(rig.storedSessions()); got != 1 {
		t.Errorf("got %d stored sessions, want the resumed one alone", got)
	}
}

func TestTheSessionPickerUsesTheWorkspaceTheme(t *testing.T) {
	rig := newInteractiveRig(t, "First answer.")
	runTestBinary(t, rig.binary, rig.workspace, rig.environment, "-p", "--yolo", "-m", "opencode-go/fake", "first question")

	storedSessions := rig.storedSessions()
	if len(storedSessions) != 1 {
		t.Fatalf("got %d stored sessions, want one to choose", len(storedSessions))
	}
	if err := os.WriteFile(
		filepath.Join(rig.workspace, "oh.toml"),
		[]byte("[ui.theme.dark]\naccent = \"#010203\"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	session := rig.start("-r")
	session.waitFor(storedSessions[0].Name)
	if stream := session.String(); !strings.Contains(stream, "\x1b[38;2;1;2;3m") {
		t.Errorf("session picker did not use the workspace accent: %q", stream)
	}
	session.typeText("\x03")
	session.waitToExit()
}

const (
	whiteBackground = "\x1b]11;rgb:ffff/ffff/ffff\x1b\\"
	lightAccent     = "\x1b[38;2;166;93;46m"
	darkAccent      = "\x1b[38;2;192;128;80m"
)

func TestThePickersAreDrawnForALightTerminal(t *testing.T) {
	rig := newInteractiveRig(t, "First answer.")
	rig.backgroundReply = whiteBackground
	runTestBinary(t, rig.binary, rig.workspace, rig.environment, "-p", "--yolo", "-m", "opencode-go/fake", "first question")

	storedSessions := rig.storedSessions()
	if len(storedSessions) != 1 {
		t.Fatalf("got %d stored sessions, want one to choose", len(storedSessions))
	}

	for _, picker := range []struct {
		name      string
		arguments []string
		shown     string
	}{
		{name: "the session picker", arguments: []string{"-r"}, shown: storedSessions[0].Name},
		{name: "the model picker", arguments: []string{"--yolo", "-m"}, shown: "fake"},
	} {
		session := rig.start(picker.arguments...)
		session.waitFor(picker.shown)
		stream := session.String()
		if !strings.Contains(stream, lightAccent) {
			t.Errorf("%s did not use the light accent: %q", picker.name, stream)
		}
		if strings.Contains(stream, darkAccent) {
			t.Errorf("%s used the dark accent: %q", picker.name, stream)
		}
		session.typeText("\x03")
		session.waitToExit()
	}
}

func TestAModelChosenFromThePickerAnswersTheConversation(t *testing.T) {
	rig := newInteractiveRig(t, "Picked answer.")

	session := rig.start("--yolo", "-m")
	session.waitFor("fake")
	session.typeAndSettle("opencode")
	session.typeText(pressEnter)
	session.waitFor(readyBanner)
	session.typeText("a question" + pressEnter)
	session.waitFor("Picked answer.")
	session.quit()

	storedSessions := rig.storedSessions()
	if len(storedSessions) != 1 {
		t.Fatalf("got %d stored sessions, want the one that was answered", len(storedSessions))
	}
	if choice := storedSessions[0].Meta.ModelChoice; choice == nil || choice.Provider != "opencode-go" || choice.ID != "fake" {
		t.Errorf("the session holds %+v, want the model that was picked", choice)
	}
}

func TestAModelPickerLeftWithoutAChoiceStartsNothing(t *testing.T) {
	rig := newInteractiveRig(t)

	session := rig.start("-m")
	session.waitFor("fake")
	session.typeText("\x03")
	session.waitToExit()

	if got := len(rig.storedSessions()); got != 0 {
		t.Errorf("got %d stored sessions, want none", got)
	}
}

func TestWhatIsTypedWhileTheSessionStartsIsKept(t *testing.T) {
	rig := newInteractiveRig(t, "Early answer.")

	session := rig.start("--yolo", "-m", "opencode-go/fake")
	session.typeText("typed early")
	session.waitFor(readyBanner)
	session.typeText(pressEnter)
	session.waitFor("Early answer.")
	session.quit()
}

type interactiveRig struct {
	t               *testing.T
	binary          string
	workspace       string
	stateDirectory  string
	environment     []string
	backgroundReply string
	isAnsweringLate bool
}

func newInteractiveRig(t *testing.T, answers ...string) *interactiveRig {
	t.Helper()

	turns := make([]sim.Turn, 0, len(answers))
	for _, answer := range answers {
		turns = append(turns, sim.Turn{Say: answer})
	}

	return newScriptedRig(t, turns...)
}

func newScriptedRig(t *testing.T, turns ...sim.Turn) *interactiveRig {
	t.Helper()

	endpoint := sim.New(&sim.Scenario{Model: "fake", Turns: turns})
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)

	stateDirectory := t.TempDir()
	return &interactiveRig{
		t:              t,
		binary:         buildTestBinary(t),
		workspace:      reachableWorkspaceDir(t),
		stateDirectory: stateDirectory,
		environment: append(
			interactiveEnvironment(t, stateDirectory),
			backend.EndpointVariable+"="+endpoint.Addresses(server.URL)[sim.Completions],
			"TERM=xterm-256color",
		),
	}
}

func (self *interactiveRig) start(arguments ...string) *interactiveSession {
	self.t.Helper()

	controller, terminal := ptytest.OpenSized(self.t, interactiveColumns, interactiveRows)

	command := exec.CommandContext(self.t.Context(), self.binary, arguments...) //nolint:gosec // running the binary under test
	command.Env = self.environment
	command.Dir = self.workspace
	command.Stdin, command.Stdout, command.Stderr = terminal, terminal, terminal
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}

	session := &interactiveSession{t: self.t, command: command, terminal: terminal, typing: controller}
	session.exited = make(chan error, 1)
	if err := command.Start(); err != nil {
		self.t.Fatal(err)
	}
	self.t.Cleanup(func() { _ = command.Process.Kill() })
	go func() { session.exited <- command.Wait() }()
	session.Transcript = ptytest.Record(controller)
	if !self.isAnsweringLate {
		answerBackgroundProbes(session, self.backgroundReply)
	}

	return session
}

const deviceReply = "\x1b[?62;4c"

func answerBackgroundProbes(session *interactiveSession, backgroundReply string) {
	go func() {
		for asked := 1; session.WaitForCount(background.Query, asked, interactiveDeadline); asked++ {
			if _, err := session.typing.WriteString(backgroundReply + deviceReply); err != nil {
				return
			}
		}
	}()
}

func (self *interactiveRig) storedSessions() []*store.Session {
	self.t.Helper()

	storedSessions, err := store.List(filepath.Join(self.stateDirectory, "org.crdx", "oh", "sessions"))
	if err != nil {
		self.t.Fatal(err)
	}

	return storedSessions
}

func interactiveEnvironment(t *testing.T, stateDirectory string) []string {
	t.Helper()

	configDirectory := t.TempDir()
	configPath := filepath.Join(configDirectory, "org.crdx", "oh", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, fmt.Appendf(nil, "version = %d\n", config.Format), 0o600); err != nil {
		t.Fatal(err)
	}

	environment := testBinaryEnvironment(t, stateDirectory)
	return append(environment, "XDG_CONFIG_HOME="+configDirectory, "OH_STATE_DIR=")
}

type interactiveSession struct {
	*ptytest.Transcript

	t        *testing.T
	command  *exec.Cmd
	terminal *os.File
	exited   chan error
	typing   *os.File
}

func (self *interactiveSession) modes() unix.Termios {
	self.t.Helper()

	modes, err := unix.IoctlGetTermios(int(self.terminal.Fd()), unix.TCGETS)
	if err != nil {
		self.t.Fatal(err)
	}

	return *modes
}

func (self *interactiveSession) typeText(text string) {
	self.t.Helper()

	if _, err := self.typing.WriteString(text); err != nil {
		self.t.Fatal(err)
	}
}

func (self *interactiveSession) typeAndSettle(text string) {
	self.t.Helper()

	drawn := self.Len()
	self.typeText(text)
	if !self.WaitForGrowth(drawn, interactiveDeadline) {
		self.t.Fatalf("typing %q drew nothing, having drawn:\n%s", text, self.String())
	}
	self.waitToSettle()
}

func (self *interactiveSession) waitFor(text string) {
	self.t.Helper()

	self.waitForCount(text, 1)
}

func (self *interactiveSession) waitForAnother(text string) {
	self.t.Helper()

	self.waitForCount(text, self.Count(text)+1)
}

func (self *interactiveSession) waitForCount(text string, count int) {
	self.t.Helper()

	if !self.WaitForCount(text, count, interactiveDeadline) {
		self.t.Fatalf("%q was never drawn, having drawn:\n%s", text, self.String())
	}
	self.waitToSettle()
}

func (self *interactiveSession) waitToSettle() {
	self.WaitToSettle(interactiveQuiet)
}

func (self *interactiveSession) waitToExit() {
	self.t.Helper()

	select {
	case err := <-self.exited:
		if err != nil {
			self.t.Fatalf("oh left with %v, having drawn:\n%s", err, self.String())
		}
	case <-time.After(interactiveDeadline):
		self.t.Fatalf("oh never left, having drawn:\n%s", self.String())
	}
}

func (self *interactiveSession) quit() {
	self.t.Helper()

	self.typeText(endOfTransmission)
	self.waitToExit()
}

func TestCommandsTypedIntoAnInteractiveSessionAreAnsweredInPlace(t *testing.T) {
	rig := newInteractiveRig(t)

	session := rig.start("--yolo", "-m", "opencode-go/fake")
	session.waitFor(readyBanner)

	session.typeAndSettle("/ctx")
	session.typeAndSettle(pressEnter)
	session.requireShown(" tool definitions")
	session.typeAndSettle(pressEscape)

	session.typeAndSettle("/grants")
	session.typeAndSettle(pressEscape)
	session.typeAndSettle(pressEnter)
	session.requireShown("Paths:")
	session.requireHidden("Caps:")
	session.requireShown("rxw  yolo shell")
	session.typeAndSettle(pressEscape)

	session.quit()
}

var terminalQueries = regexp.MustCompile(`\x1b\]11;\?\x1b\\|\x1b_G[^\x1b]*\x1b\\|\x1b\[c|\x1b\[[<>]1?u|\x1b\[\?(1004|2004|5522)[hl]|\x1b\[2[23];0t|\x1b\[\d? q|\x1b\]2;[^\x07\x1b]*(\x07|\x1b\\)`)

func (self *interactiveSession) requireShown(text string) {
	self.t.Helper()

	shown := strings.Join(self.screen(), "\n")
	if !strings.Contains(shown, text) {
		self.t.Errorf("%q is not on the screen, which shows:\n%s", text, shown)
	}
}

func (self *interactiveSession) requireHidden(text string) {
	self.t.Helper()

	shown := strings.Join(self.screen(), "\n")
	if strings.Contains(shown, text) {
		self.t.Errorf("%q is on the screen, which shows:\n%s", text, shown)
	}
}

func (self *interactiveSession) screen() []string {
	self.t.Helper()

	drawn := terminalQueries.ReplaceAllString(self.String(), "")
	return playScreenOfSize(self.t, drawn, interactiveColumns, interactiveRows).text()
}

func TestCommandArgumentsAreCompletedFromTheSessionItself(t *testing.T) {
	rig := newInteractiveRig(t)

	session := rig.start("--yolo", "-m", "opencode-go/fake")
	session.waitFor(readyBanner)
	session.requireHidden("opencode-go/fake")

	session.typeAndSettle("/new opencode-go/")
	session.requireShown("› opencode-go/fake@")
	session.typeAndSettle("\t")
	session.requireShown("/new opencode-go/fake@")
	session.requireHidden("› ")
	session.typeAndSettle(clearInput)

	session.typeAndSettle("/fork opencode-go/fa")
	session.requireShown("› opencode-go/fake@")
	session.typeAndSettle(pressEscape)
	session.typeAndSettle(clearInput)

	session.typeAndSettle("/!")
	session.requireHidden("no matching commands")
	session.requireHidden("› ")
	session.typeAndSettle(clearInput)

	session.quit()
}

func TestStartingASessionAsksTheTerminalAboutGraphicsOnce(t *testing.T) {
	rig := newInteractiveRig(t)

	session := rig.start("--yolo", "-m", "opencode-go/fake")
	session.waitFor(readyBanner)

	if asked := session.Count(graphicsProbe); asked != 1 {
		t.Errorf("startup asked the terminal about graphics %d times, want once", asked)
	}

	session.quit()
}

func TestASessionIsDrawnForTheBackgroundTheTerminalReports(t *testing.T) {
	const (
		black     = "\x1b]11;rgb:0000/0000/0000\x07"
		darkGrey  = "\x1b]11;rgb:5050/5050/5050\x1b\\"
		lightGrey = "\x1b]11;rgb:a0/a0/a0\x07"

		greyDarkAccent  = "\x1b[38;2;255;196;154m"
		greyLightAccent = "\x1b[38;2;106;50;16m"
	)
	accents := []string{darkAccent, greyDarkAccent, greyLightAccent, lightAccent}

	for name, test := range map[string]struct {
		reply      string
		appearance string
		want       string
	}{
		"a dark terminal":                            {reply: black, want: darkAccent},
		"a dark grey terminal":                       {reply: darkGrey, want: greyDarkAccent},
		"a light grey terminal":                      {reply: lightGrey, want: greyLightAccent},
		"a light terminal":                           {reply: whiteBackground, want: lightAccent},
		"a terminal that keeps its colour to itself": {want: darkAccent},
		"a light terminal told to be dark":           {reply: whiteBackground, appearance: "dark", want: darkAccent},
		"a dark terminal told to be grey-dark":       {reply: black, appearance: "grey-dark", want: greyDarkAccent},
		"a dark terminal told to be grey-light":      {reply: black, appearance: "grey-light", want: greyLightAccent},
		"a dark terminal told to be light":           {reply: black, appearance: "light", want: lightAccent},
	} {
		t.Run(name, func(t *testing.T) {
			rig := newInteractiveRig(t)
			rig.backgroundReply = test.reply
			if test.appearance != "" {
				if err := os.WriteFile(
					filepath.Join(rig.workspace, "oh.toml"),
					[]byte("[ui.theme]\nappearance = \""+test.appearance+"\"\n"),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
			}

			session := rig.start("--yolo", "-m", "opencode-go/fake")
			session.waitFor(readyBanner)
			if asked := session.Count(background.Query); asked != 1 {
				t.Errorf("startup asked the terminal about its background %d times, want once", asked)
			}
			stream := session.String()
			for _, accent := range accents {
				if isDrawn := strings.Contains(stream, accent); isDrawn != (accent == test.want) {
					t.Errorf("drawing the accent %q is %t, want only %q: %q", accent, isDrawn, test.want, stream)
				}
			}

			session.quit()
		})
	}
}

func TestWhatIsTypedWhileTheTerminalIsAskedAboutItsBackgroundIsKept(t *testing.T) {
	rig := newInteractiveRig(t, "Early answer.")
	rig.isAnsweringLate = true

	session := rig.start("--yolo", "-m", "opencode-go/fake")
	if !session.WaitForCount(background.Query, 1, interactiveDeadline) {
		t.Fatal("the terminal was never asked about its background")
	}
	session.typeText("typed early")
	session.typeText(whiteBackground + deviceReply)
	session.waitFor(readyBanner)
	session.requireShown("typed early")
	if stream := session.String(); !strings.Contains(stream, lightAccent) {
		t.Errorf("the session did not use the light accent: %q", stream)
	}
	session.typeText(pressEnter)
	session.waitFor("Early answer.")
	session.quit()
}

func TestATerminalThatAnswersNothingIsDrawnDarkOnceItStopsWaiting(t *testing.T) {
	rig := newInteractiveRig(t)
	rig.isAnsweringLate = true

	session := rig.start("--yolo", "-m", "opencode-go/fake")
	session.waitFor(readyBanner)
	if stream := session.String(); !strings.Contains(stream, darkAccent) || strings.Contains(stream, lightAccent) {
		t.Errorf("a silent terminal was not drawn in the dark palette: %q", stream)
	}
	session.quit()
}

func TestNothingAsksAboutTheBackgroundWhenNothingWillBeColoured(t *testing.T) {
	for name, test := range map[string]struct {
		environment []string
		arguments   []string
		shown       string
	}{
		"colour turned off": {
			environment: []string{"NO_COLOR=1"},
			arguments:   []string{"--yolo", "-m", "opencode-go/fake"},
			shown:       readyBanner,
		},
		"the version asked for": {arguments: []string{"--version"}},
	} {
		t.Run(name, func(t *testing.T) {
			rig := newInteractiveRig(t)
			rig.environment = append(rig.environment, test.environment...)

			session := rig.start(test.arguments...)
			if test.shown != "" {
				session.waitFor(test.shown)
				session.quit()
			} else {
				session.waitToExit()
			}

			if asked := session.Count(background.Query); asked != 0 {
				t.Errorf("the terminal was asked about its background %d times, want never", asked)
			}
		})
	}
}

func TestAPrintedAnswerIsDrawnForTheBackgroundTheTerminalReports(t *testing.T) {
	rig := newInteractiveRig(t, "Use `paint` here.")
	rig.backgroundReply = whiteBackground

	session := rig.start("-p", "--yolo", "-m", "opencode-go/fake", "which function?")
	session.waitToExit()

	stream := session.String()
	if asked := session.Count(background.Query); asked != 1 {
		t.Errorf("print mode asked the terminal about its background %d times, want once", asked)
	}
	if !strings.Contains(stream, lightAccent+"paint") {
		t.Errorf("the printed answer did not use the light accent: %q", stream)
	}
}

func TestASessionIsDrawnInTheWorkspaceTheme(t *testing.T) {
	rig := newInteractiveRig(t)
	if err := os.WriteFile(
		filepath.Join(rig.workspace, "oh.toml"),
		[]byte("[ui.theme.dark]\naccent = \"#010203\"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	session := rig.start("--yolo", "-m", "opencode-go/fake")
	session.waitFor(readyBanner)
	if stream := session.String(); !strings.Contains(stream, "\x1b[38;2;1;2;3m") {
		t.Errorf("the session did not use the workspace accent: %q", stream)
	}

	session.quit()
}
