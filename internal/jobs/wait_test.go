package jobs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/internal/sandbox"
)

func TestWaitingOnAFinishedJobReturnsAtOnce(t *testing.T) {
	manager := New(nil)
	manager.Restore([]Snapshot{{Name: "build", Command: "just build", State: StateComplete}})

	name, err := manager.Wait(t.Context(), []string{"build"})
	if err != nil {
		t.Errorf("got %v, want a finished job to be waited on for no time at all", err)
	}
	if name != "build" {
		t.Errorf("got %q, want the finished job's name", name)
	}
}

func TestWaitingOnSeveralFinishedJobsUsesTheRequestedOrder(t *testing.T) {
	manager := New(nil)
	manager.Restore([]Snapshot{
		{Name: "build", State: StateComplete},
		{Name: "docs", State: StateFailed},
	})

	name, err := manager.Wait(t.Context(), []string{"docs", "build"})
	if err != nil {
		t.Fatal(err)
	}
	if name != "docs" {
		t.Errorf("got %q, want the first requested finished job", name)
	}
}

func TestWaitingOnAnUnknownJobSaysSo(t *testing.T) {
	if _, err := New(nil).Wait(t.Context(), []string{"ghost"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want the job not to be found", err)
	}
}

func TestWaitingWithoutAJobIsRefused(t *testing.T) {
	if _, err := New(nil).Wait(t.Context(), nil); err == nil {
		t.Error("a wait without a job was accepted")
	}
}

func TestWaitingOnSeveralJobsRefusesAnUnknownNameBeforeReturning(t *testing.T) {
	manager := New(nil)
	manager.Restore([]Snapshot{{Name: "build", State: StateComplete}})

	if _, err := manager.Wait(t.Context(), []string{"build", "ghost"}); !errors.Is(err, ErrNotFound) ||
		!strings.Contains(err.Error(), "ghost") {
		t.Errorf("got %v, want the unknown watched job to be named", err)
	}
}

func TestWaitingOnSeveralJobsReturnsAsSoonAsAnyHasEnded(t *testing.T) {
	manager := New(nil)

	build, err := manager.claim("build", "just build", sandbox.Policy{})
	if err != nil {
		t.Fatal(err)
	}
	docs, err := manager.claim("docs", "python3", sandbox.Policy{})
	if err != nil {
		t.Fatal(err)
	}

	result := make(chan string)
	go func() {
		name, waitErr := manager.Wait(t.Context(), []string{"build", "docs"})
		if waitErr != nil {
			result <- waitErr.Error()
			return
		}
		result <- name
	}()

	manager.conclude(docs, StateComplete, 0, "")
	close(docs.over)

	if name := <-result; name != "docs" {
		t.Errorf("got %q, want the job that ended first", name)
	}
	if snapshot, statusErr := manager.Status("build"); statusErr != nil || !snapshot.IsLive() {
		t.Errorf("got %#v and %v, want the other job to remain live", snapshot, statusErr)
	}

	manager.conclude(build, StateComplete, 0, "")
	close(build.over)
}

func TestWaitingEndsWithTheContextThatAskedForIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := New(nil)

		if _, err := manager.claim("docs", "python3", sandbox.Policy{}); err != nil {
			t.Fatal(err)
		}

		waiting, stopWaiting := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer stopWaiting()

		if _, err := manager.Wait(waiting, []string{"docs"}); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("got %v, want the wait to end with its context", err)
		}
	})
}

func TestWaitingOnAJobThatCouldNotBeStartedReturnsAtOnce(t *testing.T) {
	manager := New(refusingRunner{})

	if _, err := manager.Start(t.Context(), "docs", t.TempDir(), "python3", sandbox.Policy{}); err == nil {
		t.Fatal("the job started, want it refused")
	}

	waiting, stopWaiting := context.WithTimeout(t.Context(), time.Second)
	defer stopWaiting()

	name, err := manager.Wait(waiting, []string{"docs"})
	if err != nil {
		t.Errorf("got %v, want a job that never ran to be waited on for no time at all", err)
	}
	if name != "docs" {
		t.Errorf("got %q, want the failed job's name", name)
	}
}

type refusingRunner struct{}

func (refusingRunner) Run(
	context.Context,
	string,
	string,
	sandbox.Policy,
) (sandbox.Result, error) {
	return sandbox.Result{}, errors.New("nothing runs here")
}

func (refusingRunner) Start(
	context.Context,
	string,
	string,
	sandbox.Policy,
	sandbox.Output,
) (sandbox.Command, error) {
	return nil, errors.New("nothing runs here")
}
