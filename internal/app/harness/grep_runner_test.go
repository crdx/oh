package harness

import (
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/backend"
	"crdx.org/oh/internal/sandbox/testexec"
	"crdx.org/oh/internal/sim"
)

func searchedBy(t *testing.T, arguments ...string) string {
	t.Helper()

	readlink, err := exec.LookPath("readlink")
	if err != nil {
		t.Skipf("readlink is unavailable: %v", err)
	}
	standIns := t.TempDir()
	script := fmt.Sprintf(
		"#!/bin/sh\nprintf 'probe.txt:1:namespace=%%s stand-in=%%s\\n' \"$(%s /proc/self/ns/user)\" \"${%s:-no}\"\n",
		readlink, testexec.StandInVariable,
	)
	//nolint:gosec // the stand-in for ripgrep has to be runnable
	if err := os.WriteFile(filepath.Join(standIns, "rg"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	workspaceDir := reachableWorkspaceDir(t)
	if err := os.WriteFile(filepath.Join(workspaceDir, "probe.txt"), []byte("probe\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	endpoint := sim.New(&sim.Scenario{
		Model: "fake",
		Turns: []sim.Turn{
			{Calls: []sim.Call{{Name: "grep", Arguments: `{"pattern":"probe"}`}}},
			{Say: "Searched."},
		},
	})
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)

	environment := append(
		testBinaryEnvironment(t, t.TempDir()),
		backend.EndpointVariable+"="+endpoint.Addresses(server.URL)[sim.Messages],
		"PATH="+standIns+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	runTestBinary(t, buildTestBinary(t), workspaceDir, environment, append([]string{"-p", "-m", "anthropic/fake"}, arguments...)...)

	requests := endpoint.Requests()
	if len(requests) < 2 {
		t.Fatalf("the endpoint saw %d requests", len(requests))
	}
	for _, entry := range requests[1].Input {
		if entry.Type == sim.CallOutput {
			return entry.Output
		}
	}
	t.Fatal("the model was told nothing of the search")
	return ""
}

func TestAYoloSessionSearchesOnTheHostWithNoStandIn(t *testing.T) {
	own, err := os.Readlink("/proc/self/ns/user")
	if err != nil {
		t.Skipf("no user namespace to compare: %v", err)
	}

	told := searchedBy(t, "--yolo", "search for the probe")

	if want := "namespace=" + own + " stand-in=no"; !strings.Contains(told, want) {
		t.Errorf("the model was told %q, want %q from ripgrep run on the host", told, want)
	}
}
