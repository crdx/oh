package editor

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/style"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestConfigurationCanReplaceItsEditorCommand(t *testing.T) {
	initial := Command{"first-editor", "--wait"}
	configuration := NewConfiguration(initial)
	initial[0] = "mutated"

	if got := configuration.GetCommand(); !slices.Equal(got, Command{"first-editor", "--wait"}) {
		t.Errorf("got initial command %v", got)
	}

	replacement := Command{"second-editor"}
	configuration.ReplaceCommand(replacement)
	replacement[0] = "mutated"
	got := configuration.GetCommand()
	if !slices.Equal(got, Command{"second-editor"}) {
		t.Errorf("got replacement command %v", got)
	}
	got[0] = "mutated"
	if current := configuration.GetCommand(); !slices.Equal(current, Command{"second-editor"}) {
		t.Errorf("returned command changed the configuration to %v", current)
	}
}

func TestConfiguredEditorIsUsed(t *testing.T) {
	command, err := buildCommand(Command{"configured-editor", "--wait"}, []string{"/tmp/config.toml"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(command.Args, []string{"configured-editor", "--wait", "/tmp/config.toml"}) {
		t.Errorf("got arguments %v", command.Args)
	}
}

func TestEveryPathIsPassedToTheEditor(t *testing.T) {
	command, err := buildCommand(Command{"configured-editor"}, []string{"/tmp/skills", "/tmp/more-skills"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(command.Args, []string{"configured-editor", "/tmp/skills", "/tmp/more-skills"}) {
		t.Errorf("got arguments %v", command.Args)
	}
}

func TestAnEditorIsDetectedWhenNoneIsConfigured(t *testing.T) {
	directory := t.TempDir()
	writeExecutable(t, directory, "code")
	t.Setenv("PATH", directory)

	command, err := buildCommand(nil, []string{"/tmp/config.toml"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(command.Args, []string{"code", "--wait", "/tmp/config.toml"}) {
		t.Errorf("got arguments %v", command.Args)
	}
}

func TestTheFirstEditorOnPathIsPreferred(t *testing.T) {
	directory := t.TempDir()
	writeExecutable(t, directory, "subl")
	writeExecutable(t, directory, "code")
	t.Setenv("PATH", directory)

	command, found := Detect()
	if !found {
		t.Fatal("expected an editor to be detected")
	}
	if !slices.Equal(command, Command{"subl", "--wait"}) {
		t.Errorf("got command %v", command)
	}
}

func TestAnEditorMustBeConfiguredWhenNoneIsFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := buildCommand(Command{" "}, []string{"/tmp/config.toml"})
	if err == nil || !strings.Contains(err.Error(), "set editor in config.toml") {
		t.Errorf("got %v", err)
	}
}

func TestATerminalEditorIsRefused(t *testing.T) {
	for _, name := range []string{"vim", "nvim", "nano", "/usr/bin/emacs"} {
		t.Run(name, func(t *testing.T) {
			_, err := buildCommand(Command{name}, []string{"/tmp/config.toml"})
			if err == nil || !strings.Contains(err.Error(), "is not supported yet") {
				t.Errorf("got %v", err)
			}
		})
	}
}

func TestAGraphicalEditorIsNotRefused(t *testing.T) {
	for _, name := range []string{"subl", "code", "/usr/bin/gvim", "zed"} {
		if IsTerminalEditor(name) {
			t.Errorf("%s was taken for a terminal editor", name)
		}
	}
}

func writeExecutable(t *testing.T, directory string, name string) {
	t.Helper()

	//nolint:gosec // it has to be executable for LookPath to find it
	err := os.WriteFile(filepath.Join(directory, name), []byte("#!/bin/sh\n"), 0o755)
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenReportsAnEditorThatCannotStart(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	err := Open(Command{"missing-editor"}, "/tmp/config.toml")
	if err == nil || !strings.Contains(err.Error(), "could not start editor") {
		t.Errorf("got %v", err)
	}
}

func TestGoldenEditorExitFailureMatchesTheGolden(t *testing.T) {
	t.Cleanup(style.Init(&strings.Builder{}))

	command := exec.CommandContext(t.Context(), "sh", "-c", "exit 42")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}

	var errors bytes.Buffer
	reportExit(command, &errors)

	goldenPath := filepath.Join("testdata", "exit-failure.txt")
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, errors.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if errors.String() != string(want) {
		t.Errorf("editor failure differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, &errors, want)
	}
}
