package subagentCounts

import (
	"testing"
	"time"

	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/subagentrecord"
	"crdx.org/oh/internal/app/subagents"
)

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
