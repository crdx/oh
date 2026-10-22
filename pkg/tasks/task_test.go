package tasks_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/internal/waiting"
	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/tool"
)

func TestAForegroundCallHandsOffWithoutRunningAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		started := make(chan struct{})
		release := make(chan struct{})
		runs := 0
		_, err := manager.Start(t.Context(), "one", "bash", false, false, func(context.Context) (tool.ToolCallResult, error) {
			runs++
			close(started)
			<-release
			return tool.ToolCallResult{Output: "done"}, nil
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		<-started
		startTime := time.Now()
		_, isBackground, err := manager.WaitOrDetach(t.Context(), "one")
		if err != nil || !isBackground {
			t.Fatalf("handoff returned background %t, failure %v", isBackground, err)
		}
		if elapsed := time.Since(startTime); elapsed != 30*time.Second {
			t.Errorf("handoff took %s, want 30s", elapsed)
		}
		close(release)
		synctest.Wait()
		conclusions := manager.Drain()
		if len(conclusions) != 1 || conclusions[0].Result.Output != "done" || runs != 1 {
			t.Errorf("got %v conclusions after %d executions", conclusions, runs)
		}
	})
}

func TestAnImmediateBackgroundCallCompletesOnlyOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		_, err := manager.Start(t.Context(), "one", "bash", true, false, func(context.Context) (tool.ToolCallResult, error) {
			return tool.ToolCallResult{Output: "done"}, nil
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, isBackground, err := manager.WaitOrDetach(t.Context(), "one")
		if err != nil || !isBackground {
			t.Fatalf("background call returned background %t, failure %v", isBackground, err)
		}
		synctest.Wait()
		if conclusions := manager.Drain(); len(conclusions) != 1 || conclusions[0].Result.Output != "done" {
			t.Errorf("missing completion: %v", conclusions)
		}
		if conclusions := manager.Drain(); len(conclusions) != 0 {
			t.Errorf("completion delivered twice: %v", conclusions)
		}
	})
}

func TestABackgroundCallEndsAtThirtyMinutes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		_, err := manager.Start(t.Context(), "one", "bash", true, false, func(ctx context.Context) (tool.ToolCallResult, error) {
			<-ctx.Done()
			return tool.ToolCallResult{Output: "partial"}, ctx.Err()
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		over, release, err := manager.Hold("one")
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		<-over
		conclusions := manager.Drain()
		if len(conclusions) != 1 || !errors.Is(conclusions[0].Failure, context.DeadlineExceeded) ||
			!strings.Contains(conclusions[0].Snapshot.Failure, "30m0s") {
			t.Errorf("wrong thirty-minute outcome: %v", conclusions)
		}
	})
}

func TestCancellingTheTurnBeforeHandoffStopsTheTask(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		turnContext, cancel := context.WithCancel(t.Context())
		_, err := manager.Start(turnContext, "one", "bash", false, false, func(ctx context.Context) (tool.ToolCallResult, error) {
			<-ctx.Done()
			return tool.ToolCallResult{}, ctx.Err()
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		_, isBackground, err := manager.WaitOrDetach(turnContext, "one")
		if !errors.Is(err, context.Canceled) || isBackground {
			t.Errorf("cancel returned background %t, failure %v", isBackground, err)
		}
		synctest.Wait()
		if snapshot, err := manager.Status("one"); err == nil {
			t.Errorf("cancel left a foreground task record: %+v", snapshot)
		}
	})
}

func TestApprovalTimeDoesNotConsumeForegroundOrBackgroundAllowance(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		callContext, _ := waiting.Track(t.Context())
		approvalStanding := make(chan struct{})
		approve := make(chan struct{})
		_, err := manager.Start(callContext, "one", "bash", false, false, func(ctx context.Context) (tool.ToolCallResult, error) {
			finishWaiting := waiting.Begin(ctx)
			close(approvalStanding)
			<-approve
			finishWaiting()
			<-ctx.Done()
			return tool.ToolCallResult{}, ctx.Err()
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		<-approvalStanding
		handedOff := make(chan bool, 1)
		go func() {
			_, isBackground, _ := manager.WaitOrDetach(callContext, "one")
			handedOff <- isBackground
		}()
		time.Sleep(31 * time.Minute)
		select {
		case <-handedOff:
			t.Error("approval consumed the foreground allowance")
		default:
		}
		close(approve)
		if isBackground := <-handedOff; !isBackground {
			t.Error("the call did not hand off after approval")
		}
		over, release, err := manager.Hold("one")
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		<-over
		if conclusions := manager.Drain(); len(conclusions) != 1 || !strings.Contains(conclusions[0].Snapshot.Failure, "30m0s") {
			t.Errorf("the background allowance was lost during approval: %v", conclusions)
		}
	})
}

