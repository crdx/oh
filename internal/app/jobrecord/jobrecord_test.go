package jobrecord_test

import (
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/jobrecord"
	"crdx.org/oh/internal/jobs"
	"crdx.org/oh/pkg/agent"
)

func TestTheLastListingIsTheOneRestored(t *testing.T) {
	events := []agent.Event{
		jobrecord.ListingEvent([]jobs.Snapshot{{Name: "old", State: jobs.StateComplete}}),
		{Kind: agent.UserMessageEvent, Text: "carry on"},
		jobrecord.ListingEvent([]jobs.Snapshot{
			{Name: "docs", Command: "python3 -m http.server", State: jobs.StateRunning},
			{Name: "build", Command: "just build", State: jobs.StateFailed},
		}),
	}

	restored, wasRecorded := jobrecord.LastRecorded(events)
	if !wasRecorded {
		t.Fatal("the listing was not restored")
	}
	if len(restored) != 2 || restored[0].Name != "docs" || restored[0].Command != "python3 -m http.server" {
		t.Errorf("got %#v, want the last listing with its commands", restored)
	}
}

func TestNothingIsRestoredWithoutAListing(t *testing.T) {
	events := []agent.Event{{Kind: agent.UserMessageEvent, Text: "hello"}}

	if _, wasRecorded := jobrecord.LastRecorded(events); wasRecorded {
		t.Error("a session with no jobs restored a listing")
	}
}

func TestAListingCarriesTheFactsAJobIsRestartedFrom(t *testing.T) {
	startedAt := time.Date(2026, time.August, 23, 14, 32, 9, 0, time.UTC)
	event := jobrecord.ListingEvent([]jobs.Snapshot{{
		Name:      "docs",
		Command:   "python3 -m http.server",
		State:     jobs.StateRunning,
		StartedAt: startedAt,
	}})

	restored, _ := jobrecord.LastRecorded([]agent.Event{event})
	if len(restored) != 1 {
		t.Fatalf("got %#v, want one job", restored)
	}
	if !restored[0].StartedAt.Equal(startedAt) {
		t.Errorf("got %v, want the moment it started", restored[0].StartedAt)
	}
}

func TestOneEndedJobIsNamedInTheSingular(t *testing.T) {
	notice, isSaid := jobrecord.EndedWithSessionNotice(jobrecord.EndedWithSessionEvent([]string{"docs"}))
	if !isSaid {
		t.Fatal("one ended job said nothing")
	}
	if want := "Job `docs` stopped"; !strings.Contains(notice, want) {
		t.Errorf("got %q, want it to say %q", notice, want)
	}
}

func TestSeveralEndedJobsAreNamedInThePlural(t *testing.T) {
	notice, isSaid := jobrecord.EndedWithSessionNotice(
		jobrecord.EndedWithSessionEvent([]string{"docs", "build", "watch"}),
	)
	if !isSaid {
		t.Fatal("several ended jobs said nothing")
	}
	if want := "Jobs `docs`, `build` and `watch` stopped"; !strings.Contains(notice, want) {
		t.Errorf("got %q, want it to name all three", notice)
	}
}

func TestAnEndedJobNoticeAgreesWithItsOwnNumber(t *testing.T) {
	const reopened = "The session was closed and reopened after the last turn. "
	const closed = " stopped when it closed. Restart "
	const remedy = " if still needed."

	for _, test := range []struct {
		names   []string
		expects string
	}{
		{[]string{"docs"}, reopened + "Job `docs`" + closed + "it" + remedy},
		{[]string{"docs", "watch"}, reopened + "Jobs `docs` and `watch`" + closed + "each" + remedy},
		{[]string{"docs", "build", "watch"}, reopened + "Jobs `docs`, `build` and `watch`" + closed + "each" + remedy},
	} {
		notice, isSaid := jobrecord.EndedWithSessionNotice(jobrecord.EndedWithSessionEvent(test.names))
		if !isSaid {
			t.Fatalf("%v said nothing", test.names)
		}
		if notice != test.expects {
			t.Errorf("got %q, want %q", notice, test.expects)
		}
	}
}

func TestTheModelIsToldHowToRestartAJobEndedWithTheSession(t *testing.T) {
	event := jobrecord.EndedWithSessionEvent([]string{"docs"})
	notice, isSaid := jobrecord.EndedWithSessionModelNotice(event)
	if !isSaid {
		t.Fatal("the ended job said nothing to the model")
	}
	if want := "Restart it with `job(action=\"start\", name=…)` if still needed."; !strings.Contains(notice, want) {
		t.Errorf("got %q, want it to say %q", notice, want)
	}
}

