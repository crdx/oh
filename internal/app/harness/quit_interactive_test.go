package harness

import (
	"strings"
	"testing"
	"time"

	"crdx.org/oh/pkg/agent"
)

func TestControlDTakesBackThenStopsThenQuitsWithoutSendingQueuedMessages(t *testing.T) {
	rig, endpoint, requested, release := newHeldResponseRig(t, "The held answer.")
	session := rig.start("--yolo", "-m", "opencode-go/fake", "initial message")
	select {
	case <-requested:
	case <-time.After(interactiveDeadline):
		t.Fatal("the provider request never arrived")
	}
	session.typeText("must never be delivered" + pressEnter)
	session.waitFor("⏳ must never be delivered")
	previousDraws := session.Count("must never be delivered")
	session.typeText(endOfTransmission)
	session.waitForCount("must never be delivered", previousDraws+1)
	session.typeText(clearInput + endOfTransmission)
	session.waitFor("Turn stopped because the user pressed ctrl+d.")
	session.typeText(endOfTransmission)
	session.waitToExit()
	release()

	requests := endpoint.Requests()
	if len(requests) != 1 {
		t.Fatalf("got %d provider requests, want only the initial turn", len(requests))
	}
	for _, entry := range requests[0].Input {
		if strings.Contains(entry.Content, "must never be delivered") {
			t.Fatalf("the queued message reached the provider: %+v", entry)
		}
	}

	stored := rig.storedSessions()
	if len(stored) != 1 {
		t.Fatalf("got %d stored sessions, want one completed quit", len(stored))
	}
	wasCancellationPersisted := false
	for _, event := range stored[0].Events {
		if event.Kind == agent.InterruptionEvent && event.Name == "ctrl_d" {
			wasCancellationPersisted = true
		}
	}
	if !wasCancellationPersisted || !stored[0].CanResume() {
		t.Fatalf("the cancelled turn was not persisted: %+v", stored[0].Events)
	}
}
