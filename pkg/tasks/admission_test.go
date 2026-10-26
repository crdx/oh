package tasks_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"

	"crdx.org/oh/pkg/tasks"
)

func TestLoweringTheTaskLimitKeepsRunningTasksAndBlocksNewAdmissions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.NewWithMaximumRunning(t.Context(), 3)
		defer manager.Close()
		for _, name := range []string{"one", "two", "three"} {
			if _, err := manager.Start(t.Context(), name, "bash", true, false, holdUntilStopped, nil); err != nil {
				t.Fatal(err)
			}
		}
		synctest.Wait()
		manager.SetMaximumRunning(1)
		if _, err := manager.Start(t.Context(), "four", "bash", true, false, holdUntilStopped, nil); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		for _, name := range []string{"one", "two", "three"} {
			if got := stateOf(t, manager, name); got != tasks.Running {
				t.Errorf("%s is %s", name, got)
			}
		}
		for _, name := range []string{"one", "two", "three"} {
			if got := stateOf(t, manager, "four"); got != tasks.Queued {
				t.Fatalf("four is %s before %s stops", got, name)
			}
			if err := manager.Stop(name, "test stop"); err != nil {
				t.Fatal(err)
			}
			synctest.Wait()
		}
		if got := stateOf(t, manager, "four"); got != tasks.Running {
			t.Errorf("four is %s after the earlier tasks stop", got)
		}
	})
}

func TestRaisingTheTaskLimitImmediatelyAdmitsWaitingTasks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.NewWithMaximumRunning(t.Context(), 1)
		defer manager.Close()
		for _, name := range []string{"one", "two", "three"} {
			if _, err := manager.Start(t.Context(), name, "bash", true, false, holdUntilStopped, nil); err != nil {
				t.Fatal(err)
			}
		}
		synctest.Wait()
		manager.SetMaximumRunning(0)
		manager.SetMaximumRunning(-1)
		count := 0
		for _, snapshot := range manager.List() {
			if snapshot.State == tasks.Running {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("invalid limits changed the running count to %d", count)
		}
		manager.SetMaximumRunning(3)
		synctest.Wait()
		for _, name := range []string{"one", "two", "three"} {
			if got := stateOf(t, manager, name); got != tasks.Running {
				t.Errorf("%s is %s", name, got)
			}
		}
	})
}

func TestAStoppedWaiterNeverRunsAfterTheTaskLimitIncreases(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.NewWithMaximumRunning(t.Context(), 1)
		defer manager.Close()
		if _, err := manager.Start(t.Context(), "one", "bash", true, false, holdUntilStopped, nil); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if _, err := manager.Start(t.Context(), "two", "bash", true, false, holdUntilStopped, nil); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if err := manager.Stop("two", "test stop"); err != nil {
			t.Fatal(err)
		}
		manager.SetMaximumRunning(3)
		synctest.Wait()
		if got := stateOf(t, manager, "two"); got != tasks.Stopped {
			t.Errorf("two is %s", got)
		}
		manager.Close()
		if got := stateOf(t, manager, "one"); got != tasks.Stopped {
			t.Errorf("one is %s", got)
		}
	})
}

func TestClosingTheManagerReleasesWaitersAfterLimitChanges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.NewWithMaximumRunning(t.Context(), 1)
		for _, name := range []string{"one", "two", "three"} {
			if _, err := manager.Start(t.Context(), name, "bash", true, false, holdUntilStopped, nil); err != nil {
				t.Fatal(err)
			}
		}
		synctest.Wait()
		var workers sync.WaitGroup
		for range 5 {
			workers.Go(func() {
				for index := range 100 {
					manager.SetMaximumRunning(1 + index%3)
				}
			})
		}
		workers.Wait()
		manager.Close()
		for _, snapshot := range manager.List() {
			if snapshot.State != tasks.Stopped {
				t.Errorf("%s is %s", snapshot.ID, snapshot.State)
			}
		}
		if _, err := manager.Start(context.Background(), "late", "bash", true, false, holdUntilStopped, nil); !errors.Is(err, tasks.ErrClosed) {
			t.Errorf("closed manager returned %v", err)
		}
	})
}
