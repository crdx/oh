package harness

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"crdx.org/oh/internal/sim"
	"crdx.org/oh/pkg/agent"
)

const pressCtrlZ = "\x1a"

func TestTakingBackAStreamingTurnThroughTheBinaryRemovesItFromTheNextRequest(t *testing.T) {
	rig, endpoint := newEndpointRig(t, sim.New(&sim.Scenario{Model: "fake", Turns: []sim.Turn{
		{Say: strings.Repeat("discarded answer ", 100), Pace: sim.Duration{Duration: 50 * time.Millisecond}},
		{Say: "The corrected answer."},
		{Say: "The resumed answer."},
	}}))
	conversation := rig.start("--yolo", "-m", "opencode-go/fake", "a mistaken message")
	if !conversation.WaitForCount("discarded answer", 1, interactiveDeadline) {
		t.Fatalf("the streamed answer did not start:\n%s", conversation.String())
	}
	storedBefore := rig.storedSessions()
	if len(storedBefore) != 1 {
		t.Fatalf("expected one session, got %d", len(storedBefore))
	}
	name := storedBefore[0].Name
	previousDraws := conversation.Count("a mistaken message")
	conversation.typeText(pressCtrlZ)
	conversation.waitForCount("a mistaken message", previousDraws+1)
	conversation.requireShown("a mistaken message")
	conversation.requireHidden("discarded answer")
	if strings.Contains(conversation.String(), "Turn stopped because the user pressed ctrl+z.") {
		t.Fatal("successful undo emitted its internal stop notice")
	}
	conversation.requireHidden("Message taken back")
	conversation.requireHidden("Totals reset")
	if got := len(endpoint.Requests()); got != 1 {
		t.Fatalf("undo submitted the restored draft: %d requests", got)
	}
	storedAfter := rig.storedSessions()
	if len(storedAfter) != 1 || storedAfter[0].Name != name || len(storedAfter[0].Items) != 0 {
		t.Fatalf("undo changed sessions or retained provider history: %+v", storedAfter)
	}
	for _, event := range storedAfter[0].Events {
		if event.Kind == agent.UserMessageEvent || event.Kind == agent.ModelMessageEvent {
			t.Fatalf("the discarded turn remains in the journal: %+v", event)
		}
	}
	conversation.typeText(clearInput + "the corrected message" + pressEnter)
	conversation.waitFor("The corrected answer.")
	requests := endpoint.Requests()
	if len(requests) != 2 {
		t.Fatalf("expected the corrected request, got %d", len(requests))
	}
	for _, entry := range requests[1].Input {
		if strings.Contains(entry.Content, "mistaken message") || strings.Contains(entry.Content, "discarded answer") || strings.Contains(entry.Content, "turn_aborted") {
			t.Fatalf("the provider still received the discarded turn: %+v", entry)
		}
	}
	conversation.quit()
	root, err := os.OpenRoot(filepath.Join(rig.stateDirectory, "org.crdx", "oh", "sessions", name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	transcript, err := root.ReadFile("chat.md")
	if err != nil || strings.Contains(string(transcript), "mistaken message") || !strings.Contains(string(transcript), "corrected message") {
		t.Fatalf("transcript does not match the corrected history: %s, %v", transcript, err)
	}
	resumed := rig.start("-r", name, "carry on")
	resumed.waitFor("The resumed answer.")
	resumed.requireHidden("mistaken message")
	resumed.quit()
}

func TestTakingBackAQueuedMessageThroughTheBinaryDoesNotStopTheTurn(t *testing.T) {
	rig, endpoint, requested, release := newHeldResponseRig(t, "Still running after recall.")
	conversation := rig.start("--yolo", "-m", "opencode-go/fake", "the running question")
	select {
	case <-requested:
	case <-time.After(interactiveDeadline):
		t.Fatal("the provider request never arrived")
	}
	conversation.typeText("queued correction" + pressEnter)
	conversation.waitFor("⏳ queued correction")
	previousDraws := conversation.Count("queued correction")
	conversation.typeText(pressCtrlZ)
	if !conversation.WaitForCount("queued correction", previousDraws+1, interactiveDeadline) {
		t.Fatalf("queued message did not return to the input:\n%s", conversation.String())
	}
	conversation.requireHidden("undoing…")
	release()
	conversation.waitFor("Still running after recall.")
	conversation.requireShown("queued correction")
	conversation.requireHidden("Turn stopped")
	if len(endpoint.Requests()) != 1 {
		t.Fatal("queued recall started another request")
	}
	conversation.typeText(clearInput)
	conversation.quit()
}

func newHeldResponseRig(t *testing.T, answer string) (*interactiveRig, *sim.Endpoint, <-chan struct{}, func()) {
	t.Helper()
	requested := make(chan struct{})
	released := make(chan struct{})
	announceRequest := sync.OnceFunc(func() { close(requested) })
	release := sync.OnceFunc(func() { close(released) })
	endpoint := sim.NewResponder(&sim.Scenario{Model: "fake"}, func(sim.Request) sim.Turn {
		announceRequest()
		<-released
		return sim.Turn{Say: answer}
	})
	rig, _ := newEndpointRig(t, endpoint)
	t.Cleanup(release)
	return rig, endpoint, requested, release
}

func TestTurnUndoFailureThroughTheBinaryExplainsTheStopWithoutASecondNotice(t *testing.T) {
	rig, _, requested, release := newHeldResponseRig(t, "The held answer.")
	conversation := rig.start("--yolo", "-m", "opencode-go/fake", "keep the original message")
	select {
	case <-requested:
	case <-time.After(interactiveDeadline):
		t.Fatal("the provider request never arrived")
	}
	conversation.typeAndSettle("keep this draft")
	stored := rig.storedSessions()
	if len(stored) != 1 {
		t.Fatalf("expected one session, got %d", len(stored))
	}
	root, err := os.OpenRoot(filepath.Join(rig.stateDirectory, "org.crdx", "oh", "sessions", stored[0].Name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	if err := root.Rename("session.jsonl", "held.jsonl"); err != nil {
		t.Fatal(err)
	}
	restoreJournal := sync.OnceFunc(func() {
		if err := root.Remove("session.jsonl"); err != nil && !os.IsNotExist(err) {
			t.Error(err)
		}
		if err := root.Rename("held.jsonl", "session.jsonl"); err != nil {
			t.Error(err)
		}
	})
	t.Cleanup(restoreJournal)
	if err := root.Mkdir("session.jsonl", 0o700); err != nil {
		t.Fatal(err)
	}
	conversation.typeText(pressCtrlZ)
	conversation.waitFor("undo failed; the turn remains stopped:")
	conversation.requireShown("keep this draft")
	if strings.Contains(conversation.String(), "Turn stopped because the user pressed ctrl+z.") {
		t.Fatal("failed undo emitted a duplicate stop notice")
	}
	restoreJournal()
	release()
	conversation.typeText(pressCtrlC + pressCtrlC)
	conversation.quit()
	stored = rig.storedSessions()
	isRecorded := false
	for _, event := range stored[0].Events {
		if event.Kind == agent.InterruptionEvent && event.Name == "ctrl_z" {
			isRecorded = true
		}
	}
	if !isRecorded || !stored[0].CanResume() {
		t.Fatal("failed undo lost its recorded reason or left an incomplete turn")
	}
}
