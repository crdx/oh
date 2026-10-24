package tasks_test

import (
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/pkg/tasks"
)

func TestARunningForegroundCallDetachesAfterTheSetForegroundLimit(t *testing.T) {
	for _, test := range []struct {
		name  string
		limit time.Duration
		want  time.Duration
	}{
		{"default", 0, tasks.ForegroundLimit},
		{"shorter", 5 * time.Second, 5 * time.Second},
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
				_, isBackground, err := manager.WaitOrDetach(t.Context(), snapshot.ID)
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
				if len(listing) != 1 || listing[0].ID != snapshot.ID || listing[0].ToolName != "lookup" || listing[0].State != tasks.Running {
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
		_, isBackground, err := manager.WaitOrDetach(t.Context(), snapshot.ID)
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
		if len(listing) != 1 || listing[0].ID != snapshot.ID || listing[0].ToolName != "offload" || listing[0].State != tasks.Queued {
			t.Errorf("the detached queued call is missing from the task listing: %+v", listing)
		}
	})
}
