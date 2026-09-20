package job

import (
	"context"
	"errors"
	"io"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/internal/jobs"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/internal/stop"
	"crdx.org/oh/pkg/tool"
)

func TestAWaitReportsTheJobAndItsOutputOnceItHasEnded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := jobs.New(endingRunner{after: 50 * time.Millisecond, output: "all done\n"})
		defer func() { _ = manager.Close() }()

		if _, err := manager.Start(t.Context(), "build", t.TempDir(), "just build", sandbox.Policy{}); err != nil {
			t.Fatal(err)
		}

		report, err := waited(t.Context(), manager, []string{"build"}, waitForAny, time.Minute)
		if err != nil {
			t.Fatalf("the wait failed: %v", err)
		}

		if !strings.Contains(report, "build: complete") || !strings.Contains(report, "all done") {
			t.Errorf("got %q, want the finished job described with what it printed", report)
		}
	})
}

func TestAWaitOnAJobThatKeepsRunningGivesUpAndSaysSo(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := jobs.New(endingRunner{after: time.Hour, output: "serving\n"})
		defer func() { _ = manager.Close() }()

		if _, err := manager.Start(t.Context(), "docs", t.TempDir(), "python3 -m http.server", sandbox.Policy{}); err != nil {
			t.Fatal(err)
		}

		report, err := waited(t.Context(), manager, []string{"docs"}, waitForAny, 100*time.Millisecond)
		if err != nil {
			t.Fatalf("the wait failed: %v", err)
		}

		if !strings.Contains(report, "docs: running") {
			t.Errorf("got %q, want the job still described as running", report)
		}
		if !strings.Contains(report, "the wait gave up after 0.1s") {
			t.Errorf("got %q, want it to say how long it waited", report)
		}
		if !strings.Contains(report, "serving") {
			t.Errorf("got %q, want what the job has printed so far", report)
		}
	})
}

func TestAWaitOnSeveralJobsReportsEveryStatusWhenItsLimitIsReached(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := jobs.New(endingRunner{after: time.Hour})
		defer func() { _ = manager.Close() }()

		for _, name := range []string{"build", "docs"} {
			if _, err := manager.Start(t.Context(), name, t.TempDir(), name, sandbox.Policy{}); err != nil {
				t.Fatal(err)
			}
		}

		report, err := waited(t.Context(), manager, []string{"build", "docs"}, waitForAll, time.Millisecond)
		if err != nil {
			t.Fatalf("the wait failed: %v", err)
		}

		for _, wanted := range []string{"build: running", "docs: running", "before all watched jobs ended"} {
			if !strings.Contains(report, wanted) {
				t.Errorf("got %q, want it to carry %q", report, wanted)
			}
		}

		report, err = waited(t.Context(), manager, []string{"build", "docs"}, waitForAny, time.Millisecond)
		if err != nil {
			t.Fatalf("the second wait failed: %v", err)
		}
		if !strings.Contains(report, "before any watched job ended") {
			t.Errorf("got %q, want the any condition named", report)
		}
	})
}

func TestAWaitOnSeveralJobsCarriesWhatEachHasPrintedWhenItGivesUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := jobs.New(endingRunner{after: time.Hour, output: "listening on 8080\n"})
		defer func() { _ = manager.Close() }()

		for _, name := range []string{"build", "docs"} {
			if _, err := manager.Start(t.Context(), name, t.TempDir(), name, sandbox.Policy{}); err != nil {
				t.Fatal(err)
			}
		}

		synctest.Wait()

		report, err := waited(t.Context(), manager, []string{"build", "docs"}, waitForAll, time.Millisecond)
		if err != nil {
			t.Fatalf("the wait failed: %v", err)
		}

		if strings.Count(report, "listening on 8080") != 2 {
			t.Errorf("got %q, want what each job has printed rather than a call to fetch it", report)
		}
	})
}