func TestARespawningTaskKeepsItsIdentityAndStopsAfterQuickRuns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		runs := 0
		_, err := manager.Start(t.Context(), "watch", "bash", true, true, func(context.Context) (tool.ToolCallResult, error) {
			runs++
			return tool.ToolCallResult{Output: "run"}, nil
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		over, release, err := manager.Hold("watch")
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		<-over
		conclusions := manager.Drain()
		if runs != tasks.QuickRunsTolerated || len(conclusions) != runs {
			t.Fatalf("got %d runs and %d conclusions", runs, len(conclusions))
		}
		for index, conclusion := range conclusions {
			isLast := index == runs-1
			if conclusion.Snapshot.ID != "watch" || conclusion.Snapshot.Run != index+1 ||
				conclusion.IsFinal != isLast || conclusion.Snapshot.IsRespawnLimitReached != isLast {
				t.Errorf("run %d reported %+v", index+1, conclusion)
			}
		}
	})
}

func TestAWaitedOnTaskIsNotPrunedUntilReleased(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		_, err := manager.Start(t.Context(), "one", "bash", true, false, func(context.Context) (tool.ToolCallResult, error) {
			return tool.ToolCallResult{Output: "done"}, nil
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		over, release, err := manager.Hold("one")
		if err != nil {
			t.Fatal(err)
		}
		<-over
		if names := manager.PruneFinished(); len(names) != 0 {
			t.Errorf("pruned held task: %v", names)
		}
		release()
		if names := manager.PruneFinished(); len(names) != 1 || names[0] != "one" {
			t.Errorf("finished task not pruned: %v", names)
		}
	})
}

func TestStopWhereStopsOnlyTasksWhosePolicyMatches(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		started := make(chan struct{}, 2)
		release := make(chan struct{})
		execute := func(context.Context) (tool.ToolCallResult, error) {
			started <- struct{}{}
			<-release
			return tool.ToolCallResult{Output: "done"}, nil
		}
		if _, err := manager.Start(t.Context(), "a", "bash", true, false, execute, "writable"); err != nil {
			t.Fatal(err)
		}
		if _, err := manager.Start(t.Context(), "b", "bash", true, false, execute, "read-only"); err != nil {
			t.Fatal(err)
		}
		<-started
		<-started
		names := manager.StopWhere(func(_ string, policy any) bool { return policy == "writable" })
		if len(names) != 1 || names[0] != "a" {
			t.Fatalf("stopped %v, want [a]", names)
		}
		snapshot, err := manager.Status("a")
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.State != tasks.Stopping {
			t.Errorf("matched task is %s, want %s", snapshot.State, tasks.Stopping)
		}
		close(release)
		synctest.Wait()
		snapshot, err = manager.Status("a")
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.State != tasks.Stopped {
			t.Errorf("stopped task is %s, want %s", snapshot.State, tasks.Stopped)
		}
		if snapshotB, err := manager.Status("b"); err != nil || snapshotB.State != tasks.Done {
			t.Errorf("unmatched task is %s (%v), want %s", snapshotB.State, err, tasks.Done)
		}
	})
}

func TestAnApprovalStartingAfterExecutionDoesNotEndTheTimer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		callContext, _ := waiting.Track(t.Context())
		started := make(chan struct{})
		beginApproval := make(chan struct{})
		approvalStanding := make(chan struct{})
		finishApproval := make(chan struct{})
		_, err := manager.Start(callContext, "one", "bash", false, false, func(ctx context.Context) (tool.ToolCallResult, error) {
			close(started)
			<-beginApproval
			endWait := waiting.Begin(ctx)
			close(approvalStanding)
			<-finishApproval
			endWait()
			<-ctx.Done()
			return tool.ToolCallResult{}, ctx.Err()
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		<-started
		handedOff := make(chan bool, 1)
		go func() {
			_, isBackground, _ := manager.WaitOrDetach(callContext, "one")
			handedOff <- isBackground
		}()
		time.Sleep(tasks.ForegroundLimit / 2)
		close(beginApproval)
		<-approvalStanding
		time.Sleep(31 * time.Minute)
		select {
		case <-handedOff:
			t.Fatal("the foreground timer ended during approval")
		default:
		}
		close(finishApproval)
		if !<-handedOff {
			t.Error("the call did not hand off after approval")
		}
	})
}

