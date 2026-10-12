package harness

import (
	"bytes"
	"slices"
	"testing"
	"time"

	"crdx.org/oh/internal/app/commands"
	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/experimental"
	"crdx.org/oh/internal/app/hostcommand"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/app/snippets"
	"crdx.org/oh/internal/app/work"
)

func TestLeadingSpacesStayLiteralAtStartupAndInPlainMode(t *testing.T) {
	for name, message := range map[string]string{
		"command": " /help",
		"snippet": " //add review this",
	} {
		for _, isPlain := range []bool{false, true} {
			t.Run(name, func(t *testing.T) {
				var screenOutput bytes.Buffer
				self := testConversation(t, &screenOutput)
				wasCommandRun := false
				self.commands = fixtureCommandRegistryWithSnippets(t,
					map[string]snippets.Definition{
						"add": {Prompt: "Expand this: {{ .Arg }}", Arguments: snippets.ArgumentsRequired},
					},
					slash.Command{Name: "help", Run: func(slash.Context, slash.Arguments) error {
						wasCommandRun = true
						return nil
					}},
				)
				history := edit.NewHistory("", historyLimit)
				inputLine := edit.NewInput(history)
				if isPlain {
					self.acceptPlainInput(history, message)
				} else {
					self.acceptInitialInput(inputLine, history, message)
					for report := range self.currentTurn.Events() {
						self.takeTurn(report)
					}
					self.finish()
				}
				if wasCommandRun || !hasUserMessage(self.recordedEvents, message[1:]) {
					t.Errorf("%q was dispatched or changed instead of sent literally", message)
				}
				recalled := edit.NewInput(history)
				recalled.Apply(key.Key{Code: key.Up}, false)
				if got := recalled.Text(); got != message {
					t.Errorf("history recalled %q, want %q", got, message)
				}
			})
		}
	}
}

func TestAltEnterRejectsAnUnknownSlashCommandInsteadOfSendingIt(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.currentTurn = Turn{Stream: testRunningTurnStream()}
	self.experimental = experimental.New(map[string]any{string(experimental.EnterInterrupts): true})
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)
	inputLine.SetText("/unknown")

	self.apply(inputLine, history, key.Key{Code: key.Enter, Mod: key.Alt})
	if self.currentTurn.Cancelled() || self.currentTurn.HasInterjections() {
		t.Error("the unknown slash command interrupted or entered the model queue")
	}
	if got := inputLine.Text(); got != "/unknown" {
		t.Errorf("the rejected command left input %q", got)
	}
	if got := self.feedback.Message().Text; got != "Command not found: /unknown (prepend a space to send)" {
		t.Errorf("got feedback %q", got)
	}
}

func TestALeadingSpaceKeepsAHostCommandFromRunning(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.currentTurn = Turn{Stream: testRunningTurnStream()}
	commandSet, err := commands.New(commands.Options{
		Workspace: work.At(t.TempDir()),
		StartHostCommand: func(string, string) error {
			t.Error("the literal host command was executed")
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	self.commands = fixtureRegistry(t, commandSet)

	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)
	inputLine.SetText(" /!echo hello")
	self.apply(inputLine, history, key.Key{Code: key.Enter})
	if got := self.currentTurn.GetInterjections(); !slices.Equal(got, []string{"/!echo hello"}) {
		t.Errorf("queued %q instead of the literal host command", got)
	}
	if self.isHostCommandUnderway() {
		t.Error("a literal host command started on the host")
	}
}

func TestAltEnterRunsAHostCommandWithoutInterruptingTheTurn(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}
	self.experimental = experimental.New(map[string]any{string(experimental.EnterInterrupts): true})
	held := holdHostCommands(self)

	commandSet, err := commands.New(commands.Options{
		Workspace:        work.At(t.TempDir()),
		StartHostCommand: self.startHostCommand,
	})
	if err != nil {
		t.Fatal(err)
	}
	self.commands = fixtureRegistry(t, commandSet)

	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)
	inputLine.SetText("/!echo hello")
	self.apply(inputLine, history, key.Key{Code: key.Enter, Mod: key.Alt})

	select {
	case command := <-held.started:
		if command != "echo hello" {
			t.Errorf("ran %q, want echo hello", command)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("alt+enter did not run the host command")
	}
	if self.currentTurn.Cancelled() {
		t.Error("alt+enter interrupted the running turn")
	}
	if got := inputLine.Text(); got != "" {
		t.Errorf("input still holds %q", got)
	}

	held.finishes <- hostcommand.Result{Command: "echo hello", Output: "hello\n"}
	endHeldHostCommand(t, self)
	if !self.currentTurn.Running() {
		t.Error("the command ended the model turn")
	}
}