func TestAWaitEndsWhenTheTurnDoes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := jobs.New(endingRunner{after: time.Hour})
		defer func() { _ = manager.Close() }()

		if _, err := manager.Start(t.Context(), "docs", t.TempDir(), "python3 -m http.server", sandbox.Policy{}); err != nil {
			t.Fatal(err)
		}

		turn, endTurn := context.WithCancelCause(t.Context())
		go func() {
			time.Sleep(50 * time.Millisecond)
			endTurn(stop.Because("access changed"))
		}()

		_, err := waited(turn, manager, []string{"docs"}, waitForAny, time.Hour)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want the wait to end with the turn that asked for it", err)
		}
		if got, want := err.Error(), "stopped because access changed"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestAJobFailureLeavesTheFinalOutputForTheAgentToMeasure(t *testing.T) {
	_, metrics, err := run(t.Context(), jobs.New(nil), nil, nil, Args{Action: actionStatus, Name: "ghost"})
	if err == nil {
		t.Fatal("the unknown job was accepted")
	}
	if metrics != (tool.ToolCallMetrics{}) {
		t.Errorf("got premature metrics %#v, want the agent to measure the final failure", metrics)
	}
}

func TestAWaitTakesTheAskedForLimitAndNeverExceedsTheCeiling(t *testing.T) {
	for _, current := range []struct {
		seconds int
		limit   time.Duration
	}{
		{seconds: 0, limit: waitLimit},
		{seconds: 30, limit: 30 * time.Second},
		{seconds: int(waitLimit.Seconds()) + 60, limit: waitLimit},
	} {
		if limit := getWaitLimit(Args{WaitSeconds: current.seconds}); limit != current.limit {
			t.Errorf("%d seconds gave %s, want %s", current.seconds, limit, current.limit)
		}
	}
}

func TestAWaitGivesUpAfterTheNumberOfSecondsItWasGiven(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := jobs.New(endingRunner{after: time.Hour})
		defer func() { _ = manager.Close() }()

		if _, err := manager.Start(t.Context(), "docs", t.TempDir(), "python3 -m http.server", sandbox.Policy{}); err != nil {
			t.Fatal(err)
		}

		started := time.Now()
		report, err := waited(t.Context(), manager, []string{"docs"}, waitForAny, getWaitLimit(Args{WaitSeconds: 1}))
		if err != nil {
			t.Fatalf("the wait failed: %v", err)
		}

		if elapsed := time.Since(started); elapsed != time.Second {
			t.Errorf("the wait took %s, want the second it was given", elapsed)
		}
		if !strings.Contains(report, "the wait gave up after 1s") {
			t.Errorf("got %q, want it to say it waited the second it was given", report)
		}
	})
}

func TestAWaitOnAnUnknownJobSaysSo(t *testing.T) {
	if _, err := waited(t.Context(), jobs.New(nil), []string{"ghost"}, waitForAny, time.Minute); !errors.Is(err, jobs.ErrNotFound) {
		t.Errorf("got %v, want the job not to be found", err)
	}
}

type endingRunner struct {
	after  time.Duration
	output string
}

func (endingRunner) Run(
	context.Context,
	string,
	string,
	sandbox.Policy,
) (sandbox.Result, error) {
	return sandbox.Result{}, errors.New("nothing is run outright here")
}

func (self endingRunner) Start(
	_ context.Context,
	_ string,
	_ string,
	_ sandbox.Policy,
	output sandbox.Output,
) (sandbox.Command, error) {
	if self.output != "" {
		_, _ = io.WriteString(output, self.output)
	}

	return &endingCommand{over: time.After(self.after), stopped: make(chan struct{})}, nil
}

type endingCommand struct {
	over    <-chan time.Time
	stopped chan struct{}
}

func (self *endingCommand) Wait() (sandbox.Result, error) {
	select {
	case <-self.over:
	case <-self.stopped:
	}

	return sandbox.Result{}, nil
}

func (self *endingCommand) Signal(syscall.Signal) error {
	self.Stop()

	return nil
}

func (self *endingCommand) Stop() {
	select {
	case <-self.stopped:
	default:
		close(self.stopped)
	}
}
