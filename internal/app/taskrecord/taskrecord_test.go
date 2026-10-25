package taskrecord_test

import (
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/taskrecord"
	"crdx.org/oh/internal/toolresult"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
	"crdx.org/oh/pkg/tasks"
)

func TestACompletedRunIsRecordedAndDescribed(t *testing.T) {
	event := taskrecord.EndedEvent(tasks.Conclusion{Snapshot: tasks.Snapshot{
		ID: "task-7", Run: 2, State: tasks.Failed, Output: "failed output", Failure: "exit status 1",
	}})
	notice, ok := taskrecord.EndedNotice(event)
	if !ok || !strings.Contains(notice, "task-7") || !strings.Contains(notice, "failed output") || !strings.Contains(notice, "exited 1") {
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
		1: "Task `check` finished",
		2: "Task `check` run 2 finished",
	} {
		conclusion := tasks.Conclusion{Snapshot: tasks.Snapshot{ID: "check", Run: run, State: tasks.Done}, IsFinal: true}
		if notice, isSaid := taskrecord.EndedNotice(taskrecord.EndedEvent(conclusion)); !isSaid || notice != want {
			t.Errorf("run %d said %q (%t), want %q", run, notice, isSaid, want)
		}
	}
}

func TestEveryTerminalTaskNoticeUsesTheSamePunctuation(t *testing.T) {
	startedAt := time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		state                 tasks.State
		failure               string
		isFinal               bool
		isRespawnLimitReached bool
		want                  string
	}{
		{state: tasks.Done, isFinal: true, want: "Task `golden-1` finished after 8m 10s"},
		{state: tasks.Failed, failure: "exit status 1", isFinal: true, want: "Task `golden-1` failed after 8m 10s: exited 1"},
		{state: tasks.TimedOut, failure: "time limit reached", isFinal: true, want: "Task `golden-1` timed out after 8m 10s: time limit reached"},
		{state: tasks.Stopped, failure: "the user stopped it.", isFinal: true, want: "Task `golden-1` stopped after 8m 10s: the user stopped it."},
		{state: tasks.Done, want: "Task `golden-1` finished after 8m 10s; respawning"},
		{
			state: tasks.Done, isFinal: true, isRespawnLimitReached: true,
			want: "Task `golden-1` finished after 8m 10s; " + tasks.RespawnLimitReason(),
		},
	} {
		conclusion := tasks.Conclusion{Snapshot: tasks.Snapshot{
			ID: "golden-1", Run: 1, State: test.state, Failure: test.failure,
			StartedAt: startedAt, EndedAt: startedAt.Add(8*time.Minute + 10*time.Second),
			IsRespawnLimitReached: test.isRespawnLimitReached,
		}, IsFinal: test.isFinal}
		if notice, isSaid := taskrecord.EndedNotice(taskrecord.EndedEvent(conclusion)); !isSaid || notice != test.want {
			t.Errorf("state %s said %q (%t), want %q", test.state, notice, isSaid, test.want)
		}
	}
}

func TestAFailedTaskNoticeSaysWhyItFailed(t *testing.T) {
	conclusion := tasks.Conclusion{
		Snapshot: tasks.Snapshot{ID: "task-41", Run: 1, State: tasks.Failed, Failure: "the command exited with status 1", Output: "exit(1):\nnope\n"},
		IsFinal:  true,
	}

	notice, isSaid := taskrecord.EndedNotice(taskrecord.EndedEvent(conclusion))
	if want := "Task `task-41` failed: exited 1\n\n```\nexit(1):\nnope\n```"; !isSaid || notice != want {
		t.Errorf("got %q (%t), want %q", notice, isSaid, want)
	}
}

func TestARemovedTaskLeavesNoPendingNotice(t *testing.T) {
	built := tasks.Snapshot{ID: "build", ToolName: "bash", Run: 1, State: tasks.Done, Output: "built ok\n"}
	tested := tasks.Snapshot{ID: "test", ToolName: "bash", Run: 1, State: tasks.Done, Output: "tests passed\n"}
	pending := taskrecord.PendingNotices([]agent.Event{
		taskrecord.FinishedEvent(tasks.Conclusion{Snapshot: built, IsFinal: true}),
		taskrecord.FinishedEvent(tasks.Conclusion{Snapshot: tested, IsFinal: true}),
		taskrecord.ListingEvent([]tasks.Snapshot{built, tested}),
		taskrecord.ListingEvent([]tasks.Snapshot{tested}),
	})

	if len(pending) != 1 || pending[0].Name != "test" {
		t.Errorf("got %+v, want only the listed task's notice", pending)
	}
}

