package commands

import (
	"errors"
	"strings"
	"testing"
	"time"

	"crdx.org/io/internal/app/caps"
	"crdx.org/io/internal/app/slash"
	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/jobs"
	"crdx.org/io/pkg/agent"
)

func fixtureJobs() (Jobs, *[]string) {
	stopped := []string{}
	discarded := []string{}
	_ = discarded
	listing := []jobs.Snapshot{
		{
			Name:      "docs",
			Command:   "python3 -m http.server 8080",
			State:     jobs.StateRunning,
			StartedAt: time.Now().Add(-30 * time.Second),
		},
		{
			Name:      "build",
			Command:   "just build",
			State:     jobs.StateFailed,
			StartedAt: time.Now().Add(-90 * time.Second),
			EndedAt:   time.Now().Add(-88 * time.Second),
			ExitCode:  1,
		},
	}

	find := func(name string) (jobs.Snapshot, error) {
		for _, snapshot := range listing {
			if snapshot.Name == name {
				return snapshot, nil
			}
		}

		return jobs.Snapshot{}, jobs.ErrNotFound
	}

	managedJobs := Jobs{
		List:   func() []jobs.Snapshot { return listing },
		Status: find,
		Output: func(name string) (string, jobs.Snapshot, error) {
			snapshot, err := find(name)
			if err != nil {
				return "", snapshot, err
			}

			return "Serving HTTP on localhost port 8080 ...\n", snapshot, nil
		},
		PruneFinished: func() []string {
			pruned := []string{}
			for _, snapshot := range listing {
				if !snapshot.IsLive() {
					pruned = append(pruned, snapshot.Name)
				}
			}

			return pruned
		},
		Discard: func(name string) (jobs.Snapshot, error) {
			snapshot, err := find(name)
			if err != nil {
				return jobs.Snapshot{}, err
			}
			discarded = append(discarded, name)

			return snapshot, nil
		},
		Stop: func(name string) (agent.Event, error) {
			snapshot, err := find(name)
			if err != nil {
				return agent.Event{}, err
			}
			stopped = append(stopped, snapshot.Name)

			return caps.JobStoppedByUserEvent(snapshot.Name), nil
		},
	}

	return managedJobs, &stopped
}

func invokeJobCommand(t *testing.T, managedJobs Jobs, input string) (*commandTestContext, error) {
	t.Helper()

	registry := newCommandRegistry(t, commandEnvironment{jobs: managedJobs})
	invocation, isFound := registry.Find(input)
	if !isFound {
		t.Fatalf("did not find %s", input)
	}
	context := &commandTestContext{}

	return context, invocation.Command.Run(context, invocation.Arguments)
}

func TestTheJobsCommandListsEveryJob(t *testing.T) {
	managedJobs, _ := fixtureJobs()

	context, err := invokeJobCommand(t, managedJobs, "/jobs")
	if err != nil {
		t.Fatal(err)
	}

	notice := style.Plain(context.notice)
	for _, wanted := range []string{"docs: running", "$ python3 -m http.server 8080", "build: failed", "exit(1)"} {
		if !strings.Contains(notice, wanted) {
			t.Errorf("got %q, want it to carry %q", notice, wanted)
		}
	}
}

func TestTheJobsCommandSaysSoWhenThereAreNone(t *testing.T) {
	managedJobs, _ := fixtureJobs()
	managedJobs.List = func() []jobs.Snapshot { return nil }

	context, err := invokeJobCommand(t, managedJobs, "/jobs")
	if err != nil {
		t.Fatal(err)
	}

	if notice := context.notice; notice != "No background jobs." {
		t.Errorf("got %q, want it to say there are none", notice)
	}
}

func TestTheJobCommandReportsOneStatus(t *testing.T) {
	managedJobs, _ := fixtureJobs()

	context, err := invokeJobCommand(t, managedJobs, "/job status docs")
	if err != nil {
		t.Fatal(err)
	}

	if notice := context.notice; !strings.HasPrefix(notice, "docs: running") {
		t.Errorf("got %q, want the job's own status", notice)
	}
}

