package harness

import (
	"slices"
	"testing"

	"crdx.org/oh/internal/app/toolset"
	"crdx.org/oh/internal/sim"
)

func TestAHeadlessSessionOffersOnlyTheToolsThatWorkAlone(t *testing.T) {
	rig := newInteractiveRig(t, "Noted.")
	writeRigConfig(t, rig, "[experimental]\nwait_tool = true\n")
	runTestBinary(t, rig.binary, rig.workspace, rig.environment, "-p", "--yolo", "-m", "opencode-go/fake", "which tools do you have?")

	storedSessions := rig.storedSessions()
	if len(storedSessions) != 1 {
		t.Fatalf("got %d stored sessions, want one", len(storedSessions))
	}
	if got := storedSessions[0].Meta.Tools; !slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(toolset.HeadlessTools))) {
		t.Errorf("a headless session offered %v, want %v", got, toolset.HeadlessTools)
	}
}

func TestAnInteractiveSessionResumedHeadlessKeepsItsToolsButDisablesWhatNeedsWaking(t *testing.T) {
	rig := newScriptedRig(t, sim.Turn{Say: "First."}, sim.Turn{Say: "Second."})
	session := rig.start("--yolo", "-m", "opencode-go/fake", "hello")
	session.waitFor("First.")
	session.quit()

	storedSessions := rig.storedSessions()
	if len(storedSessions) != 1 {
		t.Fatalf("got %d stored sessions, want one", len(storedSessions))
	}
	if !slices.Contains(storedSessions[0].Meta.Tools, "job") {
		t.Fatalf("the interactive session offered %v, want job among them", storedSessions[0].Meta.Tools)
	}

	runTestBinary(t, rig.binary, rig.workspace, rig.environment, "-p", "-r", storedSessions[0].Name, "carry on")

	resumed := rig.storedSessions()[0]
	if !slices.Equal(resumed.Meta.Tools, storedSessions[0].Meta.Tools) {
		t.Errorf("resuming headless changed the tools to %v", resumed.Meta.Tools)
	}
	availability, isRecorded := toolset.LastRecordedAvailability(resumed.Events)
	if !isRecorded {
		t.Fatal("resuming headless recorded no change of availability")
	}
	for _, name := range resumed.Meta.Tools {
		want := toolset.ToolWithheld
		if slices.Contains(toolset.HeadlessTools, name) {
			want = toolset.ToolAvailable
		}
		if got := availability[name]; got != want {
			t.Errorf("%s is %q headless, want %q", name, got, want)
		}
	}
}
