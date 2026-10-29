package tasks_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/tool"
)

func TestARunningForegroundCallDetachesAfterTheSetForegroundLimit(t *testing.T) {
	for _, test := range []struct {
		name  string
		limit time.Duration
		want  time.Duration
	}{
		{"default", 0, 5 * time.Second},
		{"shorter", time.Second, time.Second},
		{"longer", 2 * time.Minute, 2 * time.Minute},
		{"negative", -time.Second, tasks.ForegroundLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				manager := tasks.New(t.Context())
				defer manager.Close()
				manager.SetForegroundLimit(test.limit)
				if got := manager.ForegroundLimit(); got != test.want {
					t.Errorf("the manager reports a foreground limit of %s, want %s", got, test.want)
				}
				snapshot, err := manager.Start(t.Context(), "", "lookup", false, false, holdUntilStopped, nil)
				if err != nil {
					t.Fatal(err)
				}
				synctest.Wait()
				started := time.Now()
				conclusion, isBackground, err := manager.WaitOrDetach(t.Context(), snapshot.ID)
				if err != nil || !isBackground {
					t.Fatalf("a running foreground call returned background=%t, %v", isBackground, err)
				}
				if waited := time.Since(started); waited != test.want {
					t.Errorf("the call detached after %s, want %s", waited, test.want)
				}
				select {
				case <-manager.Changes():
				default:
					t.Fatal("the detached lookup did not request a redraw")
				}
				listing := manager.List()
				if len(listing) != 1 || listing[0].ID != conclusion.Snapshot.ID || listing[0].ToolName != "lookup" || listing[0].State != tasks.Running {
					t.Errorf("the detached lookup is missing from the task listing: %+v", listing)
				}
			})
		})
	}
}

func TestAQueuedForegroundCallDetachesAfterTheSetForegroundLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		manager.SetForegroundLimit(5 * time.Second)
		slots := make(chan struct{}, 1)
		slots <- struct{}{}
		snapshot, err := manager.Start(t.Context(), "", "offload", false, false, waitForSlot(slots, time.Second), nil)
		if err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		conclusion, isBackground, err := manager.WaitOrDetach(t.Context(), snapshot.ID)
		if err != nil || !isBackground {
			t.Fatalf("a queued foreground call returned background=%t, %v", isBackground, err)
		}
		if waited := time.Since(started); waited != 5*time.Second {
			t.Errorf("the call detached after %s, want 5s", waited)
		}
		select {
		case <-manager.Changes():
		default:
			t.Fatal("the detached queued call did not request a redraw")
		}
		listing := manager.List()
		if len(listing) != 1 || listing[0].ID != conclusion.Snapshot.ID || listing[0].ToolName != "offload" || listing[0].State != tasks.Queued {
			t.Errorf("the detached queued call is missing from the task listing: %+v", listing)
		}
	})
}

func TestChangingTheForegroundLimitOnlyChangesSubsequentWaits(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		manager.SetForegroundLimit(10 * time.Second)
		snapshot, err := manager.Start(t.Context(), "first", "bash", false, false, holdUntilStopped, nil)
		if err != nil {
			t.Fatal(err)
		}
		detached := make(chan time.Duration, 1)
		started := time.Now()
		go func() {
			_, isBackground, failure := manager.WaitOrDetach(t.Context(), snapshot.ID)
			if failure != nil || !isBackground {
				t.Errorf("first wait returned %t, %v", isBackground, failure)
			}
			detached <- time.Since(started)
		}()
		synctest.Wait()
		manager.SetForegroundLimit(time.Second)
		if got := <-detached; got != 10*time.Second {
			t.Errorf("existing wait detached after %s", got)
		}
		second, err := manager.Start(t.Context(), "second", "bash", false, false, holdUntilStopped, nil)
		if err != nil {
			t.Fatal(err)
		}
		started = time.Now()
		_, isBackground, err := manager.WaitOrDetach(t.Context(), second.ID)
		if err != nil || !isBackground || time.Since(started) != time.Second {
			t.Errorf("new wait returned %t, %v after %s", isBackground, err, time.Since(started))
		}
	})
}

func TestDetachedCallsSkipReservedAndChosenBackgroundNames(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		manager.Reserve([]string{"bash-4"})
		if _, err := manager.Start(t.Context(), "", "bash", true, false, holdUntilStopped, nil, tasks.StartOptions{Name: "bash-5"}); err != nil {
			t.Fatal(err)
		}
		first, err := manager.Start(t.Context(), "", "bash", false, false, holdUntilStopped, nil)
		if err != nil {
			t.Fatal(err)
		}
		second, err := manager.Start(t.Context(), "", "bash", false, false, holdUntilStopped, nil)
		if err != nil {
			t.Fatal(err)
		}
		for index, snapshot := range []tasks.Snapshot{second, first} {
			conclusion, isBackground, err := manager.WaitOrDetach(t.Context(), snapshot.ID)
			want := []string{"bash-6", "bash-7"}[index]
			if err != nil || !isBackground || conclusion.Snapshot.ID != want {
				t.Fatalf("detachment returned %+v, %t, %v; want %s", conclusion, isBackground, err, want)
			}
			if _, err := manager.Status(want); err != nil {
				t.Errorf("cannot control detached task %s: %v", want, err)
			}
		}
	})
}

func TestAForegroundWaitKeepsItsEntryAfterAnotherWaitDetachesIt(t *testing.T) {
	for _, shouldCancel := range []bool{false, true} {
		name := "completed"
		if shouldCancel {
			name = "cancelled"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				manager := tasks.New(t.Context())
				defer manager.Close()
				release := make(chan struct{})
				snapshot, err := manager.Start(t.Context(), "", "bash", false, false,
					func(ctx context.Context) (tool.ToolCallResult, error) {
						select {
						case <-release:
							return tool.ToolCallResult{Output: "done"}, nil
						case <-ctx.Done():
							return tool.ToolCallResult{}, ctx.Err()
						}
					}, nil)
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				waited := make(chan error, 1)
				go func() {
					conclusion, isBackground, err := manager.WaitOrDetach(ctx, snapshot.ID)
					if isBackground || !shouldCancel && (err != nil || conclusion.Snapshot.ID != "bash-1" || conclusion.Result.Output != "done") {
						t.Errorf("first waiter returned %+v, %t, %v", conclusion, isBackground, err)
					}
					waited <- err
				}()
				synctest.Wait()
				manager.SetForegroundLimit(time.Second)
				conclusion, isBackground, err := manager.WaitOrDetach(t.Context(), snapshot.ID)
				if err != nil || !isBackground || conclusion.Snapshot.ID != "bash-1" {
					t.Fatalf("second waiter returned %+v, %t, %v", conclusion, isBackground, err)
				}
				if shouldCancel {
					cancel()
				} else {
					close(release)
				}
				err = <-waited
				if shouldCancel && !errors.Is(err, context.Canceled) {
					t.Errorf("cancelled waiter returned %v", err)
				}
			})
		})
	}
}
