package tasks_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/internal/waiting"
	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/tool"
)

func holdUntilStopped(ctx context.Context) (tool.ToolCallResult, error) {
	<-ctx.Done()
	return tool.ToolCallResult{Output: "old run"}, ctx.Err()
}

func TestAStopSupersedesAnInProgressRestart(t *testing.T) {
	for _, isRunning := range []bool{false, true} {
		for _, withdrawAccess := range []bool{false, true} {
			t.Run(fmt.Sprintf("running=%t/withdraw=%t", isRunning, withdrawAccess), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					manager := tasks.New(t.Context())
					defer manager.Close()
					invocation := &tasks.Invocation{Arguments: `{}`}
					_, err := manager.Start(t.Context(), "worker", "bash", true, false,
						func(ctx context.Context) (tool.ToolCallResult, error) {
							if isRunning {
								return holdUntilStopped(ctx)
							}
							return tool.ToolCallResult{Output: "finished"}, nil
						}, "old access", tasks.StartOptions{Invocation: invocation})
					if err != nil {
						t.Fatal(err)
					}
					synctest.Wait()
					preparationStarted := make(chan struct{})
					releasePreparation := make(chan struct{})
					newRuns := 0
					manager.BindTool("bash", func(context.Context, tasks.Invocation) (tasks.PreparationPlan, error) {
						close(preparationStarted)
						<-releasePreparation
						return tasks.PreparationPlan{Execute: func(context.Context) (tool.ToolCallResult, error) {
							newRuns++
							return tool.ToolCallResult{}, nil
						}}, nil
					})
					restarted := make(chan error, 1)
					go func() { _, failure := manager.Restart(t.Context(), "worker"); restarted <- failure }()
					<-preparationStarted
					if withdrawAccess {
						stopped := manager.StopWhere(func(_ string, policy any) bool { return policy == "old access" }, "access was withdrawn")
						if !slices.Equal(stopped, []string{"worker"}) {
							t.Errorf("withdrawal did not stop the pending restart: %v", stopped)
						}
					} else if err := manager.Stop("worker", "the user stopped it"); err != nil {
						t.Fatal(err)
					}
					close(releasePreparation)
					if err := <-restarted; !errors.Is(err, context.Canceled) {
						t.Errorf("stopped restart returned %v", err)
					}
					synctest.Wait()
					if newRuns != 0 {
						t.Errorf("stop was followed by %d replacement runs", newRuns)
					}
				})
			})
		}
	}
}

func TestAFullQueueRefusesRestartBeforeStoppingTheWorkingTask(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		for index := range tasks.DefaultMaximumRunning {
			_, err := manager.Start(t.Context(), fmt.Sprintf("active-%d", index), "bash", true, false,
				holdUntilStopped, nil, tasks.StartOptions{Invocation: &tasks.Invocation{Arguments: `{}`}})
			if err != nil {
				t.Fatal(err)
			}
		}
		synctest.Wait()
		for index := range tasks.MaxQueued {
			if _, err := manager.Start(t.Context(), fmt.Sprintf("queued-%d", index), "bash", true, false, holdUntilStopped, nil); err != nil {
				t.Fatal(err)
			}
		}
		manager.BindTool("bash", func(context.Context, tasks.Invocation) (tasks.PreparationPlan, error) {
			return tasks.PreparationPlan{Execute: holdUntilStopped}, nil
		})
		_, err := manager.Restart(t.Context(), "active-0")
		if err == nil || !strings.Contains(err.Error(), "queued") {
			t.Errorf("full queue did not refuse restart before stopping: %v", err)
		}
		current, err := manager.Status("active-0")
		if err != nil || current.Run != 1 || current.State != tasks.Running {
			t.Errorf("full queue damaged the working task: %+v, %v", current, err)
		}
	})
}

func TestDiscardRefusesAHeldTaskBeforeStoppingIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		if _, err := manager.Start(t.Context(), "held", "bash", true, false, holdUntilStopped, nil); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		_, release, err := manager.Hold("held")
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		if _, err := manager.Discard(t.Context(), "held"); err == nil {
			t.Error("discard accepted a task already in use")
		}
		current, err := manager.Status("held")
		if err != nil || current.State != tasks.Running {
			t.Errorf("failed discard stopped its target: %+v, %v", current, err)
		}
	})
}

