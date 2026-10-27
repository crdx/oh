package commands

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/call"
	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/toolbox/bash"
)

var errNoSuchTask = errors.New("there is no task named ghost")

type taskActions struct {
	stopped   []string
	restarted []string
	discarded []string
}

func bashInvocation(command string) *tasks.Invocation {
	arguments, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		panic(err)
	}
	return &tasks.Invocation{Arguments: string(arguments)}
}

func describeFixtureCall(snapshot tasks.Snapshot) string {
	var arguments struct {
		Command string `json:"command"`
	}
	if snapshot.Invocation != nil {
		_ = json.Unmarshal([]byte(snapshot.Invocation.Arguments), &arguments)
	}
	return call.LabelForRendering(bash.DescribeCommand(arguments.Command)).Render()
}

func fixtureTasks() (Tasks, *taskActions) {
	actions := &taskActions{}
	listing := []tasks.Snapshot{
		{
			ID:         "docs",
			ToolName:   "bash",
			Invocation: bashInvocation("python3 -m http.server 8080"),
			State:      tasks.Running,
			Run:        1,
			StartedAt:  time.Now().Add(-30 * time.Second),
		},
		{
			ID:         "build",
			ToolName:   "bash",
			Invocation: bashInvocation("just build"),
			State:      tasks.Failed,
			Run:        1,
			StartedAt:  time.Now().Add(-90 * time.Second),
			EndedAt:    time.Now().Add(-88 * time.Second),
			Failure:    "exit status 1",
		},
	}

	find := func(name string) (tasks.Snapshot, error) {
		for _, snapshot := range listing {
			if snapshot.ID == name {
				return snapshot, nil
			}
		}

		return tasks.Snapshot{}, errNoSuchTask
	}

	managedTasks := Tasks{
		List:   func() []tasks.Snapshot { return listing },
		Status: find,
		Output: func(name string) (tasks.Snapshot, error) {
			snapshot, err := find(name)
			snapshot.Output = "Serving HTTP on localhost port 8080 ...\n"
			return snapshot, err
		},
		Stop: func(name string, _ string) error {
			if _, err := find(name); err != nil {
				return err
			}
			actions.stopped = append(actions.stopped, name)
			return nil
		},
		Restart: func(name string) (tasks.Snapshot, error) {
			snapshot, err := find(name)
			if err != nil {
				return snapshot, err
			}
			actions.restarted = append(actions.restarted, name)
			snapshot.Run++
			snapshot.State = tasks.Queued
			return snapshot, nil
		},
		Discard: func(name string) (tasks.Snapshot, error) {
			snapshot, err := find(name)
			if err != nil {
				return snapshot, err
			}
			actions.discarded = append(actions.discarded, name)
			if isLiveTask(snapshot) {
				snapshot.State = tasks.Stopped
			}
			return snapshot, nil
		},
		PruneFinished: func() []string {
			pruned := []string{}
			for _, snapshot := range listing {
				if !isLiveTask(snapshot) {
					pruned = append(pruned, snapshot.ID)
				}
			}

			return pruned
		},
		DescribeCall: describeFixtureCall,
	}

	return managedTasks, actions
}

func invokeTaskCommand(t *testing.T, managedTasks Tasks, input string) (*commandTestContext, error) {
	t.Helper()

	registry := newCommandRegistry(t, commandEnvironment{tasks: managedTasks})
	invocation, isFound := registry.Find(input)
	if !isFound {
		t.Fatalf("did not find %s", input)
	}
	context := newCommandTestContext(t)

	return context, invocation.Command.Run(context, invocation.Arguments)
}

