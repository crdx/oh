package taskNames_test

import (
	"slices"
	"testing"
	"time"

	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/segment/taskNames"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/pkg/tasks"
)

var noon = time.Date(2001, time.January, 1, 12, 0, 0, 0, time.UTC)

func build(t *testing.T, listing ...tasks.Snapshot) segment.Segment {
	t.Helper()

	return buildWithRoutes(t, nil, listing...)
}

func buildWithRoutes(t *testing.T, routes []portgrant.Route, listing ...tasks.Snapshot) segment.Segment {
	t.Helper()

	built, err := taskNames.New(
		func() []tasks.Snapshot { return listing },
		func() []portgrant.Route { return routes },
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

func TestNoTasksDrawNothing(t *testing.T) {
	if drawn := build(t).Render(segment.Context{}); drawn != "" {
		t.Errorf("got %q, want nothing at all", drawn)
	}
}

func TestEveryRunningTaskIsNamed(t *testing.T) {
	drawn := style.Plain(build(t,
		tasks.Snapshot{ID: "docs", State: tasks.Running},
		tasks.Snapshot{ID: "watch", State: tasks.Running},
	).Render(segment.Context{}))

	if drawn != "\u25cf docs \u25cf watch" {
		t.Errorf("got %q, want both tasks named with their own mark", drawn)
	}
}

func TestEachRungNamesAtMostThreeTasksAndUsesStateMarksForTheRest(t *testing.T) {
	built := build(t,
		tasks.Snapshot{ID: "golden", State: tasks.Running},
		tasks.Snapshot{ID: "foo", State: tasks.Running},
		tasks.Snapshot{ID: "bar", State: tasks.Done, EndedAt: endedAgo(time.Second)},
		tasks.Snapshot{ID: "one", State: tasks.Done, EndedAt: endedAgo(time.Second)},
		tasks.Snapshot{ID: "two", State: tasks.Done, EndedAt: endedAgo(time.Second)},
		tasks.Snapshot{ID: "three", State: tasks.Done, EndedAt: endedAgo(time.Second)},
	)
	fitter, isFitter := built.(segment.Fitter)
	if !isFitter {
		t.Fatal("task names segment does not fit itself to available room")
	}

	ladder := fitter.Ladder(segment.Context{})
	got := make([]string, 0, len(ladder))
	for _, rung := range ladder {
		got = append(got, style.Plain(rung))
	}

	want := []string{
		"\u25cf golden \u25cf foo \u25cb bar \u25cb \u25cb \u25cb",
		"\u25cf golden \u25cf foo \u25cb \u25cb \u25cb \u25cb",
		"\u25cf golden \u25cf \u25cb \u25cb \u25cb \u25cb",
		"\u25cf \u25cf \u25cb \u25cb \u25cb \u25cb",
		"+",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestATaskWithAForwardedPortIsLeftForThePortSegmentToName(t *testing.T) {
	drawn := style.Plain(buildWithRoutes(t, []portgrant.Route{{Port: 8000, TaskName: "docs"}},
		tasks.Snapshot{ID: "docs", State: tasks.Running},
		tasks.Snapshot{ID: "watch", State: tasks.Running},
	).Render(segment.Context{}))

	if drawn != "\u25cf watch" {
		t.Errorf("got %q, want the forwarded task left out", drawn)
	}
}

func TestTasksAreSortedByStatusWithRunningFirst(t *testing.T) {
	drawn := style.Plain(build(t,
		tasks.Snapshot{ID: "failed", State: tasks.Failed, EndedAt: endedAgo(time.Second)},
		tasks.Snapshot{ID: "queued", State: tasks.Queued},
		tasks.Snapshot{ID: "done", State: tasks.Done, EndedAt: endedAgo(time.Second)},
		tasks.Snapshot{ID: "running", State: tasks.Running},
		tasks.Snapshot{ID: "stopping", State: tasks.Stopping},
	).Render(segment.Context{}))

	want := "\u25cf running \u25cf queued \u25cf stopping \u2717 \u25cb"
	if drawn != want {
		t.Errorf("got %q, want tasks sorted by status as %q", drawn, want)
	}
}

func TestATaskTakesTheStyleOfItsState(t *testing.T) {
	for _, test := range []struct {
		state tasks.State
		want  string
	}{
		{state: tasks.Queued, want: style.Info("\u25cf docs")},
		{state: tasks.Running, want: style.Info("\u25cf docs")},
		{state: tasks.Stopping, want: style.Change("\u25cf docs")},
		{state: tasks.Done, want: style.Dim("\u25cb docs")},
		{state: tasks.Stopped, want: style.Dim("\u25cb docs")},
		{state: tasks.Failed, want: style.Failure("\u2717 docs")},
		{state: tasks.TimedOut, want: style.Failure("\u2717 docs")},
	} {
		drawn := build(t, tasks.Snapshot{ID: "docs", State: test.state, EndedAt: endedAgo(time.Second)}).Render(segment.Context{})
		if drawn != test.want {
			t.Errorf("state %s drew %q, want %q", test.state, drawn, test.want)
		}
	}
}

func TestAGhostIsGoneOnceItHasBeenSeen(t *testing.T) {
	drawn := build(t,
		tasks.Snapshot{ID: "check", State: tasks.Done, EndedAt: endedAgo(time.Minute)},
		tasks.Snapshot{ID: "lint", State: tasks.Failed, EndedAt: endedAgo(time.Hour)},
		tasks.Snapshot{ID: "docs", State: tasks.Running},
	).Render(segment.Context{})

	if plain := style.Plain(drawn); plain != "\u25cf docs" {
		t.Errorf("got %q, want only what is still running", plain)
	}
}

func TestATaskLeftBehindByAClosedSessionIsNeverDrawn(t *testing.T) {
	if drawn := build(t, tasks.Snapshot{ID: "docs", State: tasks.Stopped}).Render(segment.Context{}); drawn != "" {
		t.Errorf("got %q, want a task with no end time left off the bar", drawn)
	}
}

func TestAGhostAsksForTheRedrawThatWillTakeItAway(t *testing.T) {
	endedAt := endedAgo(time.Second)
	refresher, _ := build(t, tasks.Snapshot{ID: "docs", State: tasks.Done, EndedAt: endedAt}).(segment.Refresher)

	at := refresher.NextRefresh(segment.Phase{At: noon})
	if !at.After(noon) {
		t.Errorf("got %v, want a redraw still to come", at)
	}
	if at.Before(endedAt) {
		t.Errorf("got %v, want the moment the ghost is due to go", at)
	}
}

func TestALiveTaskAsksForARedraw(t *testing.T) {
	refresher, isRefresher := build(t, tasks.Snapshot{ID: "docs", State: tasks.Running}).(segment.Refresher)
	if !isRefresher {
		t.Fatal("the segment does not ask for a redraw")
	}

	if at := refresher.NextRefresh(segment.Phase{At: noon}); at.IsZero() {
		t.Error("a running task did not ask for a redraw")
	}
}

func TestNothingLeftToDrawAsksForNoRedraw(t *testing.T) {
	refresher, isRefresher := build(t, tasks.Snapshot{ID: "builder", State: tasks.Failed, EndedAt: endedAgo(time.Hour)}).(segment.Refresher)
	if !isRefresher {
		t.Fatal("the segment does not ask for a redraw")
	}

	if at := refresher.NextRefresh(segment.Phase{At: noon}); !at.IsZero() {
		t.Errorf("got %v, want a segment with nothing to draw to ask for no redraw", at)
	}
}
