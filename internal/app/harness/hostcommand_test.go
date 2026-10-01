package harness

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/commands"
	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/hostcommand"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/work"
)

type heldHostCommand struct {
	output   *hostcommand.Output
	started  chan string
	finishes chan hostcommand.Result
}

func holdHostCommands(self *App) *heldHostCommand {
	held := &heldHostCommand{
		started:  make(chan string, 1),
		finishes: make(chan hostcommand.Result),
	}

	self.hostCommand.run = func(stopContext context.Context, _ string, command string, output *hostcommand.Output) (hostcommand.Result, error) {
		held.output = output
		held.started <- command

		select {
		case result := <-held.finishes:
			return result, nil
		case <-stopContext.Done():
			return hostcommand.Result{Command: command, StoppedAfter: 4 * time.Second, IsStoppedByUser: true}, nil
		}
	}

	return held
}

func endHeldHostCommand(t *testing.T, self *App) {
	t.Helper()

	select {
	case outcome := <-self.hostCommandOutcomes():
		self.hostCommandEnded(outcome)
	case <-time.After(5 * time.Second):
		t.Fatal("the command never ended")
	}
}

func TestAHostCommandRunsWithoutHoldingTheInterface(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.settleAccess()
	held := holdHostCommands(self)

	if err := self.startHostCommand(t.TempDir(), "git push"); err != nil {
		t.Fatal(err)
	}
	if command := <-held.started; command != "git push" {
		t.Errorf("got command %q", command)
	}

	rows := self.hostCommandRows(replayColumns)
	if len(rows) != 1 || !strings.Contains(style.Plain(rows[0]), "git push") || !strings.Contains(style.Plain(rows[0]), "esc to stop") {
		t.Errorf("got rows %q, want the running command and how to stop it", rows)
	}
	if err := self.startHostCommand(t.TempDir(), "ls"); !errors.Is(err, errHostCommandUnderway) {
		t.Errorf("got error %v, want a second command refused while the first runs", err)
	}

	held.finishes <- hostcommand.Result{Command: "git push", Output: "Everything up-to-date\n"}
	endHeldHostCommand(t, self)

	if self.isHostCommandUnderway() || len(self.hostCommandRows(replayColumns)) != 0 {
		t.Error("the command still stands after it ended")
	}
	if !self.currentTurn.Running() {
		t.Fatal("the command left the conversation asleep, want a turn of its own")
	}
}

func TestAStopKeyStopsAHostCommandWithoutWakingTheConversation(t *testing.T) {
	for name, keypress := range map[string]key.Key{
		"escape": {Code: key.Escape},
		"ctrl+d": {Code: key.Rune, Value: 'd', Mod: key.Ctrl},
	} {
		t.Run(name, func(t *testing.T) {
			var screenOutput bytes.Buffer
			self := testConversation(t, &screenOutput)
			self.settleAccess()
			held := holdHostCommands(self)
			history := edit.NewHistory("", historyLimit)
			inputLine := edit.NewInput(history)

			if err := self.startHostCommand(t.TempDir(), "git push"); err != nil {
				t.Fatal(err)
			}
			<-held.started

			if !self.apply(inputLine, history, keypress) {
				t.Fatal("the key ended the session, want it to stop the command")
			}
			endHeldHostCommand(t, self)

			if self.currentTurn.Running() {
				t.Error("the stopped command woke the conversation, want it left for the next message")
			}
			if notice := strings.Join(self.pendingNotices.notices(), "\n"); !strings.Contains(notice, "Interrupted by the user after 4s") {
				t.Errorf("got pending notices %q, want the stop held for the next message", notice)
			}
		})
	}
}

func TestAStopKeyReachesTheTurnOnceTheCommandIsStopping(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.settleAccess()
	held := holdHostCommands(self)
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)

	if err := self.startHostCommand(t.TempDir(), "git push"); err != nil {
		t.Fatal(err)
	}
	<-held.started

	if !self.stopHostCommand() {
		t.Fatal("the command did not stop")
	}
	if self.stopHostCommand() {
		t.Error("a command already stopping was stopped again, want the key passed on")
	}

	self.apply(inputLine, history, key.Key{Code: key.Escape})
	endHeldHostCommand(t, self)
}

