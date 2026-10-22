package tasks_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/tool"
)

func waitForSlot(slots chan struct{}, work time.Duration) tasks.Execution {
	return func(ctx context.Context) (tool.ToolCallResult, error) {
		if err := tool.TakeSlot(ctx, slots); err != nil {
			return tool.ToolCallResult{}, err
		}
		defer func() { <-slots }()
		time.Sleep(work)
		return tool.ToolCallResult{Output: "ran"}, nil
	}
}

func stateOf(t *testing.T, manager *tasks.Manager, id string) tasks.State {
	t.Helper()
	snapshot, err := manager.Status(id)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot.State
}

func TestATaskWaitingForAToolSlotIsQueued(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		slots := make(chan struct{}, 1)
		slots <- struct{}{}
		snapshot, err := manager.Start(t.Context(), "third", "offload", true, false, waitForSlot(slots, time.Minute), nil)
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if state := stateOf(t, manager, snapshot.ID); state != tasks.Queued {
			t.Errorf("a task waiting for a slot is %s, want queued", state)
		}
		<-slots
		synctest.Wait()
		if state := stateOf(t, manager, snapshot.ID); state != tasks.Running {
			t.Errorf("a task holding its slot is %s, want running", state)
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		if state := stateOf(t, manager, snapshot.ID); state != tasks.Done {
			t.Errorf("a finished task is %s, want done", state)
		}
	})
}

func TestTimeWaitingForAToolSlotIsExcludedFromTheRunLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		slots := make(chan struct{}, 1)
		slots <- struct{}{}
		snapshot, err := manager.Start(t.Context(), "late", "offload", true, false, waitForSlot(slots, tasks.RunLimit-time.Minute), nil)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(tasks.RunLimit)
		<-slots
		time.Sleep(tasks.RunLimit)
		synctest.Wait()
		if state := stateOf(t, manager, snapshot.ID); state != tasks.Done {
			t.Errorf("a task that waited for a slot is %s, want done", state)
		}
	})
}

func TestStoppingATaskWaitingForAToolSlotStopsIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		slots := make(chan struct{}, 1)
		slots <- struct{}{}
		snapshot, err := manager.Start(t.Context(), "waiting", "offload", true, false, waitForSlot(slots, time.Minute), nil)
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if err := manager.Stop(snapshot.ID); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if state := stateOf(t, manager, snapshot.ID); state != tasks.Stopped {
			t.Errorf("a stopped task is %s, want stopped", state)
		}
		if len(slots) != 1 {
			t.Errorf("the stopped task changed the slots to %d taken", len(slots))
		}
	})
}

func TestAForegroundCallWaitingForAToolSlotDetachesAfterTheForegroundLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		slots := make(chan struct{}, 1)
		slots <- struct{}{}
		snapshot, err := manager.Start(t.Context(), "", "offload", false, false, waitForSlot(slots, time.Second), nil)
		if err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		_, isBackground, err := manager.WaitOrDetach(t.Context(), snapshot.ID)
		if err != nil || !isBackground {
			t.Fatalf("a foreground call waiting for a slot returned background=%t, %v", isBackground, err)
		}
		if waited := time.Since(started); waited != tasks.ForegroundLimit {
			t.Errorf("the call detached after %s, want %s", waited, tasks.ForegroundLimit)
		}
		if state := stateOf(t, manager, snapshot.ID); state != tasks.Queued {
			t.Errorf("the detached call is %s, want queued", state)
		}
	})
}
