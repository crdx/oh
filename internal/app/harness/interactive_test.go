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
	t              *testing.T
	binary         string
	workspace      string
	stateDirectory string
	environment    []string
}

func newInteractiveRig(t *testing.T, answers ...string) *interactiveRig {
	t.Helper()

	turns := make([]sim.Turn, 0, len(answers))
	for _, answer := range answers {
		turns = append(turns, sim.Turn{Say: answer})
	}
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

	return session
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
	session.requireShown("tool definitions (")
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

var terminalQueries = regexp.MustCompile(`\x1b_G[^\x1b]*\x1b\\|\x1b\[c|\x1b\[[<>]1?u|\x1b\[\?(1004|2004|5522)[hl]|\x1b\[2[23];0t|\x1b\[\d? q|\x1b\]2;[^\x07\x1b]*(\x07|\x1b\\)`)

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