func TestTheJobCommandShowsWhatAJobPrinted(t *testing.T) {
	managedJobs, _ := fixtureJobs()

	context, err := invokeJobCommand(t, managedJobs, "/job output docs")
	if err != nil {
		t.Fatal(err)
	}

	if notice := context.notice; !strings.Contains(notice, "Serving HTTP") {
		t.Errorf("got %q, want the job's output", notice)
	}
}

func TestAJobThatPrintedNothingSaysSo(t *testing.T) {
	managedJobs, _ := fixtureJobs()
	managedJobs.Output = func(string) (string, jobs.Snapshot, error) {
		return "   \n", jobs.Snapshot{Name: "docs", State: jobs.StateRunning}, nil
	}

	context, err := invokeJobCommand(t, managedJobs, "/job output docs")
	if err != nil {
		t.Fatal(err)
	}

	if notice := context.notice; !strings.HasSuffix(notice, " (no output)") || strings.Contains(notice, "\n") {
		t.Errorf("got %q, want the no-output marker on the status line", notice)
	}
}

func TestStoppingAJobFromTheKeyboardTellsTheModel(t *testing.T) {
	managedJobs, stopped := fixtureJobs()

	context, err := invokeJobCommand(t, managedJobs, "/job stop docs")
	if err != nil {
		t.Fatal(err)
	}

	if len(*stopped) != 1 || (*stopped)[0] != "docs" {
		t.Errorf("got stopped %v, want the named job alone", *stopped)
	}

	if len(context.events) != 1 || context.events[0].Kind != caps.JobStop {
		t.Fatalf("got events %#v, want the stop recorded", context.events)
	}

	notice, isShown := caps.JobStopNotice(context.events[0])
	if !isShown || !strings.Contains(notice, "docs") {
		t.Errorf("got %q, want the recorded stop to name the job", notice)
	}
}

func TestAnUnknownJobIsRefused(t *testing.T) {
	managedJobs, _ := fixtureJobs()

	for _, input := range []string{"/job status ghost", "/job output ghost", "/job stop ghost", "/job discard ghost"} {
		if _, err := invokeJobCommand(t, managedJobs, input); !errors.Is(err, jobs.ErrNotFound) {
			t.Errorf("%s gave %v, want the job not to be found", input, err)
		}
	}
}

func TestTheJobCommandRefusesAnIncompleteInvocation(t *testing.T) {
	managedJobs, _ := fixtureJobs()

	for _, input := range []string{"/job", "/job status", "/job docs", "/job frobnicate docs"} {
		if _, err := invokeJobCommand(t, managedJobs, input); !slash.IsUsageError(err) {
			t.Errorf("%s gave %v, want the usage to be shown", input, err)
		}
	}
}

func TestTheJobCommandsAreAbsentWithoutAManager(t *testing.T) {
	registry := newCommandRegistry(t, commandEnvironment{})

	for _, input := range []string{"/jobs", "/job status docs"} {
		if _, isFound := registry.Find(input); isFound {
			t.Errorf("%s was offered with no job manager", input)
		}
	}
}

func TestAJobCanBeDiscardedFromTheKeyboard(t *testing.T) {
	managedJobs, _ := fixtureJobs()

	context, err := invokeJobCommand(t, managedJobs, "/job discard build")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(context.success, "build") {
		t.Errorf("got %q, want it to confirm the job was discarded", context.success)
	}
	if len(context.events) != 0 {
		t.Errorf("got events %#v, want discarding to record nothing", context.events)
	}
}

func TestPruningWithNoNameSweepsEveryFinishedJob(t *testing.T) {
	managedJobs, _ := fixtureJobs()

	context, err := invokeJobCommand(t, managedJobs, "/job prune")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(context.success, "build") {
		t.Errorf("got %q, want the sweep to name what it pruned", context.success)
	}
	if strings.Contains(context.success, "docs") {
		t.Errorf("got %q, want the running job left alone", context.success)
	}
}
