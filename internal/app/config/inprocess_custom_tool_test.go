package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool/command"
)

func TestInProcessCustomToolReceivesItsSessionAndScratchDirectory(t *testing.T) {
	directory := t.TempDir()
	script := filepath.Join(directory, "session-and-scratch")
	writeRunnableFile(t, script)
	if err := os.WriteFile(script, []byte("#!/bin/bash\nset -euo pipefail\nprintf '%s %s' \"$OH_SESSION\" \"$OH_SCRATCH_DIR\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "config.toml")
	body := fmt.Sprintf("[tool.scratch]\ndescription = \"report session and scratch\"\ncommand = [%q]\n", script)
	if err := writeConfigFile(path, body); err != nil {
		t.Fatal(err)
	}
	settings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	stateDirectory := t.TempDir()
	t.Setenv(location.StateDirVariable, stateDirectory)
	log, err := store.Create(location.GetSessionsDir(), store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	if err := log.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "report the session and scratch directory"}); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	stored, err := store.Read(location.GetSessionsDir(), log.Name())
	if err != nil {
		t.Fatal(err)
	}
	sessionName := stored.Name
	scratch := location.GetTmpDir(sessionName)
	tools, err := settings.BuildCustomTools(command.Options{Workspace: directory, Scratch: scratch, Session: sessionName})
	if err != nil {
		t.Fatal(err)
	}
	call, err := tools[0].Parse(`{}`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != sessionName+" "+scratch {
		t.Errorf("command received %q, want session name and scratch directory", result.Output)
	}
}

func TestInProcessWorkspaceSourceCannotDeclareACustomTool(t *testing.T) {
	directory := t.TempDir()
	globalPath := filepath.Join(directory, "config.toml")
	workspacePath := filepath.Join(directory, "oh.toml")
	if err := writeConfigFile(globalPath, ""); err != nil {
		t.Fatal(err)
	}
	if err := writeConfigFile(workspacePath, "[tool.weather]\ndescription = \"x\"\ncommand = [\"echo\"]\n"); err != nil {
		t.Fatal(err)
	}
	_, err := LoadSources(Source{Path: globalPath}, Source{Path: workspacePath, IsOverride: true})
	if err == nil || !strings.Contains(err.Error(), "cannot be overridden in oh.toml") {
		t.Errorf("got %v", err)
	}
}

func TestInProcessOnlyBashAcceptsHostNetworkSettings(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(path, "[tool.read]\nhost_network = \"ask\"\n"); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	want := path + ": tool.read.host_network is only valid for bash"
	if err == nil || err.Error() != want {
		t.Errorf("got %v, want %q", err, want)
	}

	if err := writeConfigFile(path, "[tool.bash]\nhost_network = \"ask\"\n"); err != nil {
		t.Fatal(err)
	}
	settings, err := Load(path)
	if err != nil {
		t.Fatalf("bash host_network setting was refused: %v", err)
	}
	if got := settings.Tools["bash"].HostNetwork.Rule; got != "ask" {
		t.Errorf("bash host_network rule is %q, want ask", got)
	}
}
