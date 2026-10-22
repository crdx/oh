package priority_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"crdx.org/oh/internal/priority"
)

func TestACommandAndItsChildrenRunAtLowerPriority(t *testing.T) {
	var output strings.Builder
	command := exec.CommandContext(t.Context(), "sh", "-c", "nice; sh -c nice")
	command.Stdout = &output
	command.Stderr = &output
	own, err := unix.Getpriority(unix.PRIO_PROCESS, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	niceness := max(20-own, priority.Niceness)
	if err := priority.Start(command); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("%v: %s", err, output.String())
	}
	if want := fmt.Sprintf("%d\n%d\n", niceness, niceness); output.String() != want {
		t.Errorf("got %q, want %q", output.String(), want)
	}
}

func TestStartingACommandLeavesThisProcessAtItsOwnPriority(t *testing.T) {
	before, err := unix.Getpriority(unix.PRIO_PROCESS, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(context.Background(), "true")
	if err := priority.Start(command); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
	after, err := unix.Getpriority(unix.PRIO_PROCESS, os.Getpid())
	if err != nil || after != before {
		t.Errorf("this process moved from %d to %d (%v)", before, after, err)
	}
}
