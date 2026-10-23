package tasks_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/pkg/tasks"
)

func buildAfter(t *testing.T, manager *tasks.Manager, wait time.Duration) tasks.Snapshot {
	t.Helper()

	time.Sleep(wait)
	synctest.Wait()
	for _, snapshot := range manager.List() {
		if snapshot.ID == "build" {
			return snapshot
		}
	}
	t.Fatal("there is no task named build")
	return tasks.Snapshot{}
}

func TestATaskWithoutALimitRunsUntilItIsStopped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		if _, err := manager.Start(t.Context(), "build", "offload", true, false, holdUntilStopped, nil); err != nil {
			t.Fatal(err)
		}

		if snapshot := buildAfter(t, manager, time.Hour); snapshot.State != tasks.Running {
			t.Fatalf("the task was %s after an hour", snapshot.State)
		}
		if err := manager.Stop("build", "the user stopped it"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestATaskHonoursItsExplicitLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		limit := 10 * time.Minute
		if _, err := manager.Start(t.Context(), "build", "offload", true, false, holdUntilStopped, nil,
			tasks.StartOptions{RunLimit: limit}); err != nil {
			t.Fatal(err)
		}

		if snapshot := buildAfter(t, manager, limit-time.Second); snapshot.State != tasks.Running {
			t.Fatalf("the task was %s a second before its limit of %s", snapshot.State, limit)
		}
		if snapshot := buildAfter(t, manager, 2*time.Second); snapshot.State != tasks.TimedOut {
			t.Errorf("the task was %s a second after its limit of %s", snapshot.State, limit)
		}
	})
}

func TestARestartKeepsTheLimitItsPlanGives(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		manager.BindTool("offload", func(context.Context, tasks.Invocation) (tasks.PreparationPlan, error) {
			return tasks.PreparationPlan{Execute: holdUntilStopped, RunLimit: 45 * time.Minute}, nil
		})
		if _, err := manager.Start(t.Context(), "build", "offload", true, false, holdUntilStopped, nil,
			tasks.StartOptions{RunLimit: 45 * time.Minute, Invocation: &tasks.Invocation{Arguments: `{}`}}); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if _, err := manager.Restart(t.Context(), "build"); err != nil {
			t.Fatal(err)
		}

		if snapshot := buildAfter(t, manager, 44*time.Minute); snapshot.State != tasks.Running || snapshot.Run != 2 {
			t.Fatalf("the restarted task was %s on run %d after 44 minutes", snapshot.State, snapshot.Run)
		}
		if snapshot := buildAfter(t, manager, 2*time.Minute); snapshot.State != tasks.TimedOut {
			t.Errorf("the restarted task was %s after 46 minutes, want it stopped at its limit", snapshot.State)
		}
	})
}