func TestAForegroundResultIsNotListedAsBackgroundWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		_, err := manager.Start(t.Context(), "one", "bash", false, false, func(context.Context) (tool.ToolCallResult, error) {
			return tool.ToolCallResult{Output: "done"}, nil
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, isBackground, err := manager.WaitOrDetach(t.Context(), "one"); err != nil || isBackground {
			t.Fatalf("foreground result unexpectedly detached: %t, %v", isBackground, err)
		}
		if listed := manager.List(); len(listed) != 0 {
			t.Errorf("foreground result was retained in the background task list: %+v", listed)
		}
		if _, err := manager.Status("one"); err == nil {
			t.Error("a completed foreground call kept its task record")
		}
	})
}

func TestRestoringTasksStopsOutstandingWorkAndRetainsCompletedRuns(t *testing.T) {
	manager := tasks.New(t.Context())
	defer manager.Close()
	interrupted := manager.Restore([]tasks.Snapshot{
		{ID: "task-4", State: tasks.Queued, Run: 1, IsRespawning: true},
		{ID: "task-9", State: tasks.Running, Run: 3, IsRespawning: true},
		{ID: "task-11", State: tasks.Stopping, Run: 2},
		{ID: "task-12", State: tasks.Done, Run: 1, Output: "saved"},
	})
	if !slices.Equal(interrupted, []string{"task-4", "task-9", "task-11"}) {
		t.Errorf("interrupted %v", interrupted)
	}
	for _, id := range interrupted {
		snapshot, err := manager.Status(id)
		if err != nil || snapshot.State != tasks.Stopped || snapshot.IsRespawning {
			t.Errorf("%s restored as %+v, %v", id, snapshot, err)
		}
	}
	if snapshot, err := manager.Status("task-12"); err != nil || snapshot.State != tasks.Done || snapshot.Output != "saved" {
		t.Errorf("finished task changed on restore: %+v, %v", snapshot, err)
	}
	snapshot, err := manager.Start(t.Context(), "", "bash", true, false, func(context.Context) (tool.ToolCallResult, error) {
		return tool.ToolCallResult{}, nil
	}, nil)
	if err != nil || snapshot.ID != "task-13" {
		t.Errorf("new task %+v, %v collides with restored work", snapshot, err)
	}
}

func TestAnEndedSessionCannotStartAnotherTask(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	manager := tasks.New(ctx)
	defer manager.Close()
	cancel()
	isStarted := false
	_, err := manager.Start(t.Context(), "one", "bash", true, false, func(context.Context) (tool.ToolCallResult, error) {
		isStarted = true
		return tool.ToolCallResult{}, nil
	}, nil)
	if !errors.Is(err, tasks.ErrClosed) || isStarted {
		t.Errorf("cancelled manager started work: %v, %t", err, isStarted)
	}
}

func TestARespawnPreservesThePreviousRunOutcome(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		secondStarted := make(chan struct{})
		runs := 0
		_, err := manager.Start(t.Context(), "one", "bash", true, true, func(ctx context.Context) (tool.ToolCallResult, error) {
			runs++
			if runs == 1 {
				return tool.ToolCallResult{Output: "first output"}, nil
			}
			close(secondStarted)
			<-ctx.Done()
			return tool.ToolCallResult{}, ctx.Err()
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		<-secondStarted
		snapshot, err := manager.Status("one")
		if err != nil || len(snapshot.Previous) == 0 || snapshot.Previous[0].Output != "first output" {
			t.Errorf("previous output missing: %+v, %v", snapshot, err)
		}
		if err := manager.Stop("one"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestABackgroundTaskSignalsItsStartBeforeTheTurnEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		_, err := manager.Start(t.Context(), "one", "bash", true, false, func(ctx context.Context) (tool.ToolCallResult, error) {
			<-ctx.Done()
			return tool.ToolCallResult{}, ctx.Err()
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-manager.Changes():
		default:
			t.Fatal("the background task start was not reported")
		}
		if listing := manager.List(); len(listing) != 1 {
			t.Errorf("a started background task has no listing: %+v", listing)
		}
	})
}

func TestAnAlreadyCancelledCallNeverStartsBackgroundWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		callContext, cancel := context.WithCancel(t.Context())
		cancel()
		isStarted := false
		_, err := manager.Start(callContext, "one", "bash", true, false, func(context.Context) (tool.ToolCallResult, error) {
			isStarted = true
			return tool.ToolCallResult{}, nil
		}, nil)
		if !errors.Is(err, context.Canceled) || isStarted || len(manager.List()) > 0 {
			t.Errorf("a cancelled call started a task: %v, %t", err, isStarted)
		}
	})
}

