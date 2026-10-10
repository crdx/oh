package harness

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/sim"
)

func TestAJobRunsOnTheHostOutsideTheSandbox(t *testing.T) {
	start := sim.Call{
		Name:      "job",
		Arguments: `{"action":"start","name":"check","command":"echo from-the-$((1+1))-host","intent":"Checking a job runs on the host"}`,
	}
	rig, _ := newRespondingRig(t, func(request sim.Request) sim.Turn {
		switch {
		case mentions(request, "from-the-2-host"):
			return sim.Turn{Say: "The job ran."}
		case hasCallOutput(request):
			return sim.Turn{Say: "Started."}
		}
		return sim.Turn{Calls: []sim.Call{start}}
	})
	session := rig.start("--yolo", "-m", "opencode-go/fake", "run a job")
	session.waitFor("The job ran.")
	session.quit()

	storedSessions := rig.storedSessions()
	if len(storedSessions) != 1 {
		t.Fatalf("got %d stored sessions, want one", len(storedSessions))
	}
	journal, err := os.ReadFile(filepath.Join(
		rig.stateDirectory, "org.crdx", "oh", "sessions", storedSessions[0].Name, "session.jsonl",
	))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(journal), "from-the-2-host") {
		t.Errorf("the job's output never reached the journal:\n%s", journal)
	}
	if strings.Contains(string(journal), "unknown tool") {
		t.Errorf("the job tool was not offered under --yolo:\n%s", journal)
	}
}

func TestAYoloSessionOnTheDefaultCapsIsToldItsWorkspaceIsWritable(t *testing.T) {
	rig := newInteractiveRig(t, "Noted.")
	runTestBinary(t, rig.binary, rig.workspace, rig.environment, "-p", "--yolo", "-m", "opencode-go/fake", "can you edit files?")

	storedSessions := rig.storedSessions()
	if len(storedSessions) != 1 {
		t.Fatalf("got %d stored sessions, want one", len(storedSessions))
	}
	systemPrompt := storedSessions[0].Meta.SystemPrompt

	if !strings.Contains(systemPrompt, "- The workspace ("+rig.workspace+") is read-write") {
		t.Errorf("the workspace is not described as writable:\n%s", systemPrompt)
	}
	for _, unwanted := range []string{"Read-only Workspaces", "ctrl+x w", "ctrl+x x", "ctrl+x n", "ctrl+x g"} {
		if strings.Contains(systemPrompt, unwanted) {
			t.Errorf("the system prompt mentions %q under --yolo:\n%s", unwanted, systemPrompt)
		}
	}
}

func TestAYoloSessionIgnoresTheKeysForWhatItLeavesOpen(t *testing.T) {
	harness := &App{mode: caps.NewMode(caps.Read | caps.Shell)}
	harness.mode.Unconfine()

	for _, forced := range []caps.Set{caps.Read, caps.Shell, caps.Write, caps.Network, caps.Git} {
		harness.toggleCap(forced)
		if got := harness.mode.Current(); got != caps.Unconfined() {
			t.Errorf("toggling %s left %s, want %s", forced.Flag(), got.Flags(), caps.Unconfined().Flags())
		}
	}
	if harness.hasUntoldPendingNotices() {
		t.Error("an ignored key queued a mode change")
	}
}

func TestAYoloSessionLeftConfinedIsToldWhatItWasGranted(t *testing.T) {
	harness := &App{
		mode:   caps.NewMode(caps.Read | caps.Shell | caps.Lookup),
		screen: output.New(&bytes.Buffer{}),
	}

	harness.unconfine()

	if got, want := harness.mode.Current(), caps.Unconfined()|caps.Lookup; got != want {
		t.Errorf("got %s, want %s", got.Flags(), want.Flags())
	}
	told := strings.Join(noteTextsOf(harness.pendingNotices.modelNotes()), "\n")
	for _, wanted := range []string{
		"The workspace is now read-write.",
		".git is now read-write.",
		"Fetch can now access the internet.",
	} {
		if !strings.Contains(told, wanted) {
			t.Errorf("the model was told %q, want it to contain %q", told, wanted)
		}
	}
	for _, unwanted := range []string{"host network", "Bash is now"} {
		if strings.Contains(told, unwanted) {
			t.Errorf("the model was told %q, which mentions %q", told, unwanted)
		}
	}
}
