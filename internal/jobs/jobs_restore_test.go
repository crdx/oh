package jobs

import (
	"errors"
	"testing"
	"time"

	"crdx.org/oh/internal/sandbox"
)

func TestARestoredJobIsNoLongerLive(t *testing.T) {
	manager := New(nil)
	manager.Restore([]Snapshot{
		{Name: "docs", Command: "python3 -m http.server", State: StateRunning},
		{Name: "build", Command: "just build", State: StateFailed, EndedAt: time.Now()},
	})

	docs, err := manager.Status("docs")
	if err != nil {
		t.Fatalf("the restored job was not found: %v", err)
	}
	if docs.IsLive() {
		t.Errorf("got state %s, want a restored job to be dead", docs.State)
	}
	if docs.State != StateEnded {
		t.Errorf("got state %s, want it to say it ended with the session", docs.State)
	}
}

func TestARestoredJobKeepsItsFinishedState(t *testing.T) {
	manager := New(nil)
	manager.Restore([]Snapshot{{Name: "build", Command: "just build", State: StateFailed}})

	build, err := manager.Status("build")
	if err != nil {
		t.Fatalf("the restored job was not found: %v", err)
	}
	if build.State != StateFailed {
		t.Errorf("got state %s, want the failure it actually ended in", build.State)
	}
}

func TestARestoredJobIsListedButNotLive(t *testing.T) {
	manager := New(nil)
	manager.Restore([]Snapshot{{Name: "docs", State: StateRunning}})

	if listing := manager.List(); len(listing) != 1 || listing[0].IsLive() {
		t.Errorf("got %#v, want a restored job to be listed but not live", listing)
	}
}

func TestARestoredJobRemembersItsCommand(t *testing.T) {
	manager := New(nil)
	manager.Restore([]Snapshot{{Name: "docs", Command: "python3 -m http.server", State: StateRunning}})

	command, isRemembered := manager.RememberedCommand("docs")
	if !isRemembered || command != "python3 -m http.server" {
		t.Errorf("got %q %v, want the command it was started with", command, isRemembered)
	}
}

func TestAnUnknownJobRemembersNoCommand(t *testing.T) {
	manager := New(nil)

	if _, isRemembered := manager.RememberedCommand("ghost"); isRemembered {
		t.Error("an unknown job remembered a command")
	}
}

func TestRestoringNeverDisplacesALiveJob(t *testing.T) {
	manager := New(nil)
	if _, err := manager.claim("docs", "the live one", sandbox.Policy{}); err != nil {
		t.Fatal(err)
	}
	manager.Restore([]Snapshot{{Name: "docs", Command: "the remembered one", State: StateRunning}})

	if command, _ := manager.RememberedCommand("docs"); command != "the live one" {
		t.Errorf("got %q, want the live job left alone", command)
	}
}

func TestAFinishedJobCanBeDiscarded(t *testing.T) {
	manager := New(nil)
	manager.Restore([]Snapshot{{Name: "build", Command: "just build", State: StateFailed}})

	if _, err := manager.Discard("build"); err != nil {
		t.Fatalf("a finished job could not be discarded: %v", err)
	}
	if _, err := manager.Status("build"); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want the job gone from the listing", err)
	}
	if len(manager.List()) != 0 {
		t.Errorf("got %#v, want an empty listing", manager.List())
	}
}

func TestDiscardingALiveJobStopsItFirst(t *testing.T) {
	manager := New(nil)
	if _, err := manager.claim("docs", "python3", sandbox.Policy{}); err != nil {
		t.Fatal(err)
	}

	discarded, err := manager.Discard("docs")
	if err != nil {
		t.Fatalf("a live job could not be discarded: %v", err)
	}
	if discarded.IsLive() {
		t.Errorf("got state %s, want the job stopped before it was discarded", discarded.State)
	}
	if len(manager.List()) != 0 {
		t.Errorf("got %#v, want the discarded job gone from the listing", manager.List())
	}
}

func TestDiscardingAnUnknownJobSaysSo(t *testing.T) {
	if _, err := New(nil).Discard("ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want the job not to be found", err)
	}
}

func TestFinishedJobsAreSweptButLiveOnesAreLeft(t *testing.T) {
	manager := New(nil)
	manager.Restore([]Snapshot{
		{Name: "build", State: StateFailed},
		{Name: "old", State: StateComplete},
	})
	if _, err := manager.claim("docs", "python3", sandbox.Policy{}); err != nil {
		t.Fatal(err)
	}

	forgotten := manager.PruneFinished()
	if len(forgotten) != 2 {
		t.Errorf("got %v, want both finished jobs swept", forgotten)
	}
	if listing := manager.List(); len(listing) != 1 || listing[0].Name != "docs" {
		t.Errorf("got %#v, want the live job left behind", listing)
	}
}

func TestAForgottenNameIsFreeAgain(t *testing.T) {
	manager := New(nil)
	manager.Restore([]Snapshot{{Name: "docs", Command: "old command", State: StateFailed}})

	if _, err := manager.Discard("docs"); err != nil {
		t.Fatal(err)
	}
	if _, isRemembered := manager.RememberedCommand("docs"); isRemembered {
		t.Error("a forgotten job still remembered its command")
	}
}