func TestTheTasksCommandListsEveryTask(t *testing.T) {
	managedTasks, _ := fixtureTasks()

	context, err := invokeTaskCommand(t, managedTasks, "/tasks")
	if err != nil {
		t.Fatal(err)
	}

	if !context.isListing || !context.hasOwnStyle {
		t.Error("tasks were not drawn as a listing in their own style")
	}
	if !strings.Contains(context.notice, style.Subject("docs")) {
		t.Error("task name was not styled as a subject")
	}

	notice := style.Plain(context.notice)
	for _, wanted := range []string{"docs", "running", "$ python3 -m http.server 8080", "build", "failed", "exit status 1"} {
		if !strings.Contains(notice, wanted) {
			t.Errorf("got %q, want it to carry %q", notice, wanted)
		}
	}

	lines := strings.Split(notice, "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want a heading and two tasks", len(lines))
	}
	if docsStatusAt, buildStatusAt := strings.Index(lines[1], "running"), strings.Index(lines[2], "failed"); docsStatusAt != buildStatusAt {
		t.Errorf("status columns start at %d and %d, want them aligned", docsStatusAt, buildStatusAt)
	}
	if docsCommandAt, buildCommandAt := strings.Index(lines[1], "$"), strings.Index(lines[2], "$"); docsCommandAt != buildCommandAt {
		t.Errorf("command columns start at %d and %d, want them aligned", docsCommandAt, buildCommandAt)
	}
}

func TestTaskOutcomesCarryTheirStateStyle(t *testing.T) {
	for _, test := range []struct {
		state tasks.State
		paint style.Style
	}{
		{state: tasks.Done, paint: style.Dim},
		{state: tasks.Stopped, paint: style.Dim},
		{state: tasks.Running, paint: style.Normal},
		{state: tasks.Queued, paint: style.Normal},
		{state: tasks.Failed, paint: style.Failure},
		{state: tasks.TimedOut, paint: style.Failure},
	} {
		snapshot := tasks.Snapshot{State: test.state}
		want := test.paint(taskOutcome(snapshot, false))
		if got := styleTaskOutcome(snapshot); got != want {
			t.Errorf("%s outcome was %q, want %q", test.state, got, want)
		}
	}
}

func TestTheTasksCommandSaysSoWhenThereAreNone(t *testing.T) {
	managedTasks, _ := fixtureTasks()
	managedTasks.List = func() []tasks.Snapshot { return nil }

	context, err := invokeTaskCommand(t, managedTasks, "/tasks")
	if err != nil {
		t.Fatal(err)
	}

	if notice := context.notice; notice != "No background tasks." {
		t.Errorf("got %q, want it to say there are none", notice)
	}
}

func TestTheTaskCommandReportsOneStatus(t *testing.T) {
	managedTasks, _ := fixtureTasks()

	context, err := invokeTaskCommand(t, managedTasks, "/task status docs")
	if err != nil {
		t.Fatal(err)
	}

	if notice := context.notice; notice != "docs: running for 30s" {
		t.Errorf("got %q, want the task's own status", notice)
	}
}

func TestTheTaskCommandShowsWhatATaskPrinted(t *testing.T) {
	managedTasks, _ := fixtureTasks()

	context, err := invokeTaskCommand(t, managedTasks, "/task cat docs")
	if err != nil {
		t.Fatal(err)
	}

	if notice := context.notice; !strings.HasPrefix(notice, "docs: running") || !strings.HasSuffix(notice, "\nServing HTTP on localhost port 8080 ...") {
		t.Errorf("got %q, want the status and the task's output", notice)
	}
}

func TestATaskThatPrintedNothingSaysSo(t *testing.T) {
	managedTasks, _ := fixtureTasks()
	managedTasks.Output = func(string) (tasks.Snapshot, error) {
		return tasks.Snapshot{ID: "docs", State: tasks.Running, Output: "   \n"}, nil
	}

	context, err := invokeTaskCommand(t, managedTasks, "/task cat docs")
	if err != nil {
		t.Fatal(err)
	}

	if notice := context.notice; notice != "docs: running with no output" {
		t.Errorf("got %q, want the no-output marker on the status line", notice)
	}
}

func TestStoppingATaskFromTheKeyboardTellsTheModel(t *testing.T) {
	managedTasks, actions := fixtureTasks()

	context, err := invokeTaskCommand(t, managedTasks, "/task stop docs")
	if err != nil {
		t.Fatal(err)
	}

	if len(actions.stopped) != 1 || actions.stopped[0] != "docs" {
		t.Errorf("got stopped %v, want the named task alone", actions.stopped)
	}
	if len(context.events) != 1 || context.events[0].Kind != caps.TaskStop {
		t.Fatalf("got events %#v, want the stop recorded", context.events)
	}
	notice, isShown := caps.TaskStopNotice(context.events[0])
	if !isShown || !strings.Contains(notice, "docs") || !strings.Contains(notice, "keyboard") {
		t.Errorf("got %q, want the recorded stop to name the task and the keyboard", notice)
	}
}

