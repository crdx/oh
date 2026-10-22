package sandbox

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

const (
	sweepVariable     = "IO_SANDBOX_TEST_SWEEP"
	inheritedFileDesc = 100
)

func TestTheSweepKeepsTheRuntimeAliveAndClosesInheritedDescriptors(t *testing.T) {
	if os.Getenv(sweepVariable) != "" {
		sweepAndExec(t)
		return
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	child := exec.CommandContext(t.Context(), self, "-test.run=^"+t.Name()+"$") //nolint:gosec // the binary is this one
	child.Env = []string{sweepVariable + "=1", "PATH=" + os.Getenv("PATH")}
	output, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("the swept process failed: %v\n%s", err, output)
	}
	if said := strings.TrimSpace(string(output)); said != "swept" {
		t.Errorf("the command said %q, want the inherited descriptor gone", said)
	}
}

func sweepAndExec(t *testing.T) {
	t.Helper()
	reader, _, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Dup2(int(reader.Fd()), inheritedFileDesc); err != nil {
		t.Fatal(err)
	}

	sweepInheritedDescriptors()
	runtime.GC()

	script := "test -e /proc/self/fd/100 && echo leaked || echo swept"
	//nolint:gosec // the script is a constant
	err = syscall.Exec("/bin/sh", []string{"sh", "-c", script}, []string{"PATH=" + os.Getenv("PATH")})
	t.Fatalf("could not exec: %v", err)
}
