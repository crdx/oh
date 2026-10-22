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

func TestTheResumeNoticeNamesEachStoppedTask(t *testing.T) {
	for _, test := range []struct {
		event agent.Event
		want  string
	}{
		{
			event: taskrecord.EndedWithSessionEvent([]string{"docs"}),
			want:  "The session was closed and reopened after the last turn. Task `docs` stopped when it closed. Restart it if still needed.",
		},
		{
			event: taskrecord.EndedWithSessionEvent([]string{"docs", "build", "watch"}),
			want:  "The session was closed and reopened after the last turn. Tasks `docs`, `build` and `watch` stopped when it closed. Restart each if still needed.",
		},
		{
			event: agent.Event{Kind: taskrecord.EndedWithSession, Name: "able-dolphin", State: taskrecord.EndedWithSessionEvent([]string{"docs"}).State},
			want:  "Task `docs` was running in session `able-dolphin` when this session was forked from it. Restart it if still needed.",
		},
		{
			event: agent.Event{Kind: taskrecord.EndedWithSession, Name: "able-dolphin", State: taskrecord.EndedWithSessionEvent([]string{"docs", "watch"}).State},
			want:  "Tasks `docs` and `watch` were running in session `able-dolphin` when this session was forked from it. Restart each if still needed.",
		},
	} {
		if notice, isSaid := taskrecord.EndedWithSessionNotice(test.event); !isSaid || notice != test.want {
			t.Errorf("got %q (%t), want %q", notice, isSaid, test.want)
		}
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

func TestAFailedTaskNoticeSaysWhyItFailed(t *testing.T) {
	conclusion := tasks.Conclusion{
		Snapshot: tasks.Snapshot{ID: "task-41", Run: 1, State: tasks.Failed, Failure: "the command exited with status 1", Output: "exit(1):\nnope\n"},
		IsFinal:  true,
	}

	notice, isSaid := taskrecord.EndedNotice(taskrecord.EndedEvent(conclusion))
	if want := "Task `task-41` failed: the command exited with status 1.\n\n```\nexit(1):\nnope\n```"; !isSaid || notice != want {
		t.Errorf("got %q (%t), want %q", notice, isSaid, want)
	}
}
