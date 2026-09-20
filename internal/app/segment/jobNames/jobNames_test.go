package jobNames_test

import (
	"testing"
	"time"

	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/segment/jobNames"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/jobs"
)

var noon = time.Date(2001, time.January, 1, 12, 0, 0, 0, time.UTC)

func build(t *testing.T, listing ...jobs.Snapshot) segment.Segment {
	t.Helper()

	built, err := jobNames.New(
		func() []jobs.Snapshot { return listing },
		func() time.Time { return noon },
	)(nil)
	if err != nil {
		t.Fatalf("could not build the segment: %v", err)
	}

	return built
}

func endedAgo(ago time.Duration) time.Time {
	return noon.Add(-ago)
}

func TestNoJobsDrawNothing(t *testing.T) {
	if drawn := build(t).Render(segment.Context{}); drawn != "" {
		t.Errorf("got %q, want nothing at all", drawn)
	}
}

func TestEveryRunningJobIsNamed(t *testing.T) {
	drawn := style.Plain(build(t,
		jobs.Snapshot{Name: "docs", State: jobs.StateRunning},
		jobs.Snapshot{Name: "watch", State: jobs.StateRunning},
	).Render(segment.Context{}))

	if drawn != "\u25cf docs \u25cf watch" {
		t.Errorf("got %q, want both jobs named with their own mark", drawn)
	}
}

func TestLiveJobsComeBeforeDeadJobs(t *testing.T) {
	drawn := style.Plain(build(t,
		jobs.Snapshot{Name: "failed", State: jobs.StateFailed, EndedAt: endedAgo(time.Second)},
		jobs.Snapshot{Name: "starting", State: jobs.StateStarting},
		jobs.Snapshot{Name: "complete", State: jobs.StateComplete, EndedAt: endedAgo(time.Second)},
		jobs.Snapshot{Name: "running", State: jobs.StateRunning},
		jobs.Snapshot{Name: "stopping", State: jobs.StateStopping},
	).Render(segment.Context{}))

	want := "\u25cf starting \u25cf running \u25cf stopping \u2717 failed \u25cb complete"
	if drawn != want {
		t.Errorf("got %q, want live jobs first and dead jobs last as %q", drawn, want)
	}
}

func TestAFailedJobIsNamedWithACross(t *testing.T) {
	drawn := style.Plain(build(t,
		jobs.Snapshot{Name: "builder", State: jobs.StateFailed, EndedAt: endedAgo(time.Second)},
	).Render(segment.Context{}))

	if drawn != "\u2717 builder" {
		t.Errorf("got %q, want the failure named", drawn)
	}
}

func TestAJobBeingStoppedIsDrawnAsAChange(t *testing.T) {
	stopping := build(t, jobs.Snapshot{Name: "docs", State: jobs.StateStopping}).Render(segment.Context{})
	running := build(t, jobs.Snapshot{Name: "docs", State: jobs.StateRunning}).Render(segment.Context{})

	if stopping == running {
		t.Error("a job being stopped drew the same as one running")
	}
	if stopping != style.Change("\u25cf docs") {
		t.Errorf("got %q, want the change style", stopping)
	}
}

func TestAJobJustFinishedLingersAsAGhost(t *testing.T) {
	for _, state := range []jobs.State{jobs.StateComplete, jobs.StateStopped, jobs.StateEnded} {
		drawn := build(t, jobs.Snapshot{
			Name:    "docs",
			State:   state,
			EndedAt: endedAgo(time.Second),
		}).Render(segment.Context{})

		if plain := style.Plain(drawn); plain != "\u25cb docs" {
			t.Errorf("state %s drew %q, want a hollow ghost", state, plain)
		}
		if drawn != style.Dim("\u25cb docs") {
			t.Errorf("state %s drew %q, want it held back in dim", state, drawn)
		}
	}
}

func TestAGhostIsGoneOnceItHasBeenSeen(t *testing.T) {
	drawn := build(t,
		jobs.Snapshot{Name: "check", State: jobs.StateComplete, EndedAt: endedAgo(time.Minute)},
		jobs.Snapshot{Name: "lint", State: jobs.StateFailed, EndedAt: endedAgo(time.Hour)},
		jobs.Snapshot{Name: "docs", State: jobs.StateRunning},
	).Render(segment.Context{})

	if plain := style.Plain(drawn); plain != "\u25cf docs" {
		t.Errorf("got %q, want only what is still running", plain)
	}
}

func TestAJobLeftBehindByAClosedSessionIsNeverDrawn(t *testing.T) {
	drawn := build(t, jobs.Snapshot{Name: "docs", State: jobs.StateEnded}).Render(segment.Context{})

	if drawn != "" {
		t.Errorf("got %q, want a job that ended with an earlier session left off the bar", drawn)
	}
}

func TestAGhostAsksForTheRedrawThatWillTakeItAway(t *testing.T) {
	endedAt := endedAgo(time.Second)
	refresher, _ := build(t, jobs.Snapshot{
		Name:    "docs",
		State:   jobs.StateComplete,
		EndedAt: endedAt,
	}).(segment.Refresher)

	at := refresher.NextRefresh(segment.Phase{At: noon})
	if !at.After(noon) {
		t.Errorf("got %v, want a redraw still to come", at)
	}
	if at.Before(endedAt) {
		t.Errorf("got %v, want the moment the ghost is due to go", at)
	}
}

func TestALiveJobAsksForARedraw(t *testing.T) {
	refresher, isRefresher := build(t, jobs.Snapshot{Name: "docs", State: jobs.StateRunning}).(segment.Refresher)
	if !isRefresher {
		t.Fatal("the segment does not ask for a redraw")
	}

	if at := refresher.NextRefresh(segment.Phase{At: noon}); at.IsZero() {
		t.Error("a running job did not ask for a redraw")
	}
}

func TestNothingLeftToDrawAsksForNoRedraw(t *testing.T) {
	refresher, isRefresher := build(t, jobs.Snapshot{
		Name:    "builder",
		State:   jobs.StateFailed,
		EndedAt: endedAgo(time.Hour),
	}).(segment.Refresher)
	if !isRefresher {
		t.Fatal("the segment does not ask for a redraw")
	}

	if at := refresher.NextRefresh(segment.Phase{At: noon}); !at.IsZero() {
		t.Errorf("got %v, want a segment with nothing to draw to ask for no redraw", at)
	}
}
