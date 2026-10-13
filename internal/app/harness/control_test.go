package harness

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/location"
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
		{"check", name},
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

func TestAnalysisWritesItsCacheOnlyUnderTheOverriddenStateDirectory(t *testing.T) {
	rig := newInteractiveRig(t, "Stored answer.")
	runTestBinary(t, rig.binary, rig.workspace, rig.environment, "-p", "--yolo", "-m", "opencode-go/fake", "a question")

	root := filepath.Join(rig.stateDirectory, "org.crdx", "oh")
	cacheDirectory := t.TempDir()
	stateDirectory := t.TempDir()
	environment := slices.Concat(rig.environment, []string{
		location.StateDirVariable + "=" + root,
		"XDG_CACHE_HOME=" + cacheDirectory,
		"XDG_STATE_HOME=" + stateDirectory,
	})
	runTestBinary(t, rig.binary, rig.workspace, environment, "--ctl", "analyse")

	path := filepath.Join(root, "analysis.json")
	if info, err := os.Stat(path); err != nil || info.Size() == 0 {
		t.Fatalf("expected a populated analysis cache at %s, got %v, %v", path, info, err)
	}
	for _, directory := range []string{cacheDirectory, stateDirectory} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Errorf("analysis wrote outside the overridden state directory: %s contains %v", directory, entries)
		}
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
