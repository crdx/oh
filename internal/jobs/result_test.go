package jobs

import (
	"context"
	"strings"
	"syscall"
	"testing"
	"time"

	"crdx.org/oh/internal/sandbox"
)

type resultRunner struct {
	result sandbox.Result
}

func (self resultRunner) Run(context.Context, string, string, sandbox.Policy) (sandbox.Result, error) {
	return self.result, nil
}

func (self resultRunner) Start(
	context.Context,
	string,
	string,
	sandbox.Policy,
	sandbox.Output,
) (sandbox.Command, error) {
	return resultCommand(self), nil
}

type resultCommand struct {
	result sandbox.Result
}

func (self resultCommand) Wait() (sandbox.Result, error) { return self.result, nil }

func (resultCommand) Signal(syscall.Signal) error { return nil }

func (resultCommand) Stop() {}

func TestAJobKilledByAProcessorLimitNamesAndQuantifiesWhy(t *testing.T) {
	policy := sandbox.Policy{MaxCPUTime: 2 * time.Second, Timeout: 30 * time.Second}
	manager := New(resultRunner{result: sandbox.Result{
		ExitCode: -1,
		Signal:   syscall.SIGXCPU,
		CPUTime:  3 * time.Second,
	}})

	if _, err := manager.Start(t.Context(), "build", t.TempDir(), "just build", policy); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Wait(t.Context(), []string{"build"}); err != nil {
		t.Fatal(err)
	}

	snapshot, err := manager.Status("build")
	if err != nil {
		t.Fatal(err)
	}

	for _, wanted := range []string{"SIGXCPU", "2s", "30s", "3s"} {
		if !strings.Contains(snapshot.Failure, wanted) {
			t.Errorf("failure %q does not contain %q", snapshot.Failure, wanted)
		}
		if !strings.Contains(snapshot.Describe(), wanted) {
			t.Errorf("description %q does not contain %q", snapshot.Describe(), wanted)
		}
	}
}
