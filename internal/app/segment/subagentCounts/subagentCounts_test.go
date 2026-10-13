package subagentCounts

import (
	"testing"
	"time"

	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/subagentrecord"
	"crdx.org/oh/internal/app/subagents"
)

func TestFinishedChildrenNeitherRenderNorScheduleRefresh(t *testing.T) {
	at := time.Date(2026, time.March, 18, 12, 0, 0, 0, time.UTC)
	for _, outcome := range []subagentrecord.State{subagentrecord.Done, subagentrecord.Stopped, subagentrecord.Ended} {
		t.Run(string(outcome), func(t *testing.T) {
			instance, err := New(func() []subagents.Snapshot {
				return []subagents.Snapshot{{Name: "tame-adder", State: outcome, EndedAt: at}}
			}, func() time.Time { return at })(nil)
			if err != nil {
				t.Fatal(err)
			}
			refresher, isRefresher := instance.(segment.Refresher)
			if !isRefresher {
				t.Fatal("subagent segment has no expiry refresh")
			}
			if drawn := instance.Render(segment.Context{}); drawn != "" {
				t.Fatalf("finished child still renders %q", drawn)
			}
			if due := refresher.NextRefresh(segment.Phase{At: at}); !due.IsZero() {
				t.Fatalf("hidden child still asks to refresh at %s", due)
			}
		})
	}
}

func TestFailureWarningExpires(t *testing.T) {
	at := time.Date(2026, time.March, 18, 12, 0, 0, 0, time.UTC)
	now := at
	instance, err := New(func() []subagents.Snapshot {
		return []subagents.Snapshot{{Name: "tame-adder", State: subagentrecord.Failed, EndedAt: at}}
	}, func() time.Time { return now })(nil)
	if err != nil {
		t.Fatal(err)
	}
	refresher, isRefresher := instance.(segment.Refresher)
	if !isRefresher {
		t.Fatal("subagent segment has no expiry refresh")
	}
	if drawn := instance.Render(segment.Context{}); drawn == "" {
		t.Fatal("failure warning is hidden")
	}
	if due := refresher.NextRefresh(segment.Phase{At: now}); !due.Equal(at.Add(ghostLife)) {
		t.Fatalf("failure warning expires at %s, want %s", due, at.Add(ghostLife))
	}
	now = at.Add(ghostLife)
	if drawn := instance.Render(segment.Context{}); drawn != "" {
		t.Fatalf("expired failure warning still renders %q", drawn)
	}
	if due := refresher.NextRefresh(segment.Phase{At: now}); !due.IsZero() {
		t.Fatalf("expired failure warning still asks to refresh at %s", due)
	}
}

func TestFlashExpiresWithoutHidingTheSubagent(t *testing.T) {
	at := time.Date(2026, time.March, 18, 12, 0, 0, 0, time.UTC)
	now := at
	get := func() []subagents.Snapshot {
		return []subagents.Snapshot{{Name: "tame-adder", State: subagentrecord.Running, ActivityAt: at}}
	}
	instance, err := New(get, func() time.Time { return now })(nil)
	if err != nil {
		t.Fatal(err)
	}
	refresher, isRefresher := instance.(segment.Refresher)
	if !isRefresher {
		t.Fatal("subagent segment has no expiry refresh")
	}
	if due := refresher.NextRefresh(segment.Phase{At: now}); !due.Equal(at.Add(subagents.ActivityFlash)) {
		t.Fatalf("flash expires at %s, want %s", due, at.Add(subagents.ActivityFlash))
	}
	before := instance.Render(segment.Context{})
	now = at.Add(subagents.ActivityFlash)
	after := instance.Render(segment.Context{})
	if before == after || after == "" {
		t.Fatalf("flash did not return to an ordinary emoji: %q then %q", before, after)
	}
	if due := refresher.NextRefresh(segment.Phase{At: now}); !due.IsZero() {
		t.Fatalf("expired flash still asks to refresh at %s", due)
	}
}
