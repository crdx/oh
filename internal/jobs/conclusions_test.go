package jobs

import (
	"context"
	"errors"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/internal/sandbox"
)

type heldRunner struct {
	release chan struct{}
	result  sandbox.Result
	printed string
	spool   sandbox.Output
}

func (self *heldRunner) Run(context.Context, string, string, sandbox.Policy) (sandbox.Result, error) {
	return self.result, nil
}

func (self *heldRunner) Start(
	_ context.Context,
	_ string,
	_ string,
	_ sandbox.Policy,
	output sandbox.Output,
) (sandbox.Command, error) {
	self.spool = output

	return &heldCommand{runner: self}, nil
}

type heldCommand struct {
	runner *heldRunner
}

func (self *heldCommand) Wait() (sandbox.Result, error) {
	<-self.runner.release

	if self.runner.printed != "" {
		if _, err := self.runner.spool.Write([]byte(self.runner.printed)); err != nil {
			return sandbox.Result{}, err
		}
	}

	return self.runner.result, nil
}

func (self *heldCommand) Signal(syscall.Signal) error {
	close(self.runner.release)

	return nil
}

func (self *heldCommand) Stop() {}

func newHeldRunner(result sandbox.Result) *heldRunner {
	return &heldRunner{release: make(chan struct{}), result: result}
}

func nextConclusion(t *testing.T, manager *Manager) (Conclusion, bool) {
	t.Helper()

	select {
	case conclusion := <-manager.Conclusions():
		return conclusion, true
	case <-time.After(time.Second):
		return Conclusion{}, false
	}
}

func TestAJobThatEndsOnItsOwnAnnouncesItself(t *testing.T) {
	runner := newHeldRunner(sandbox.Result{ExitCode: 2})
	runner.printed = "undefined: getWidth\nexit status 1\n"
	manager := New(runner)

	if _, err := manager.Start(t.Context(), "build", t.TempDir(), "just build", sandbox.Policy{}); err != nil {
		t.Fatal(err)
	}

	close(runner.release)

	conclusion, isAnnounced := nextConclusion(t, manager)
	if !isAnnounced {
		t.Fatal("a job that ended on its own announced nothing")
	}
	if conclusion.Snapshot.Name != "build" || conclusion.Snapshot.State != StateFailed || conclusion.Snapshot.ExitCode != 2 {
		t.Errorf("got %#v, want the failed job", conclusion.Snapshot)
	}
	if conclusion.Output != runner.printed {
		t.Errorf("got %q, want everything the job printed", conclusion.Output)
	}
	report := Report(conclusion.Snapshot.Describe(), conclusion.Output, conclusion.DroppedBytes)
	if !strings.Contains(report, "undefined: getWidth") {
		t.Errorf("got %q, want the report to carry the output nobody would otherwise ask for", report)
	}
}

func TestAJobStoppedFromTheKeyboardAnnouncesNothing(t *testing.T) {
	runner := newHeldRunner(sandbox.Result{})
	manager := New(runner)

	if _, err := manager.Start(t.Context(), "docs", t.TempDir(), "python3", sandbox.Policy{}); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.Stop("docs"); err != nil {
		t.Fatal(err)
	}

	select {
	case conclusion := <-manager.Conclusions():
		t.Errorf("got %#v, want a stopped job to announce nothing", conclusion)
	default:
	}
}

func TestAHeldJobAnnouncesNothingAndAReleasedOneDoes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, isReleased := range []bool{false, true} {
			runner := newHeldRunner(sandbox.Result{})
			manager := New(runner)

			if _, err := manager.Start(t.Context(), "build", t.TempDir(), "just build", sandbox.Policy{}); err != nil {
				t.Fatal(err)
			}
			over, release, err := manager.Hold("build")
			if err != nil {
				t.Fatal(err)
			}
			if isReleased {
				release()
			}

			close(runner.release)
			<-over
			synctest.Wait()

			select {
			case conclusion := <-manager.Conclusions():
				if !isReleased {
					t.Errorf("got %#v, want a held job to announce nothing", conclusion)
				}
			default:
				if isReleased {
					t.Error("a released job announced nothing")
				}
			}
		}
	})
}

func TestHoldingAnUnknownJobSaysSo(t *testing.T) {
	if _, _, err := New(nil).Hold("ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want the job to be unknown", err)
	}
}
