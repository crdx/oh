package toolset

import (
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/pkg/agent"
)

func TestToolAvailabilityChangesAreAnnouncedOnce(t *testing.T) {
	current := Availability{
		"changed": ToolChanged,
		"missing": ToolMissing,
		"steady":  ToolAvailable,
	}
	restored, err := RestoreAvailability(nil, current)
	if err != nil {
		t.Fatal(err)
	}
	if !restored.IsChanged {
		t.Fatal("the changed tools were not announced")
	}
	notices, isShown := AvailabilityNotice(restored.Change)
	if !isShown || len(notices) != 2 {
		t.Fatalf("got notices %q and shown %v", notices, isShown)
	}
	if !strings.Contains(notices[0], "`changed`") || !strings.Contains(notices[0], "changed since") {
		t.Errorf("got changed notice %q", notices[0])
	}
	if !strings.Contains(notices[1], "`missing`") || !strings.Contains(notices[1], "no longer installed") {
		t.Errorf("got missing notice %q", notices[1])
	}

	again, err := RestoreAvailability([]agent.Event{restored.Change}, current)
	if err != nil {
		t.Fatal(err)
	}
	if again.IsChanged {
		t.Error("unchanged availability was announced again")
	}
}

func TestACompatibleToolReturningIsAnnounced(t *testing.T) {
	missing := Availability{"weather": ToolMissing}
	first, err := AvailabilityChangeEvent(Availability{"weather": ToolAvailable}, missing)
	if err != nil {
		t.Fatal(err)
	}

	restored, err := RestoreAvailability([]agent.Event{first}, Availability{"weather": ToolAvailable})
	if err != nil {
		t.Fatal(err)
	}
	notices, isShown := AvailabilityNotice(restored.Change)
	if !restored.IsChanged || !isShown || !slices.Equal(notices, []string{"The `weather` tool is available again."}) {
		t.Errorf("got notices %q, changed %v and shown %v", notices, restored.IsChanged, isShown)
	}
}