func TestNewHoldsCannotInterruptAnInProgressDiscard(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		stopReceived := make(chan struct{})
		finishStop := make(chan struct{})
		if _, err := manager.Start(t.Context(), "remove", "bash", true, false,
			func(ctx context.Context) (tool.ToolCallResult, error) {
				<-ctx.Done()
				close(stopReceived)
				<-finishStop
				return tool.ToolCallResult{}, ctx.Err()
			}, nil); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		discarded := make(chan error, 1)
		go func() { _, failure := manager.Discard(t.Context(), "remove"); discarded <- failure }()
		<-stopReceived
		_, release, err := manager.Hold("remove")
		if err == nil {
			t.Error("a new hold entered a task being discarded")
			release()
		}
		close(finishStop)
		if err := <-discarded; err != nil {
			t.Errorf("discard failed after stopping: %v", err)
		}
	})
}

func TestRespawnQuickRunDetectionExcludesApprovalTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		ctx, _ := waiting.Track(t.Context())
		tooManyRuns := make(chan struct{})
		runs := 0
		_, err := manager.Start(ctx, "quick", "bash", true, true, func(ctx context.Context) (tool.ToolCallResult, error) {
			runs++
			if runs == tasks.QuickRunsTolerated+1 {
				close(tooManyRuns)
			}
			finishWaiting := waiting.Begin(ctx)
			time.Sleep(10 * time.Second)
			finishWaiting()
			return tool.ToolCallResult{Output: "quick exit"}, nil
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		over, release, err := manager.Hold("quick")
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		select {
		case <-over:
		case <-tooManyRuns:
			t.Error("approval time bypassed the quick-run breaker")
			if err := manager.Stop("quick", "the user stopped it"); err != nil {
				t.Fatal(err)
			}
			<-over
		}
		if runs != tasks.QuickRunsTolerated {
			t.Errorf("respawn ran %d times, want %d", runs, tasks.QuickRunsTolerated)
		}
	})
}

func TestGeneratedAndChosenTaskNamesAreAllocatedAtomically(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		var group sync.WaitGroup
		results := make(chan tasks.Snapshot, 12)
		failures := make(chan error, 12)
		for index := range 12 {
			group.Go(func() {
				name := ""
				if index%2 == 0 {
					name = "task-1"
				}
				snapshot, err := manager.Start(t.Context(), "", "bash", true, false, holdUntilStopped, nil, tasks.StartOptions{Name: name})
				results <- snapshot
				failures <- err
			})
		}
		group.Wait()
		names := make(map[string]bool)
		for range 12 {
			if err := <-failures; err != nil {
				t.Errorf("valid concurrent launch failed: %v", err)
			}
			snapshot := <-results
			if names[snapshot.ID] {
				t.Errorf("duplicate task name: %q", snapshot.ID)
			}
			names[snapshot.ID] = true
		}
	})
}

func TestCheckpointsAndShutdownRetainLiveOutput(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		_, err := manager.Start(t.Context(), "logs", "bash", true, false,
			func(ctx context.Context) (tool.ToolCallResult, error) {
				_, _ = tool.LiveOutput(ctx).Write([]byte("partial progress"))
				<-ctx.Done()
				return tool.ToolCallResult{}, ctx.Err()
			}, nil)
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		checkpoint := manager.Checkpoint()
		if len(checkpoint) != 1 || checkpoint[0].Output != "partial progress" {
			t.Errorf("checkpoint lost live output: %+v", checkpoint)
		}
		manager.Close()
		checkpoint = manager.Checkpoint()
		if len(checkpoint) != 1 || checkpoint[0].State != tasks.Stopped || checkpoint[0].Output != "partial progress" {
			t.Errorf("shutdown lost live output: %+v", checkpoint)
		}
		conclusions := manager.Drain()
		if len(conclusions) != 1 || !conclusions[0].IsSessionEnded || !conclusions[0].IsSilent {
			t.Errorf("shutdown did not classify its stopped task: %+v", conclusions)
		}
	})
}

