package task_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"testing/synctest"

	"crdx.org/oh/internal/stop"
	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/task"
)

func perform(t *testing.T, manager *tasks.Manager, arguments string) string {
	t.Helper()
	call, err := task.New(manager).Parse(arguments)
	if err != nil {
		t.Fatal(err)
	}
	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return result.Output
}

func TestTheTaskToolRetainsPreviousRunOutputAndPrunes(t *testing.T) {
	manager := tasks.New(t.Context())
	defer manager.Close()
	manager.Restore([]tasks.Snapshot{{ID: "task-3", State: tasks.Done, Run: 2, Output: "second", Previous: []tasks.RunOutcome{{Run: 1, State: tasks.Failed, Output: "first"}}}})
	output := perform(t, manager, `{"action":"output","name":"task-3"}`)
	if !strings.Contains(output, "first") || !strings.Contains(output, "second") {
		t.Errorf("run output omitted: %q", output)
	}
	if result := perform(t, manager, `{"action":"prune"}`); !strings.Contains(result, "task-3") {
		t.Errorf("prune did not name the task: %q", result)
	}
	if result := perform(t, manager, `{"action":"list"}`); !strings.Contains(result, "no background tasks") {
		t.Errorf("pruned task still listed: %q", result)
	}
}

func TestTaskActionsRenderCompleteSubjects(t *testing.T) {
	for _, test := range []struct {
		arguments task.Args
		subject   string
		qualifier string
	}{
		{task.Args{Action: task.ActionStatus, Name: "task-7"}, "task-7", ""},
		{task.Args{Action: task.ActionOutput, Name: "task-7"}, "task-7", ""},
		{task.Args{Action: task.ActionStop, Name: "task-7"}, "task-7", ""},
		{task.Args{Action: task.ActionRestart, Name: "task-7"}, "task-7", ""},
		{task.Args{Action: task.ActionDiscard, Name: "task-7"}, "task-7", ""},
		{task.Args{Action: task.ActionList}, "tasks", ""},
		{task.Args{Action: task.ActionPrune}, "finished tasks", ""},
	} {
		rendering := task.Describe(test.arguments)
		if rendering.Subject != test.subject || rendering.Qualifier != test.qualifier {
			t.Errorf("%s rendered %+v, want %q %q", test.arguments.Action, rendering, test.subject, test.qualifier)
		}
	}
}

func TestTheTaskInterfaceHasNoWaitActionOrDuration(t *testing.T) {
	manager := tasks.New(t.Context())
	defer manager.Close()
	subject := task.New(manager)
	action := subject.Schema().Find("action")
	if action == nil || !slices.Equal(action.Values, []string{"status", "output", "stop", "list", "prune", "restart", "discard"}) {
		t.Fatalf("unexpected task actions: %+v", action)
	}
	if subject.Schema().Find("seconds") != nil {
		t.Error("task schema still offers the removed wait duration")
	}
	for _, arguments := range []string{
		`{"action":"wait","name":"task-1"}`,
		`{"action":"status","name":"task-1","seconds":30}`,
	} {
		if _, err := subject.Parse(arguments); err == nil {
			t.Errorf("task accepted removed wait parameters: %s", arguments)
		}
	}
}

func TestTaskStatusNamesLaterRunsAndTheRespawnLimit(t *testing.T) {
	manager := tasks.New(t.Context())
	defer manager.Close()
	manager.Restore([]tasks.Snapshot{
		{ID: "first", State: tasks.Done, Run: 1},
		{ID: "second", State: tasks.Done, Run: 2},
		{ID: "watch", State: tasks.Done, Run: 3, IsRespawnLimitReached: true},
	})
	for name, want := range map[string]string{
		"first":  "first: done",
		"second": "second: done, run 2",
		"watch":  "watch: done, run 3, " + tasks.RespawnLimitReason(),
	} {
		if status := perform(t, manager, `{"action":"status","name":"`+name+`"}`); status != want {
			t.Errorf("status said %q, want %q", status, want)
		}
	}
}

func TestStoppingATaskNamesWhoStoppedIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		stopped := make(chan string, 1)
		if _, err := manager.Start(t.Context(), "watch", "bash", true, true, func(ctx context.Context) (tool.ToolCallResult, error) {
			<-ctx.Done()
			stopped <- stop.Sentence(ctx)
			return tool.ToolCallResult{}, ctx.Err()
		}, nil); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()

		if status := perform(t, manager, `{"action":"stop","name":"watch"}`); status != "watch: stopping" {
			t.Errorf("got %q, want the stop without a promise to respawn", status)
		}
		if sentence := <-stopped; sentence != "the agent stopped it" {
			t.Errorf("got %q, want the agent named as the reason", sentence)
		}
	})
}
