package testexec_test

import (
	"os/exec"
	"strings"
	"testing"

	"crdx.org/oh/internal/sandbox/testexec"
	"crdx.org/oh/pkg/sandbox"
)

func TestTheStandInMarksWhatItRuns(t *testing.T) {
	printenv, err := exec.LookPath("printenv")
	if err != nil {
		t.Skipf("printenv is unavailable: %v", err)
	}

	result, err := testexec.New().RunArgv(
		t.Context(), t.TempDir(), []string{printenv, testexec.StandInVariable}, sandbox.Policy{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(result.Output) != "1" {
		t.Errorf("got %q, want the stand-in to mark its child", result.Output)
	}
}
