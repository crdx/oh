package tasks_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"testing/synctest"

	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/tool"
)

func TestNamedTasksAreNumberedWithoutLosingTheirName(t *testing.T) {
	if tasks.NamePattern() != "[a-z0-9-]{1,10}" || tasks.IDPattern() != "[a-z0-9-]{1,32}" {
		t.Fatalf("unexpected task patterns %q and %q", tasks.NamePattern(), tasks.IDPattern())
	}
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		for _, expected := range []string{"abcdefghij", "abcdefghij-1", "abcdefghij-2"} {
			snapshot, err := manager.Start(t.Context(), "", "bash", true, false,
				func(ctx context.Context) (tool.ToolCallResult, error) {
					<-ctx.Done()
					return tool.ToolCallResult{}, ctx.Err()
				}, nil, tasks.StartOptions{Name: "abcdefghij"})
			if err != nil || snapshot.ID != expected {
				t.Errorf("named start returned %+v, %v; want %s", snapshot, err, expected)
			}
		}
		for _, name := range []string{"UPPER", "two words", "elevenchars", "bad_name"} {
			if err := tasks.ValidateName(name); err == nil {
				t.Errorf("invalid task name was accepted: %q", name)
			}
		}
		if err := tasks.ValidateID("abcdefghij-12"); err != nil {
			t.Errorf("a numbered task id was refused: %v", err)
		}
		for _, id := range []string{"", "UPPER", "bad_id", strings.Repeat("a", tasks.IDLengthLimit+1)} {
			if err := tasks.ValidateID(id); err == nil {
				t.Errorf("invalid task id was accepted: %q", id)
			}
		}
	})
}

func TestAGeneratedTaskIDNamesItsToolAndSkipsRecordedIDs(t *testing.T) {
	manager := tasks.New(t.Context())
	defer manager.Close()
	manager.Reserve([]string{"lookup-2"})
	snapshot, err := manager.Start(t.Context(), "", "lookup", true, false, func(context.Context) (tool.ToolCallResult, error) {
		return tool.ToolCallResult{}, nil
	}, nil)
	if err != nil || snapshot.ID != "lookup-3" {
		t.Errorf("generated %+v, %v; want lookup-3", snapshot, err)
	}

	fallback, err := manager.Start(t.Context(), "", "weather_forecast", true, false, func(context.Context) (tool.ToolCallResult, error) {
		return tool.ToolCallResult{}, nil
	}, nil)
	if err != nil || fallback.ID != "task-1" {
		t.Errorf("fallback generated %+v, %v; want task-1", fallback, err)
	}
}

func TestLiveOutputIsReadableBeforeTheTaskEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		release := make(chan struct{})
		_, err := manager.Start(t.Context(), "build", "bash", true, false,
			func(ctx context.Context) (tool.ToolCallResult, error) {
				_, _ = tool.LiveOutput(ctx).Write([]byte("first phase\n"))
				<-release
				return tool.ToolCallResult{Output: "first phase\nfinished"}, nil
			}, nil)
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		live, err := manager.Output("build")
		if err != nil || live.State != tasks.Running || live.Output != "first phase\n" {
			t.Errorf("running output is %+v, %v", live, err)
		}
		close(release)
		synctest.Wait()
		finished, err := manager.Output("build")
		if err != nil || finished.State != tasks.Done || !strings.Contains(finished.Output, "finished") {
			t.Errorf("finished output is %+v, %v", finished, err)
		}
	})
}

func TestRememberedInvocationsRestartByNameAfterRestoration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		invocation := &tasks.Invocation{Arguments: `{"command":"echo remembered"}`}
		_, err := manager.Start(t.Context(), "build", "bash", true, false,
			func(context.Context) (tool.ToolCallResult, error) {
				return tool.ToolCallResult{Output: "first"}, nil
			}, nil, tasks.StartOptions{Invocation: invocation})
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		var snapshots []tasks.Snapshot
		encoded, err := json.Marshal(manager.List())
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(encoded, &snapshots); err != nil {
			t.Fatal(err)
		}
		restored := tasks.New(t.Context())
		defer restored.Close()
		restored.Restore(snapshots)
		calls := 0
		restored.BindTool("bash", func(_ context.Context, remembered tasks.Invocation) (tasks.PreparationPlan, error) {
			if remembered.Arguments != invocation.Arguments {
				t.Errorf("remembered %q, want %q", remembered.Arguments, invocation.Arguments)
			}
			return tasks.PreparationPlan{Execute: func(context.Context) (tool.ToolCallResult, error) {
				calls++
				return tool.ToolCallResult{Output: "restarted"}, nil
			}}, nil
		})
		started, err := restored.Restart(t.Context(), "build")
		if err != nil || started.ID != "build" || started.Run != 2 {
			t.Fatalf("restart returned %+v, %v", started, err)
		}
		synctest.Wait()
		finished, err := restored.Output("build")
		if err != nil || calls != 1 || finished.Output != "restarted" || len(finished.Previous) != 1 || finished.Previous[0].Output != "first" {
			t.Errorf("restart lost history: %+v, %v, %d calls", finished, err, calls)
		}
	})
}

