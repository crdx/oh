package taskrecord_test

import (
	"strings"
	"testing"

	"crdx.org/oh/internal/app/taskrecord"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tasks"
)

func TestACompletedRunIsRecordedAndDescribed(t *testing.T) {
	event := taskrecord.EndedEvent(tasks.Conclusion{Snapshot: tasks.Snapshot{
		ID: "task-7", Run: 2, State: tasks.Failed, Output: "failed output", Failure: "exit status 1",
	}})
	notice, ok := taskrecord.EndedNotice(event)
	if !ok || !strings.Contains(notice, "task-7") || !strings.Contains(notice, "failed output") || !strings.Contains(notice, "exit status 1") {
		t.Errorf("completion notice %q, %t", notice, ok)
	}
}

func TestASpentRespawnSaysWhyItStopped(t *testing.T) {
	event := taskrecord.EndedEvent(tasks.Conclusion{
		Snapshot: tasks.Snapshot{ID: "watch", Run: 3, State: tasks.Done, IsRespawnLimitReached: true},
		IsFinal:  true,
	})
	notice, ok := taskrecord.EndedNotice(event)
	if !ok || !strings.Contains(notice, "respawning stopped after 3 runs in a row each ended within 2s") {
		t.Errorf("spent respawn notice %q, %t", notice, ok)
	}
}

func TestTheFinalListingRestoresPreviousRunOutcomesAndRespawnLimit(t *testing.T) {
	listing := taskrecord.ListingEvent([]tasks.Snapshot{{
		ID: "task-7", Run: 2, State: tasks.Done, IsRespawnLimitReached: true,
		Previous: []tasks.RunOutcome{{Run: 1, State: tasks.Failed, Output: "first"}},
	}})
	restored, ok := taskrecord.LastRecorded([]agent.Event{listing})
	if !ok || len(restored) != 1 || len(restored[0].Previous) != 1 ||
		restored[0].Previous[0].Output != "first" || !restored[0].IsRespawnLimitReached {
		t.Errorf("restored listing %+v, %t", restored, ok)
	}
}

func TestTheForkNoticeDistinguishesAClosedSession(t *testing.T) {
	closed := taskrecord.EndedWithSessionEvent([]string{"task-7"})
	forked := closed
	forked.Name = "source-session"
	closedText, closedOK := taskrecord.EndedWithSessionNotice(closed)
	forkedText, forkedOK := taskrecord.EndedWithSessionNotice(forked)
	if !closedOK || !strings.Contains(closedText, "session closed") ||
		!forkedOK || !strings.Contains(forkedText, "source-session") || strings.Contains(forkedText, "session closed") {
		t.Errorf("closed: %q (%t), fork: %q (%t)", closedText, closedOK, forkedText, forkedOK)
	}
}

func TestPendingTaskNoticesMatchEachRunAndRespectSilentStops(t *testing.T) {
	first := tasks.Conclusion{Snapshot: tasks.Snapshot{ID: "worker", Run: 1, State: tasks.Done}, IsFinal: false}
	second := tasks.Conclusion{Snapshot: tasks.Snapshot{ID: "worker", Run: 2, State: tasks.Done}, IsFinal: true}
	silent := tasks.Conclusion{Snapshot: tasks.Snapshot{ID: "quiet", Run: 1, State: tasks.Stopped}, IsSilent: true}
	closed := tasks.Conclusion{Snapshot: tasks.Snapshot{ID: "closed", Run: 1, State: tasks.Stopped}, IsSilent: true, IsSessionEnded: true}
	events := []agent.Event{
		taskrecord.FinishedEvent(first),
		taskrecord.EndedEvent(first),
		taskrecord.FinishedEvent(second),
		taskrecord.FinishedEvent(silent),
		taskrecord.FinishedEvent(closed),
	}
	pending := taskrecord.PendingNotices(events)
	if len(pending) != 2 || pending[0].Kind != taskrecord.RunEnded || pending[1].Kind != taskrecord.EndedWithSession {
		t.Fatalf("unexpected pending notices: %+v", pending)
	}
	if notice, isSaid := taskrecord.EndedNotice(pending[0]); !isSaid || !strings.Contains(notice, "run 2") {
		t.Errorf("delivery of run 1 suppressed run 2: %q", notice)
	}
	events = append(events, pending...)
	if remaining := taskrecord.PendingNotices(events); len(remaining) != 0 {
		t.Errorf("submitted notices remained pending: %+v", remaining)
	}
}

func TestOnlyALaterRunIsNumberedInItsNotice(t *testing.T) {
	for run, want := range map[int]string{
		1: "Task `check` done.",
		2: "Task `check` run 2 done.",
	} {
		conclusion := tasks.Conclusion{Snapshot: tasks.Snapshot{ID: "check", Run: run, State: tasks.Done}, IsFinal: true}
		if notice, isSaid := taskrecord.EndedNotice(taskrecord.EndedEvent(conclusion)); !isSaid || notice != want {
			t.Errorf("run %d said %q (%t), want %q", run, notice, isSaid, want)
		}
	}
}
