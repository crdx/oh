package harness

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestEveryControlCommandRunsThroughTheBinary(t *testing.T) {
	rig := newInteractiveRig(t, "Stored answer.")
	runTestBinary(t, rig.binary, rig.workspace, rig.environment, "-p", "--yolo", "-m", "opencode-go/fake", "a question")

	storedSessions := rig.storedSessions()
	if len(storedSessions) != 1 {
		t.Fatalf("got %d stored sessions, want one to control", len(storedSessions))
	}
	name := storedSessions[0].Name

	for _, arguments := range [][]string{
		{"sessions"},
		{"analyse", name},
		{"regenerate", name},
		{"migrate", "--dry-run"},
		{"gc"},
	} {
		t.Run(strings.Join(arguments, " "), func(t *testing.T) {
			output := runTestBinary(t, rig.binary, rig.workspace, rig.environment, append([]string{"--ctl"}, arguments...)...)
			if strings.TrimSpace(output) == "" {
				t.Error("the command said nothing")
			}
		})
	}

	if got := len(rig.storedSessions()); got != 1 {
		t.Errorf("got %d stored sessions after controlling them, want the one", got)
	}
}

func TestAControlCommandNobodyKnowsIsAnsweredWithTheUsage(t *testing.T) {
	rig := newInteractiveRig(t)

	for _, arguments := range [][]string{{"--ctl"}, {"--ctl", "nonsense"}} {
		t.Run(strings.Join(arguments, " "), func(t *testing.T) {
			command := exec.CommandContext(t.Context(), rig.binary, arguments...) //nolint:gosec // running the binary under test
			command.Env = rig.environment
			command.Dir = rig.workspace
			output, err := command.CombinedOutput()

			exitError, isExit := errors.AsType[*exec.ExitError](err)
			if !isExit || exitError.ExitCode() != 2 {
				t.Errorf("got %v, want the usage exit status", err)
			}
			if !strings.Contains(string(output), "oh --ctl migrate") {
				t.Errorf("the usage was not drawn, in %q", output)
			}
		})
	}
}

func TestAFailingControlCommandSaysWhyAndFails(t *testing.T) {
	rig := newInteractiveRig(t)

	command := exec.CommandContext(t.Context(), rig.binary, "--ctl", "analyse", "no-such-session") //nolint:gosec // running the binary under test
	command.Env = rig.environment
	command.Dir = rig.workspace
	output, err := command.CombinedOutput()

	exitError, isExit := errors.AsType[*exec.ExitError](err)
	if !isExit || exitError.ExitCode() != 1 {
		t.Errorf("got %v, want a failure, having drawn %q", err, output)
	}
	if !strings.Contains(string(output), "no-such-session") {
		t.Errorf("the failure did not name the session, in %q", output)
	}
}