func TestAShownNoticeKeepsOnlyTheEndsOfLongOutput(t *testing.T) {
	output := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\n"
	conclusion := tasks.Conclusion{Snapshot: tasks.Snapshot{ID: "build", Run: 1, State: tasks.Done, Output: output}, IsFinal: true}
	completion, isEnded := taskrecord.CompletionOf(taskrecord.EndedEvent(conclusion))
	if !isEnded {
		t.Fatal("the ended event was not read back")
	}

	for edgeLines, want := range map[int]string{
		0: "Task `build` finished",
		2: "Task `build` finished\n\n```\none\ntwo\n⋮ 5 more lines\neight\nnine\n```",
		3: "Task `build` finished\n\n```\none\ntwo\nthree\n⋮ 3 more lines\nseven\neight\nnine\n```",
		4: "Task `build` finished\n\n```\none\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\n```",
		9: "Task `build` finished\n\n```\none\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\n```",
	} {
		if got := completion.NoticeWithin(edgeLines); got != want {
			t.Errorf("%d lines at each end gave %q, want %q", edgeLines, got, want)
		}
	}
	if notice, _ := taskrecord.EndedNotice(taskrecord.EndedEvent(conclusion)); !strings.Contains(notice, "four\nfive\nsix") {
		t.Errorf("the model's notice lost the middle of the output: %q", notice)
	}
}

func TestNoLinesAtEachEndHidesEvenASingleLine(t *testing.T) {
	conclusion := tasks.Conclusion{Snapshot: tasks.Snapshot{ID: "build", Run: 1, State: tasks.Done, Output: "built oh\n"}, IsFinal: true}
	completion, _ := taskrecord.CompletionOf(taskrecord.EndedEvent(conclusion))
	if got := completion.NoticeWithin(0); got != "Task `build` finished" {
		t.Errorf("got %q", got)
	}
	if got := completion.NoticeWithin(1); got != "Task `build` finished\n\n```\nbuilt oh\n```" {
		t.Errorf("got %q", got)
	}
}

func TestAShownNoticeWithoutOutputHasNoBlock(t *testing.T) {
	conclusion := tasks.Conclusion{Snapshot: tasks.Snapshot{ID: "build", Run: 1, State: tasks.Done}, IsFinal: true}
	completion, _ := taskrecord.CompletionOf(taskrecord.EndedEvent(conclusion))
	if got := completion.NoticeWithin(0); got != "Task `build` finished" {
		t.Errorf("got %q", got)
	}
}

func TestReadFindsTheRunALinkNames(t *testing.T) {
	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	endedAt := time.Date(2026, 10, 24, 12, 0, 0, 123456789, time.UTC)
	runs := []tasks.Snapshot{
		{ID: "build", Run: 1, State: tasks.Failed, EndedAt: endedAt, Output: "first build"},
		{ID: "build", Run: 2, State: tasks.Done, EndedAt: endedAt.Add(time.Second), Output: "second run"},
		{ID: "build", Run: 1, State: tasks.Done, EndedAt: endedAt.Add(time.Minute), Output: "a later task of the same name"},
		{ID: "watch", Run: 1, State: tasks.Done, Output: "a run with no end time"},
	}
	for _, run := range runs {
		if _, err := writer.Event(taskrecord.EndedEvent(tasks.Conclusion{Snapshot: run})); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Event(taskrecord.FinishedEvent(tasks.Conclusion{Snapshot: run})); err != nil {
			t.Fatal(err)
		}
	}
	name := writer.Name()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	for _, run := range runs {
		completion := taskrecord.Completion{Snapshot: run}
		found, err := taskrecord.Read(directory, completion.Reference(name))
		if err != nil || found.Output != run.Output {
			t.Errorf("run %d ended at %s gave %q and %v", run.Run, run.EndedAt, found.Output, err)
		}
	}

	missing := taskrecord.Completion{Snapshot: tasks.Snapshot{ID: "build", Run: 3, EndedAt: endedAt}}
	if _, err := taskrecord.Read(directory, missing.Reference(name)); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("a missing run gave %v", err)
	}
}

func TestATimedOutTaskNoticeSpeaksItsState(t *testing.T) {
	conclusion := tasks.Conclusion{
		Snapshot: tasks.Snapshot{ID: "deploy-wait", Run: 1, State: tasks.TimedOut, Failure: "the call reached its time limit of 5m"},
		IsFinal:  true,
	}
	notice, isSaid := taskrecord.EndedNotice(taskrecord.EndedEvent(conclusion))
	if want := "Task `deploy-wait` timed out: the call reached its time limit of 5m"; !isSaid || notice != want {
		t.Errorf("got %q (%t), want %q", notice, isSaid, want)
	}
}