func TestShutdownDoesNotRelabelAnEarlierCompletion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		_, err := manager.Start(t.Context(), "done", "bash", true, false,
			func(context.Context) (tool.ToolCallResult, error) {
				return tool.ToolCallResult{Output: "finished"}, nil
			}, nil)
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		manager.Close()
		conclusions := manager.Drain()
		if len(conclusions) != 1 || conclusions[0].IsSessionEnded || conclusions[0].IsSilent || conclusions[0].Snapshot.State != tasks.Done {
			t.Errorf("shutdown relabelled a completed task: %+v", conclusions)
		}
	})
}

func TestAQueuedForegroundCallDetachesAfterTheForegroundLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		for index := range tasks.DefaultMaximumRunning {
			if _, err := manager.Start(t.Context(), fmt.Sprintf("hold-%d", index), "bash", true, false, holdUntilStopped, nil); err != nil {
				t.Fatal(err)
			}
		}
		synctest.Wait()
		snapshot, err := manager.Start(t.Context(), "", "bash", false, false, func(context.Context) (tool.ToolCallResult, error) {
			return tool.ToolCallResult{Output: "late"}, nil
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, isBackground, err := manager.WaitOrDetach(t.Context(), snapshot.ID)
		if err != nil || !isBackground {
			t.Fatalf("a queued foreground call returned background=%t, %v", isBackground, err)
		}
		listed := slices.ContainsFunc(manager.List(), func(listed tasks.Snapshot) bool {
			return listed.ID == snapshot.ID && listed.State == tasks.Queued
		})
		if !listed {
			t.Errorf("the detached call is not a queued background task: %+v", manager.List())
		}
	})
}

func TestARefusedRestartLeavesTheRunningTaskAlone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		if _, err := manager.Start(t.Context(), "server", "bash", true, false, holdUntilStopped, nil,
			tasks.StartOptions{Invocation: &tasks.Invocation{Arguments: `{}`}}); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		withheld := errors.New("access withdrawn")
		manager.BindTool("bash", func(context.Context, tasks.Invocation) (tasks.PreparationPlan, error) {
			return tasks.PreparationPlan{Execute: holdUntilStopped, Policy: tasks.PolicySource(func() (any, error) { return nil, withheld })}, nil
		})
		if _, err := manager.Restart(t.Context(), "server"); !errors.Is(err, withheld) {
			t.Errorf("restart returned %v, want the access refusal", err)
		}
		if snapshot, err := manager.Status("server"); err != nil || snapshot.State != tasks.Running {
			t.Errorf("a refused restart stopped the running task: %+v, %v", snapshot, err)
		}
	})
}

func TestAPortRevokedDuringARestartIsNotPublishedAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		publications := 0
		publish := func(_ context.Context, _ string, port uint16) (string, error) {
			if port == 0 {
				return "", errors.New("there is no port to publish")
			}
			publications++
			return "published", nil
		}
		execute := func(ctx context.Context) (tool.ToolCallResult, error) {
			if err := tool.Launched(ctx); err != nil {
				return tool.ToolCallResult{}, err
			}
			return holdUntilStopped(ctx)
		}
		if _, err := manager.Start(t.Context(), "docs", "bash", true, false, execute, nil, tasks.StartOptions{
			Invocation: &tasks.Invocation{Arguments: `{}`, ForwardPort: 8000},
			Publish:    publish,
		}); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		release := make(chan struct{})
		manager.BindTool("bash", func(context.Context, tasks.Invocation) (tasks.PreparationPlan, error) {
			<-release
			return tasks.PreparationPlan{Execute: execute, Publish: publish}, nil
		})
		restarted := make(chan error, 1)
		go func() {
			_, err := manager.Restart(t.Context(), "docs")
			restarted <- err
		}()
		synctest.Wait()
		if !manager.ForgetPort("docs", 8000) {
			t.Fatal("the task did not hold its port")
		}
		close(release)
		if err := <-restarted; err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		publication, state := manager.AwaitPublication(t.Context(), "docs", time.Second)
		if publications != 1 || state != tasks.PublicationWithdrawn || publication != "" {
			t.Errorf("the restart published %d times, state %d, %q", publications, state, publication)
		}
		if got := tasks.PublicationReceipt(publication, state, 8000); !strings.Contains(got, "not forwarded because its forward was removed") {
			t.Errorf("receipt %q", got)
		}
	})
}
