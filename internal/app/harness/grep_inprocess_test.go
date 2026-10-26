package harness

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/internal/sandbox/testexec"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool/middleware/truncate"
)

func TestInProcessAYoloSessionSearchesOnTheHostWithNoStandIn(t *testing.T) {
	own, err := os.Readlink("/proc/self/ns/user")
	if err != nil {
		t.Skipf("no user namespace to compare: %v", err)
	}
	realBinary, err := exec.LookPath("rg")
	if err != nil {
		t.Fatal(err)
	}
	realBinary, err = filepath.Abs(realBinary)
	if err != nil {
		t.Fatal(err)
	}
	readlink, err := exec.LookPath("readlink")
	if err != nil {
		t.Fatal(err)
	}
	readlink, err = filepath.Abs(readlink)
	if err != nil {
		t.Fatal(err)
	}

	script := declaredToolScript(t)
	wrapper := filepath.Join(filepath.Dir(script), "rg")
	if err := os.Rename(script, wrapper); err != nil {
		t.Fatal(err)
	}
	probeDirectory := t.TempDir()
	probePath := filepath.Join(probeDirectory, "namespace.txt")
	contents := fmt.Sprintf("#!/bin/bash\nset -euo pipefail\nOBSERVATION=$(printf 'namespace=%%s stand-in=%%s' \"$(%q /proc/self/ns/user)\" \"${%s:-no}\")\nprintf '%%s\\n' \"$OBSERVATION\" > %q\nprintf 'probe.txt:1:%%s\\n' \"$OBSERVATION\"\nexec %q \"$@\"\n", readlink, testexec.StandInVariable, probePath, realBinary)
	if err := os.WriteFile(wrapper, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(wrapper)+string(os.PathListSeparator)+os.Getenv("PATH"))
	workspace := work.At(reachableWorkspaceDir(t))
	if err := workspace.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	if err := os.WriteFile(filepath.Join(workspace.GetDir(), "probe.txt"), []byte("probe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := builtInConfig(t)
	state, err := buildAgentState(agentOptions{
		workspace:        workspace,
		mode:             caps.NewMode(caps.Unconfined()),
		isUnconfined:     true,
		roots:            openTestRoots(t, t.TempDir(), t.TempDir()),
		refuseHomeWrite:  allowWrite,
		sessionDirectory: t.TempDir(),
		ensurePersisted:  func() error { return nil },
		outputLimit:      truncate.NewLimit(settings.Defaults.ToolOutput.Bytes),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(state.Close)
	tools, err := state.tools(state.shell(nil, sandbox.Direct()), agentFeatures{})
	if err != nil {
		t.Fatal(err)
	}
	provider := &taskFlowProvider{turns: []taskFlowProviderTurn{
		{calls: []agent.ToolCall{{ID: "grep-probe", Name: "grep", Arguments: `{"pattern":"probe"}`}}},
		{say: "Searched."},
	}}
	assistant := agent.New("Search the workspace.", provider, tools)
	for _, err := range assistant.Stream(t.Context(), "search for the probe", nil) {
		if err != nil {
			t.Fatal(err)
		}
	}
	inputs := provider.requestInputs()
	if len(inputs) != 2 || len(inputs[1]) != 1 {
		t.Fatalf("grep did not return one result to the model: %+v", inputs)
	}
	want := "namespace=" + own + " stand-in=no"
	if result := inputs[1][0].Output; !strings.Contains(result, "probe.txt:1:probe") || !strings.Contains(result, want) {
		t.Fatalf("the model received grep result %q, want the real match and %q", result, want)
	}
	root, err := os.OpenRoot(probeDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	observation, err := root.ReadFile("namespace.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(observation), want) {
		t.Errorf("ripgrep probe observed %q, want %q", observation, want)
	}
}