func TestAPrintedHostCommandIsAwaitedBeforeTheNextLine(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.settleAccess()
	self.runMode.isPlain = true
	self.hostCommand.run = func(_ context.Context, _ string, command string, _ *hostcommand.Output) (hostcommand.Result, error) {
		return hostcommand.Result{Command: command, IsStoppedByUser: true}, nil
	}

	systemCommands, err := commands.New(commands.Options{
		Workspace:        work.At(t.TempDir()),
		StartHostCommand: self.startHostCommand,
	})
	if err != nil {
		t.Fatal(err)
	}
	self.commands = fixtureRegistry(t, systemCommands)

	self.acceptPlainInput(edit.NewHistory("", historyLimit), "/!ls")

	if self.isHostCommandUnderway() {
		t.Error("the command was left running, want it awaited")
	}
	if len(self.pendingNotices.items) != 1 {
		t.Errorf("got %d pending notices, want the command's own", len(self.pendingNotices.items))
	}
}

func TestEndingTheSessionStopsAHostCommand(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	held := holdHostCommands(self)

	if err := self.startHostCommand(t.TempDir(), "git push"); err != nil {
		t.Fatal(err)
	}
	<-held.started

	self.endHostCommand()

	if self.isHostCommandUnderway() {
		t.Error("the command outlived the session")
	}
}

func TestGoldenARunningHostCommandStandsAboveTheInput(t *testing.T) {
	drawnPrinting := func(command string, printed string, elapsedTime time.Duration, columns int) func() string {
		return func() string {
			var screenOutput bytes.Buffer
			self := testConversation(t, &screenOutput)
			self.screen = output.NewTerminalOfSize(&screenOutput, columns, replayLines)
			self.settleAccess()
			startedAt := time.Unix(0, 0)
			self.now = func() time.Time { return startedAt }
			held := holdHostCommands(self)

			if err := self.startHostCommand(t.TempDir(), command); err != nil {
				t.Fatal(err)
			}
			<-held.started
			if _, err := held.output.Write([]byte(printed)); err != nil {
				t.Fatal(err)
			}
			self.now = func() time.Time { return startedAt.Add(elapsedTime) }

			history := edit.NewHistory("", historyLimit)
			self.show(edit.NewInput(history))
			output := screenOutput.String()
			self.endHostCommand()

			return output
		}
	}
	drawnAfter := func(command string, elapsedTime time.Duration, columns int) func() string {
		return drawnPrinting(command, "", elapsedTime, columns)
	}
	const fingerprintPrompt = "Place your right index finger on the fingerprint reader\n"

	passes := map[string]func() string{
		"showing what it printed last": drawnPrinting("sudo pacman -Syu", fingerprintPrompt, 3*time.Second, replayColumns),
		"cutting what it printed last": drawnPrinting("sudo pacman -Syu", fingerprintPrompt, 3*time.Second, 40),
		"overwriting its progress":     drawnPrinting("git clone https://example.com/repo", "Cloning into 'repo'...\nReceiving objects:  10%\rReceiving objects:  57%\r", 3*time.Second, replayColumns),
		"just started":                 drawnAfter("git push", 0, replayColumns),
		"spinning":                     drawnAfter("git push", 2250*time.Millisecond, replayColumns),
		"counting to its end":          drawnAfter("git push", 12*time.Second, replayColumns),
		"hinted at the edge":           drawnAfter("git push --force-with-lease origin main", 12*time.Second, 66),
		"cut without a hint":           drawnAfter("git push --force-with-lease origin main", 12*time.Second, 40),
	}

	compareWithGolden(t, "host-command-running", ".ansi", passes)
	compareWithGolden(t, "host-command-running", ".screen", shownPasses(t, passes))
}
