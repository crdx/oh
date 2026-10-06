package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/internal/sandbox"
)

type scriptedRun struct {
	lasts    time.Duration
	exitCode int
	printed  string
	refusal  error
}

type scriptedRunner struct {
	mutex  sync.Mutex
	runs   []scriptedRun
	starts int
}

func (*scriptedRunner) Run(context.Context, string, string, sandbox.Policy) (sandbox.Result, error) {
	return sandbox.Result{}, errors.New("nothing is run outright here")
}

func (self *scriptedRunner) Start(
	_ context.Context,
	_ string,
	_ string,
	_ sandbox.Policy,
	output sandbox.Output,
) (sandbox.Command, error) {
	self.mutex.Lock()
	run := scriptedRun{lasts: time.Hour}
	if self.starts < len(self.runs) {
		run = self.runs[self.starts]
	}
	self.starts++
	self.mutex.Unlock()

	if run.refusal != nil {
		return nil, run.refusal
	}

	return &scriptedCommand{
		run:     run,
		output:  output,
		over:    time.After(run.lasts),
		stopped: make(chan struct{}),
	}, nil
}

func (self *scriptedRunner) startCount() int {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.starts
}

type scriptedCommand struct {
	run      scriptedRun
	output   sandbox.Output
	over     <-chan time.Time
	stopped  chan struct{}
	stopping sync.Once
}

func (self *scriptedCommand) Wait() (sandbox.Result, error) {
	select {
	case <-self.over:
	case <-self.stopped:
		return sandbox.Result{}, nil
	}

	if self.run.printed != "" {
		if _, err := self.output.Write([]byte(self.run.printed)); err != nil {
			return sandbox.Result{}, err
		}
	}

	return sandbox.Result{ExitCode: self.run.exitCode}, nil
}

func (self *scriptedCommand) Signal(syscall.Signal) error {
	self.Stop()

	return nil
}

func (self *scriptedCommand) Stop() {
	self.stopping.Do(func() { close(self.stopped) })
}

func takeConclusion(t *testing.T, manager *Manager) Conclusion {
	t.Helper()

	synctest.Wait()

	select {
	case conclusion := <-manager.Conclusions():
		return conclusion
	default:
		t.Fatal("no conclusion was announced")
		return Conclusion{}
	}
}

func requireNoConclusion(t *testing.T, manager *Manager) {
	t.Helper()

	synctest.Wait()

	select {
	case conclusion := <-manager.Conclusions():
		t.Fatalf("got %#v, want nothing announced", conclusion)
	default:
	}
}

func TestARespawningJobAnnouncesEachExitAndStartsAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runner := &scriptedRunner{runs: []scriptedRun{
			{lasts: 5 * time.Second, printed: "changed: a.go\n"},
			{lasts: 5 * time.Second, exitCode: 1, printed: "changed: b.go\n"},
		}}
		manager := New(runner)
		defer func() { _ = manager.Close() }()

		snapshot, err := manager.StartRespawning(t.Context(), "watch", t.TempDir(), "inotifywait .", sandbox.Policy{})
		if err != nil {
			t.Fatal(err)
		}
		if got, want := snapshot.Outcome(), "running for 0s, run 1, respawns on exit"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}

		time.Sleep(5 * time.Second)
		first := takeConclusion(t, manager)
		if got, want := first.Snapshot.Describe(), "watch: complete after 5s, run 1, respawned"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if first.Output != "changed: a.go\n" {
			t.Errorf("got %q, want the first run's output", first.Output)
		}

		time.Sleep(5 * time.Second)
		second := takeConclusion(t, manager)
		if got, want := second.Snapshot.Describe(), "watch: failed after 5s, exit(1), run 2, respawned"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if second.Output != "changed: b.go\n" {
			t.Errorf("got %q, want only the second run's output", second.Output)
		}

		output, status, err := manager.Output("watch")
		if err != nil {
			t.Fatal(err)
		}
		if got, want := status.Outcome(), "running for 0s, run 3, respawns on exit"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if output != "" {
			t.Errorf("got %q, want the third run to start with nothing printed", output)
		}
		if got := runner.startCount(); got != 3 {
			t.Errorf("got %d starts, want 3", got)
		}
	})
}

func TestStoppingARespawningJobEndsItForGood(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runner := &scriptedRunner{}
		manager := New(runner)
		defer func() { _ = manager.Close() }()

		if _, err := manager.StartRespawning(t.Context(), "watch", t.TempDir(), "inotifywait .", sandbox.Policy{}); err != nil {
			t.Fatal(err)
		}

		snapshot, err := manager.Stop("watch")
		if err != nil {
			t.Fatal(err)
		}
		if got, want := snapshot.Outcome(), "stopped after 0s, run 1"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}

		requireNoConclusion(t, manager)
		if got := runner.startCount(); got != 1 {
			t.Errorf("got %d starts, want the stopped job never started again", got)
		}
	})
}