func TestStoppingAFinishedTaskRecordsNothing(t *testing.T) {
	managedTasks, actions := fixtureTasks()

	context, err := invokeTaskCommand(t, managedTasks, "/task stop build")
	if err != nil {
		t.Fatal(err)
	}

	if len(actions.stopped) != 0 || len(context.events) != 0 {
		t.Errorf("stopped %v and recorded %#v, want a finished task left alone", actions.stopped, context.events)
	}
	if !strings.HasPrefix(context.notice, "build: failed") {
		t.Errorf("got %q, want the finished task's status", context.notice)
	}
}

func TestATaskCanBeRestartedFromTheKeyboard(t *testing.T) {
	managedTasks, actions := fixtureTasks()

	context, err := invokeTaskCommand(t, managedTasks, "/task restart build")
	if err != nil {
		t.Fatal(err)
	}

	if len(actions.restarted) != 1 || actions.restarted[0] != "build" {
		t.Errorf("got restarted %v, want the named task alone", actions.restarted)
	}
	if context.success != "Restarted build as run 2." {
		t.Errorf("got %q, want the restart confirmed with its run", context.success)
	}
}

func TestDiscardingALiveTaskTellsTheModelItStopped(t *testing.T) {
	managedTasks, actions := fixtureTasks()

	context, err := invokeTaskCommand(t, managedTasks, "/task discard docs")
	if err != nil {
		t.Fatal(err)
	}

	if len(actions.discarded) != 1 || actions.discarded[0] != "docs" {
		t.Errorf("got discarded %v, want the named task alone", actions.discarded)
	}
	if len(context.events) != 1 || context.events[0].Kind != caps.TaskStop {
		t.Errorf("got events %#v, want the stop recorded", context.events)
	}
	if context.success != "Discarded docs: stopped." {
		t.Errorf("got %q, want the discard confirmed", context.success)
	}
}

func TestDiscardingAFinishedTaskRecordsNothing(t *testing.T) {
	managedTasks, _ := fixtureTasks()

	context, err := invokeTaskCommand(t, managedTasks, "/task discard build")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(context.success, "build") {
		t.Errorf("got %q, want it to confirm the task was discarded", context.success)
	}
	if len(context.events) != 0 {
		t.Errorf("got events %#v, want discarding a finished task to record nothing", context.events)
	}
}

func TestAnUnknownTaskIsRefused(t *testing.T) {
	managedTasks, _ := fixtureTasks()

	for _, input := range []string{
		"/task status ghost",
		"/task cat ghost",
		"/task stop ghost",
		"/task restart ghost",
		"/task discard ghost",
	} {
		if _, err := invokeTaskCommand(t, managedTasks, input); !errors.Is(err, errNoSuchTask) {
			t.Errorf("%s gave %v, want the task not to be found", input, err)
		}
	}
}

func TestTheTaskCommandRefusesAnIncompleteInvocation(t *testing.T) {
	managedTasks, _ := fixtureTasks()

	for _, input := range []string{"/task", "/task status", "/task cat", "/task output docs", "/task docs", "/task frobnicate docs", "/task prune docs"} {
		if _, err := invokeTaskCommand(t, managedTasks, input); !slash.IsUsageError(err) {
			t.Errorf("%s gave %v, want the usage to be shown", input, err)
		}
	}
}

func TestTheTaskCommandsAreAbsentWithoutAManager(t *testing.T) {
	registry := newCommandRegistry(t, commandEnvironment{})

	for _, input := range []string{"/tasks", "/task status docs"} {
		if _, isFound := registry.Find(input); isFound {
			t.Errorf("%s was offered with no task manager", input)
		}
	}
}

func TestPruningSweepsEveryFinishedTask(t *testing.T) {
	managedTasks, _ := fixtureTasks()

	context, err := invokeTaskCommand(t, managedTasks, "/task prune")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(context.success, "build") {
		t.Errorf("got %q, want the sweep to name what it pruned", context.success)
	}
	if strings.Contains(context.success, "docs") {
		t.Errorf("got %q, want the running task left alone", context.success)
	}
}
