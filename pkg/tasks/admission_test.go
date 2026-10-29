package tasks_test

import (
	"context"
	"errors"
	"fmt"
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

func TestRejectedBackgroundCallsDoNotConsumeGeneratedNames(t *testing.T) {
	for _, reason := range []string{"full queue", "policy refusal", "policy cancellation"} {
		t.Run(reason, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				manager := tasks.NewWithMaximumRunning(t.Context(), 1)
				defer manager.Close()
				if reason == "full queue" {
					if _, err := manager.Start(t.Context(), "active", "bash", true, false, holdUntilStopped, nil); err != nil {
						t.Fatal(err)
					}
					synctest.Wait()
					for index := range tasks.MaxQueued {
						if _, err := manager.Start(t.Context(), fmt.Sprintf("queued-%d", index), "bash", true, false, holdUntilStopped, nil); err != nil {
							t.Fatal(err)
						}
					}
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				refusal := errors.New("access refused")
				policy := tasks.PolicySource(func() (any, error) {
					if reason == "full queue" {
						t.Error("a full queue evaluated the policy")
					}
					if reason == "policy cancellation" {
						cancel()
						return "read-only", nil
					}
					return nil, refusal
				})
				_, err := manager.Start(ctx, "", "bash", true, false, holdUntilStopped, policy)
				if err == nil {
					t.Fatal("the background start was not rejected")
				}
				if reason == "policy refusal" && !errors.Is(err, refusal) {
					t.Errorf("policy refusal returned %v", err)
				}
				if reason == "policy cancellation" && !errors.Is(err, context.Canceled) {
					t.Errorf("policy cancellation returned %v", err)
				}
				manager.StopWhere(func(string, any) bool { return true }, "clear the queue")
				synctest.Wait()
				snapshot, err := manager.Start(t.Context(), "", "bash", true, false, holdUntilStopped, nil)
				if err != nil || snapshot.ID != "bash-1" {
					t.Errorf("the first admitted generated task is %+v, %v; want bash-1", snapshot, err)
				}
			})
		})
	}
}
