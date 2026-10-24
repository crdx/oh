package taskNames_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/segment/taskNames"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/toolresult"
	"crdx.org/oh/pkg/tasks"
)

var noon = time.Date(2001, time.January, 1, 12, 0, 0, 0, time.UTC)

func build(t *testing.T, listing ...tasks.Snapshot) segment.Segment {
	t.Helper()

	return buildWithRoutes(t, nil, listing...)
}

func buildWithRoutes(t *testing.T, routes []portgrant.Route, listing ...tasks.Snapshot) segment.Segment {
	t.Helper()
	for index := range listing {
		if listing[index].Run == 0 {
			listing[index].Run = 1
		}
	}

	built, err := taskNames.New(
		"tame-impala",
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

func refresherOf(t *testing.T, built segment.Segment) segment.Refresher {
	t.Helper()
	refresher, isRefresher := built.(segment.Refresher)
	if !isRefresher {
		t.Fatal("the segment does not ask for a redraw")
	}
	return refresher
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
		{state: tasks.Queued, want: style.Info("\u25cf") + " " + style.Info("docs")},
		{state: tasks.Running, want: style.Info("\u25cf") + " " + style.Info("docs")},
		{state: tasks.Stopping, want: style.Change("\u25cf") + " " + style.Change("docs")},
		{state: tasks.Done, want: style.Dim("\u25cb docs")},
		{state: tasks.Stopped, want: style.Dim("\u25cb docs")},
		{state: tasks.Failed, want: style.Failure("\u2717 docs")},
		{state: tasks.TimedOut, want: style.Failure("\u2717 docs")},
	} {
		snapshot := tasks.Snapshot{ID: "docs", Run: 1, State: test.state}
		if test.state == tasks.Done || test.state == tasks.Stopped || test.state == tasks.Failed || test.state == tasks.TimedOut {
			snapshot.EndedAt = endedAgo(time.Second)
		}
		drawn := build(t, snapshot).Render(segment.Context{})
		address := toolresult.TaskURL(toolresult.TaskReference{
			EndedAt:     snapshot.EndedAt,
			SessionName: "tame-impala",
			TaskName:    snapshot.ID,
			Run:         snapshot.Run,
		})
		want := link.RenderURL(test.want, address)
		if drawn != want {
			t.Errorf("state %s drew %q, want %q", test.state, drawn, want)
		}
	}
}

func TestTaskActivityFlashExpiresOnTime(t *testing.T) {
	now := noon.Add(500 * time.Millisecond)
	built, err := taskNames.New(
		"tame-impala",
		func() []tasks.Snapshot {
			return []tasks.Snapshot{{ID: "docs", State: tasks.Running, ActivityAt: noon}}
		},
		func() []portgrant.Route { return nil },
		func() time.Time { return now },
	)(nil)
	if err != nil {
		t.Fatal(err)
	}

	before := built.Render(segment.Context{})
	wantActive := style.Accent("\u25cf") + " " + style.Info("docs")
	if before != wantActive {
		t.Errorf("active task drew %q, want the accent mark in %q", before, wantActive)
	}
	if due := refresherOf(t, built).NextRefresh(segment.Phase{At: now}); !due.Equal(noon.Add(tasks.ActivityFlash)) {
		t.Fatalf("flash expires at %s, want %s", due, noon.Add(tasks.ActivityFlash))
	}
	now = noon.Add(tasks.ActivityFlash)
	after := built.Render(segment.Context{})
	if before == after || style.Plain(before) != style.Plain(after) {
		t.Fatalf("flash did not return to the same quiet task mark: %q then %q", before, after)
	}
}

func TestACompletedTaskMarkStaysEmptyUntilExpiry(t *testing.T) {
	for _, state := range []tasks.State{tasks.Done, tasks.Stopped} {
		for _, age := range []time.Duration{time.Second, 9 * time.Second} {
			built := build(t, tasks.Snapshot{
				ID:      "docs",
				State:   state,
				EndedAt: endedAgo(age),
			})
			if drawn := style.Plain(built.Render(segment.Context{})); drawn != "\u25cb docs" {
				t.Errorf("a %s task ended %s ago drew %q, want an empty mark", state, age, drawn)
			}

			wantRefresh := noon.Add(10*time.Second - age)
			if at := refresherOf(t, built).NextRefresh(segment.Phase{At: noon}); !at.Equal(wantRefresh) {
				t.Errorf("a %s task ended %s ago refreshes at %s, want %s", state, age, at, wantRefresh)
			}
		}

		expired := build(t, tasks.Snapshot{
			ID:      "docs",
			State:   state,
			EndedAt: endedAgo(10 * time.Second),
		})
		if drawn := expired.Render(segment.Context{}); drawn != "" {
			t.Errorf("an expired %s task drew %q, want nothing", state, drawn)
		}
	}
}

func TestAFailedTaskKeepsItsMarkUntilExpiry(t *testing.T) {
	for _, state := range []tasks.State{tasks.Failed, tasks.TimedOut} {
		built := build(t, tasks.Snapshot{
			ID:      "docs",
			State:   state,
			EndedAt: endedAgo(5 * time.Second),
		})
		if drawn := style.Plain(built.Render(segment.Context{})); drawn != "\u2717 docs" {
			t.Errorf("a %s task drew %q before expiry, want its failure mark", state, drawn)
		}

		refresher := refresherOf(t, built)
		if at := refresher.NextRefresh(segment.Phase{At: noon}); !at.Equal(noon.Add(5 * time.Second)) {
			t.Errorf("a %s task refreshes at %s, want its expiry at %s", state, at, noon.Add(5*time.Second))
		}

		expired := build(t, tasks.Snapshot{
			ID:      "docs",
			State:   state,
			EndedAt: endedAgo(10 * time.Second),
		})
		if drawn := expired.Render(segment.Context{}); drawn != "" {
			t.Errorf("an expired %s task drew %q, want nothing", state, drawn)
		}
		if at := refresherOf(t, expired).NextRefresh(segment.Phase{At: noon}); !at.IsZero() {
			t.Errorf("an expired %s task asks for another refresh at %s", state, at)
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

func TestATaskNameAndItsMarkOpenTheCurrentRun(t *testing.T) {
	snapshot := tasks.Snapshot{ID: "docs", Run: 2, State: tasks.Running}
	drawn := build(t, snapshot).Render(segment.Context{})
	address := toolresult.TaskURL(toolresult.TaskReference{
		SessionName: "tame-impala",
		TaskName:    snapshot.ID,
		Run:         snapshot.Run,
	})
	want := link.RenderURL(style.Info("●")+" "+style.Info("docs"), address)
	if drawn != want {
		t.Errorf("got %q, want the whole task mark and name linked as %q", drawn, want)
	}
}

func TestEveryUnnamedTaskMarkOpensItsOwnRun(t *testing.T) {
	snapshots := []tasks.Snapshot{
		{ID: "one", Run: 1, State: tasks.Running},
		{ID: "two", Run: 2, State: tasks.Running},
		{ID: "three", Run: 3, State: tasks.Running},
		{ID: "four", Run: 4, State: tasks.Running},
	}
	built := build(t, snapshots...)
	fitter, isFitter := built.(segment.Fitter)
	if !isFitter {
		t.Fatal("task names segment does not fit itself to available room")
	}
	ladder := fitter.Ladder(segment.Context{})
	unnamed := ladder[len(ladder)-2]
	for _, snapshot := range snapshots {
		address := toolresult.TaskURL(toolresult.TaskReference{
			SessionName: "tame-impala",
			TaskName:    snapshot.ID,
			Run:         snapshot.Run,
		})
		if count := strings.Count(unnamed, address); count != 1 {
			t.Errorf("task %s link occurs %d times in %q", snapshot.ID, count, unnamed)
		}
	}
}

func TestARecentlyFinishedTaskLinksItsExactEndTime(t *testing.T) {
	endedAt := endedAgo(time.Second)
	snapshot := tasks.Snapshot{ID: "build", Run: 1, State: tasks.Done, EndedAt: endedAt}
	drawn := build(t, snapshot).Render(segment.Context{})
	address := toolresult.TaskURL(toolresult.TaskReference{
		EndedAt:     endedAt,
		SessionName: "tame-impala",
		TaskName:    snapshot.ID,
		Run:         snapshot.Run,
	})
	if !strings.Contains(drawn, address) {
		t.Errorf("finished task did not link its exact run in %q", drawn)
	}
}