func TestCancellationBeforeTheBackgroundReceiptStopsTheTask(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		callContext, cancel := context.WithCancel(t.Context())
		_, err := manager.Start(callContext, "one", "bash", true, false, func(ctx context.Context) (tool.ToolCallResult, error) {
			<-ctx.Done()
			return tool.ToolCallResult{}, ctx.Err()
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		_, isBackground, err := manager.WaitOrDetach(callContext, "one")
		if !errors.Is(err, context.Canceled) || isBackground {
			t.Errorf("a cancelled call received a background receipt: %v, %t", err, isBackground)
		}
		synctest.Wait()
		if snapshot, err := manager.Status("one"); err != nil || snapshot.State != tasks.Stopped {
			t.Errorf("the cancelled background task did not stop: %+v, %v", snapshot, err)
		}
	})
}

func TestACompletedForegroundCallSurvivesConcurrentCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		turnContext, cancel := context.WithCancel(t.Context())
		_, err := manager.Start(turnContext, "one", "bash", false, false, func(context.Context) (tool.ToolCallResult, error) {
			return tool.ToolCallResult{Output: "finished before cancellation"}, nil
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		cancel()
		conclusion, isBackground, err := manager.WaitOrDetach(turnContext, "one")
		if err != nil || isBackground || conclusion.Result.Output != "finished before cancellation" {
			t.Errorf("settled result was lost: %+v, %t, %v", conclusion, isBackground, err)
		}
	})
}

type executionPolicyKey struct{}

type executionPolicy struct {
	name string
}

func (self executionPolicy) ForExecution(ctx context.Context) context.Context {
	return context.WithValue(ctx, executionPolicyKey{}, self.name)
}

func TestTaskPolicyIsBoundBeforeStartingAndSharedWithExecution(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		sourceCalls := 0
		started := make(chan string)
		_, err := manager.Start(t.Context(), "one", "bash", true, false, func(ctx context.Context) (tool.ToolCallResult, error) {
			name, _ := ctx.Value(executionPolicyKey{}).(string)
			started <- name
			<-ctx.Done()
			return tool.ToolCallResult{}, ctx.Err()
		}, tasks.PolicySource(func() (any, error) {
			sourceCalls++
			return executionPolicy{name: "first policy"}, nil
		}))
		if err != nil {
			t.Fatal(err)
		}
		if name := <-started; name != "first policy" || sourceCalls != 1 {
			t.Errorf("execution received policy %q after %d snapshots", name, sourceCalls)
		}
		stopped := manager.StopWhere(func(_ string, policy any) bool {
			bound, isBound := policy.(executionPolicy)
			return isBound && bound.name == "first policy"
		})
		if !slices.Equal(stopped, []string{"one"}) {
			t.Errorf("revocation did not find the execution policy: %v", stopped)
		}
	})
}

func TestPolicySnapshotFailurePreventsTaskAdmission(t *testing.T) {
	manager := tasks.New(t.Context())
	defer manager.Close()
	_, err := manager.Start(t.Context(), "one", "bash", true, false, func(context.Context) (tool.ToolCallResult, error) {
		t.Fatal("a task ran without a policy")
		return tool.ToolCallResult{}, nil
	}, tasks.PolicySource(func() (any, error) {
		return nil, errors.New("access unavailable")
	}))
	if err == nil || len(manager.List()) != 0 {
		t.Errorf("invalid policy admitted a task: %v", err)
	}
}

func TestRevocationCannotMissATaskBeingAdmitted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		entered := make(chan struct{})
		finishPolicy := make(chan struct{})
		admitted := make(chan error, 1)
		go func() {
			_, err := manager.Start(t.Context(), "one", "bash", true, false, func(ctx context.Context) (tool.ToolCallResult, error) {
				<-ctx.Done()
				return tool.ToolCallResult{}, ctx.Err()
			}, tasks.PolicySource(func() (any, error) {
				close(entered)
				<-finishPolicy
				return "granted-path", nil
			}))
			admitted <- err
		}()
		<-entered
		stopped := make(chan []string, 1)
		go func() {
			stopped <- manager.StopWhere(func(_ string, policy any) bool { return policy == "granted-path" })
		}()
		close(finishPolicy)
		if err := <-admitted; err != nil {
			t.Fatal(err)
		}
		if names := <-stopped; !slices.Equal(names, []string{"one"}) {
			t.Errorf("revocation missed the admitted task: %v", names)
		}
	})
}