func TestReadFindsTheCurrentProgressOfARunningTask(t *testing.T) {
	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "start"}); err != nil {
		t.Fatal(err)
	}

	snapshot := tasks.Snapshot{
		ID:       "build",
		ToolName: "bash",
		Run:      2,
		State:    tasks.Running,
		Output:   "first phase\n",
	}
	if err := taskrecord.WriteCurrent(session.Dir(directory, writer.Name()), []tasks.Snapshot{snapshot}); err != nil {
		t.Fatal(err)
	}
	reference := taskrecord.Completion{Snapshot: snapshot}.Reference(writer.Name())
	found, err := taskrecord.Read(directory, reference)
	if err != nil || found.Output != "first phase\n" || found.State != tasks.Running {
		t.Fatalf("current progress is %+v, %v", found, err)
	}

	snapshot.Output = "first phase\nsecond phase\n"
	if err := taskrecord.WriteCurrent(session.Dir(directory, writer.Name()), []tasks.Snapshot{snapshot}); err != nil {
		t.Fatal(err)
	}
	found, err = taskrecord.Read(directory, reference)
	if err != nil || found.Output != snapshot.Output {
		t.Errorf("updated progress is %+v, %v", found, err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := taskrecord.Read(directory, reference); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("an inactive session exposed stale current progress: %v", err)
	}
}

func TestWriteCurrentKeepsEveryLiveTaskState(t *testing.T) {
	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Close() }()
	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "start"}); err != nil {
		t.Fatal(err)
	}

	snapshots := []tasks.Snapshot{
		{ID: "queued", Run: 1, State: tasks.Queued},
		{ID: "running", Run: 1, State: tasks.Running},
		{ID: "stopping", Run: 1, State: tasks.Stopping},
	}
	if err := taskrecord.WriteCurrent(session.Dir(directory, writer.Name()), snapshots); err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range snapshots {
		reference := taskrecord.Completion{Snapshot: snapshot}.Reference(writer.Name())
		found, err := taskrecord.Read(directory, reference)
		if err != nil || found.State != snapshot.State {
			t.Errorf("%s task is %+v, %v", snapshot.State, found, err)
		}
	}
}

func TestWriteCurrentRemovesATaskOnceItsRunFinishes(t *testing.T) {
	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Close() }()
	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "start"}); err != nil {
		t.Fatal(err)
	}

	running := tasks.Snapshot{ID: "build", Run: 1, State: tasks.Running, Output: "working\n"}
	if err := taskrecord.WriteCurrent(session.Dir(directory, writer.Name()), []tasks.Snapshot{running}); err != nil {
		t.Fatal(err)
	}
	finished := running
	finished.State = tasks.Done
	finished.EndedAt = time.Unix(1, 0)
	finished.Output = "done\n"
	if _, err := writer.Event(taskrecord.FinishedEvent(tasks.Conclusion{Snapshot: finished})); err != nil {
		t.Fatal(err)
	}
	if err := taskrecord.WriteCurrent(session.Dir(directory, writer.Name()), []tasks.Snapshot{finished}); err != nil {
		t.Fatal(err)
	}

	reference := taskrecord.Completion{Snapshot: running}.Reference(writer.Name())
	found, err := taskrecord.Read(directory, reference)
	if err != nil || found.State != tasks.Done || found.Output != "done\n" {
		t.Errorf("finished task is %+v, %v", found, err)
	}
}

func TestAnUnfinishedLinkFallsBackToTheLatestFinishedMatchingRun(t *testing.T) {
	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := tasks.Snapshot{ID: "build", Run: 1, State: tasks.Done, EndedAt: time.Unix(1, 0), Output: "first"}
	latest := tasks.Snapshot{ID: "build", Run: 1, State: tasks.Done, EndedAt: time.Unix(2, 0), Output: "latest"}
	for _, snapshot := range []tasks.Snapshot{first, latest} {
		if _, err := writer.Event(taskrecord.FinishedEvent(tasks.Conclusion{Snapshot: snapshot})); err != nil {
			t.Fatal(err)
		}
	}
	if err := taskrecord.WriteCurrent(session.Dir(directory, writer.Name()), nil); err != nil {
		t.Fatal(err)
	}
	name := writer.Name()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	found, err := taskrecord.Read(directory, toolresult.TaskReference{SessionName: name, TaskName: "build", Run: 1})
	if err != nil || found.Output != "latest" {
		t.Errorf("latest finished run is %+v, %v", found, err)
	}
}
