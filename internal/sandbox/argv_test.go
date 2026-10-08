package sandbox_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"crdx.org/oh/internal/sandbox"
)

func TestArgvRejectsCommandsThatWouldNeedAShell(t *testing.T) {
	for _, arguments := range [][]string{
		nil,
		{"printf", "hello"},
		{"/usr/bin/printf", "bad\x00argument"},
	} {
		for name, runner := range map[string]sandbox.ArgvRunner{
			"DirectArgv":     sandbox.DirectArgv(),
			"UnconfinedArgv": sandbox.UnconfinedArgv(),
		} {
			if _, err := runner.RunArgv(t.Context(), t.TempDir(), arguments, sandbox.Policy{}); err == nil {
				t.Errorf("%s accepted arguments %q", name, arguments)
			}
		}
	}
}

func TestArgvRefusesUnconfinedExecution(t *testing.T) {
	_, err := sandbox.DirectArgv().RunArgv(
		t.Context(), t.TempDir(), []string{"/usr/bin/true"}, sandbox.Policy{Yolo: true},
	)
	if err == nil || !strings.Contains(err.Error(), "unconfined") {
		t.Errorf("got %v, want an unconfined refusal", err)
	}
}

func TestUnconfinedArgvRunsInTheHarnessUserNamespace(t *testing.T) {
	own, err := os.Readlink("/proc/self/ns/user")
	if err != nil {
		t.Skipf("no user namespace to compare: %v", err)
	}
	readlink, err := exec.LookPath("readlink")
	if err != nil {
		t.Skipf("readlink is unavailable: %v", err)
	}

	result, err := sandbox.UnconfinedArgv().RunArgv(
		t.Context(), t.TempDir(), []string{readlink, "/proc/self/ns/user", "`x`; $HOME"}, sandbox.Policy{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != own+"\n" {
		t.Errorf("got %q, want %q alone, with no shell reading the second argument", result.Output, own)
	}
}
