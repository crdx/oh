package harness

import (
	"bytes"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/output"
)

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