func TestARespawningJobGivesUpAfterRunsThatEachEndAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runs := make([]scriptedRun, 0, QuickRunsTolerated+1)
		runs = append(runs, scriptedRun{lasts: time.Minute})
		for range QuickRunsTolerated {
			runs = append(runs, scriptedRun{exitCode: 127, printed: "inotifywait: command not found\n"})
		}
		runner := &scriptedRunner{runs: runs}
		manager := New(runner)
		defer func() { _ = manager.Close() }()

		if _, err := manager.StartRespawning(t.Context(), "watch", t.TempDir(), "inotifywait .", sandbox.Policy{}); err != nil {
			t.Fatal(err)
		}

		time.Sleep(time.Minute)

		var outcomes []string
		for range QuickRunsTolerated + 1 {
			outcomes = append(outcomes, takeConclusion(t, manager).Snapshot.Outcome())
		}

		want := []string{
			"complete after 1m, run 1, respawned",
			"failed after 0s, exit(127), run 2, respawned",
			"failed after 0s, exit(127), run 3, respawned",
			fmt.Sprintf(
				"failed after 0s, exit(127), run 4, not respawned after %d runs in a row each ended within 2s",
				QuickRunsTolerated,
			),
		}
		if strings.Join(outcomes, "\n") != strings.Join(want, "\n") {
			t.Errorf("got\n%s\nwant\n%s", strings.Join(outcomes, "\n"), strings.Join(want, "\n"))
		}

		requireNoConclusion(t, manager)
		if got := runner.startCount(); got != QuickRunsTolerated+1 {
			t.Errorf("got %d starts, want %d", got, QuickRunsTolerated+1)
		}
	})
}

func TestARespawnThatCannotStartSaysSo(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runner := &scriptedRunner{runs: []scriptedRun{
			{lasts: time.Minute},
			{refusal: errors.New("no namespaces left")},
		}}
		manager := New(runner)
		defer func() { _ = manager.Close() }()

		if _, err := manager.StartRespawning(t.Context(), "watch", t.TempDir(), "inotifywait .", sandbox.Policy{}); err != nil {
			t.Fatal(err)
		}

		time.Sleep(time.Minute)

		if got, want := takeConclusion(t, manager).Snapshot.Outcome(), "complete after 1m, run 1, respawned"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if got, want := takeConclusion(t, manager).Snapshot.Outcome(),
			"failed after 0s, run 2, the job could not be respawned: no namespaces left"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestAWaitOnARespawningJobSeesTheRunThatEnded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runner := &scriptedRunner{runs: []scriptedRun{{lasts: 5 * time.Second, printed: "changed: a.go\n"}}}
		manager := New(runner)
		defer func() { _ = manager.Close() }()

		if _, err := manager.StartRespawning(t.Context(), "watch", t.TempDir(), "inotifywait .", sandbox.Policy{}); err != nil {
			t.Fatal(err)
		}

		name, err := manager.Wait(t.Context(), []string{"watch"})
		if err != nil {
			t.Fatal(err)
		}
		if name != "watch" {
			t.Errorf("got %q, want the job whose run ended", name)
		}

		output, snapshot, err := manager.Ended("watch")
		if err != nil {
			t.Fatal(err)
		}
		if got, want := snapshot.Describe(), "watch: complete after 5s, run 1, respawned"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if output != "changed: a.go\n" {
			t.Errorf("got %q, want the ended run's output", output)
		}

		requireNoConclusion(t, manager)
	})
}

func TestClosingTheSessionNeverRespawnsAJob(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runner := &scriptedRunner{}
		manager := New(runner)

		if _, err := manager.StartRespawning(t.Context(), "watch", t.TempDir(), "inotifywait .", sandbox.Policy{}); err != nil {
			t.Fatal(err)
		}

		if err := manager.Close(); err != nil {
			t.Fatal(err)
		}

		if got := runner.startCount(); got != 1 {
			t.Errorf("got %d starts, want none after the session closed", got)
		}
	})
}

func TestARestoredRespawningJobNoLongerRespawns(t *testing.T) {
	manager := New(nil)
	manager.Restore([]Snapshot{{Name: "watch", Command: "inotifywait .", State: StateRunning, Run: 4, Respawn: RespawnOnExit}})

	snapshot, err := manager.Status("watch")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := snapshot.Outcome(), "ended with the session, run 4"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