func TestAForkNamesTheSessionItsJobsWereRunningIn(t *testing.T) {
	for _, test := range []struct {
		names   []string
		expects string
	}{
		{
			[]string{"docs"},
			"Job `docs` was running in session `able-dolphin` when this session was forked from it. Restart it if still needed.",
		},
		{
			[]string{"docs", "watch"},
			"Jobs `docs` and `watch` were running in session `able-dolphin` when this session was forked from it. Restart each if still needed.",
		},
	} {
		event := jobrecord.EndedWithSourceSessionEvent("able-dolphin", test.names)
		notice, isSaid := jobrecord.EndedWithSessionNotice(event)
		if !isSaid {
			t.Fatalf("%v said nothing", test.names)
		}
		if notice != test.expects {
			t.Errorf("got %q, want %q", notice, test.expects)
		}
	}
}

func TestNoEndedJobsSayNothing(t *testing.T) {
	if _, isSaid := jobrecord.EndedWithSessionNotice(jobrecord.EndedWithSessionEvent(nil)); isSaid {
		t.Error("an empty list of ended jobs still said something")
	}
}

func TestAJobThatFinishedOnItsOwnSaysHowItWent(t *testing.T) {
	startedAt := time.Date(2026, time.August, 23, 14, 32, 9, 0, time.UTC)
	event := jobrecord.EndedEvent(jobs.Conclusion{
		Snapshot: jobs.Snapshot{
			Name:      "build",
			Command:   "just build",
			State:     jobs.StateFailed,
			StartedAt: startedAt,
			EndedAt:   startedAt.Add(12 * time.Second),
			ExitCode:  2,
		},
		Output: "undefined: getWidth\nexit status 1",
	})

	notice, isSaid := jobrecord.EndedNotice(event)
	if !isSaid {
		t.Fatal("a finished job said nothing")
	}

	expects := "Job `build` exited: failed after 12s, exit(2).\n\n" +
		"```\nundefined: getWidth\nexit status 1\n```"
	if notice != expects {
		t.Errorf("got %q, want %q", notice, expects)
	}
	if event.Name != "build" {
		t.Errorf("got %q, want the event to name the job", event.Name)
	}
}

func TestAJobNoticeSeparatesTruncationFromLiteralOutput(t *testing.T) {
	event := jobrecord.EndedEvent(jobs.Conclusion{
		Snapshot:     jobs.Snapshot{Name: "build", State: jobs.StateComplete},
		Output:       "  option   description\n```\n# not a heading\n",
		DroppedBytes: 128,
	})

	notice, isSaid := jobrecord.EndedNotice(event)
	if !isSaid {
		t.Fatal("a finished job said nothing")
	}
	want := "Job `build` exited: complete.\n" +
		"note: the oldest 128B of output was dropped to keep the spool bounded.\n\n" +
		"````\n  option   description\n```\n# not a heading\n````"
	if notice != want {
		t.Errorf("got %q, want %q", notice, want)
	}
}

func TestANamelessJobSaysNothingWhenItEnds(t *testing.T) {
	if _, isSaid := jobrecord.EndedNotice(jobrecord.EndedEvent(jobs.Conclusion{})); isSaid {
		t.Error("a job with no name still said something")
	}
}

func TestAnEmptyFinishedJobMarksItsCodedStatusLine(t *testing.T) {
	event := jobrecord.EndedEvent(jobs.Conclusion{
		Snapshot: jobs.Snapshot{Name: "build", State: jobs.StateComplete},
	})
	notice, isSaid := jobrecord.EndedNotice(event)
	if !isSaid {
		t.Fatal("a finished job said nothing")
	}
	if want := "Job `build` exited: complete with no output."; notice != want {
		t.Errorf("got %q, want %q", notice, want)
	}
}

func TestAJobNameContainingABacktickRemainsOneCodeSpan(t *testing.T) {
	event := jobrecord.EndedEvent(jobs.Conclusion{
		Snapshot: jobs.Snapshot{Name: "build`fast", State: jobs.StateComplete},
		Output:   "done",
	})
	notice, isSaid := jobrecord.EndedNotice(event)
	if !isSaid {
		t.Fatal("a finished job said nothing")
	}
	if want := "Job `` build`fast `` exited: complete.\n\n```\ndone\n```"; notice != want {
		t.Errorf("got %q, want %q", notice, want)
	}
}