func TestRestartRechecksAccessWithoutLosingThePreviousResult(t *testing.T) {
	manager := tasks.New(t.Context())
	defer manager.Close()
	manager.Restore([]tasks.Snapshot{{ID: "build", ToolName: "bash", State: tasks.Done, Output: "old output", Invocation: &tasks.Invocation{Arguments: `{}`}}})
	withheld := errors.New("access withdrawn")
	manager.BindTool("bash", func(context.Context, tasks.Invocation) (tasks.PreparationPlan, error) {
		return tasks.PreparationPlan{Execute: func(context.Context) (tool.ToolCallResult, error) {
			t.Error("a refused restart executed")
			return tool.ToolCallResult{}, nil
		}, Policy: tasks.PolicySource(func() (any, error) { return nil, withheld })}, nil
	})
	if _, err := manager.Restart(t.Context(), "build"); !errors.Is(err, withheld) {
		t.Errorf("restart returned %v, want access refusal", err)
	}
	if snapshot, err := manager.Output("build"); err != nil || snapshot.Output != "old output" {
		t.Errorf("refused restart lost the prior result: %+v, %v", snapshot, err)
	}
}

func TestDiscardStopsAndRemovesOnlyTheNamedTask(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		manager.Restore([]tasks.Snapshot{{ID: "keep", State: tasks.Done, Output: "kept output"}})
		_, err := manager.Start(t.Context(), "discard", "bash", true, false,
			func(ctx context.Context) (tool.ToolCallResult, error) {
				<-ctx.Done()
				return tool.ToolCallResult{State: json.RawMessage(`{"changed":true}`)}, ctx.Err()
			}, nil)
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		removed, err := manager.Discard(t.Context(), "discard")
		if err != nil || removed.State != tasks.Stopped {
			t.Errorf("discard returned %+v, %v", removed, err)
		}
		if _, err := manager.Status("discard"); err == nil {
			t.Error("discarded task is still addressable")
		}
		if listing := manager.List(); len(listing) != 1 || listing[0].ID != "keep" {
			t.Errorf("discard removed another task: %+v", listing)
		}
		conclusions := manager.Drain()
		if len(conclusions) != 1 || !conclusions[0].IsSilent || len(conclusions[0].Result.State) == 0 {
			t.Errorf("discard announced completion or lost tool state: %+v", conclusions)
		}
	})
}

func TestRestartStopsTheCurrentRunBeforeStartingTheNext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		hasStopped := false
		_, err := manager.Start(t.Context(), "server", "bash", true, false,
			func(ctx context.Context) (tool.ToolCallResult, error) {
				<-ctx.Done()
				hasStopped = true
				return tool.ToolCallResult{Output: "old run"}, ctx.Err()
			}, nil, tasks.StartOptions{Invocation: &tasks.Invocation{Arguments: `{}`}})
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		manager.BindTool("bash", func(context.Context, tasks.Invocation) (tasks.PreparationPlan, error) {
			return tasks.PreparationPlan{Execute: func(context.Context) (tool.ToolCallResult, error) {
				if !hasStopped {
					t.Error("restart overlapped its previous process")
				}
				return tool.ToolCallResult{Output: "new run"}, nil
			}}, nil
		})
		if _, err := manager.Restart(t.Context(), "server"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		finished, err := manager.Output("server")
		if err != nil || finished.Run != 2 || finished.Output != "new run" || len(finished.Previous) != 1 || finished.Previous[0].Output != "old run" {
			t.Errorf("active restart lost either run: %+v, %v", finished, err)
		}
	})
}
