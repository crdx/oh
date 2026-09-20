package jobs

import (
	"testing"

	"crdx.org/oh/internal/sandbox"
)

func TestAStopDuringTheStartWindowIsNotClobbered(t *testing.T) {
	manager := New(nil)

	opening, err := manager.claim("docs", "python3", sandbox.Policy{})
	if err != nil {
		t.Fatal(err)
	}

	manager.beginEnd(opening)

	manager.mutex.Lock()
	opening.runningCommand = nil
	manager.mutex.Unlock()
	if manager.settleStarted(opening, nil) {
		t.Error("a job stopped while starting reported itself as freshly running")
	}

	if snapshot := manager.snapshot(opening); snapshot.State == StateRunning {
		t.Errorf("got state %s, want the stop to survive the start finishing", snapshot.State)
	}
}
