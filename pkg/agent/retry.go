package agent

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const retryBudget = 15 * time.Minute

func (self *Agent) retryWait(err error, attempt int, spentTime time.Duration) (time.Duration, bool) {
	var retriable Retriable

	if !errors.As(err, &retriable) || !retriable.Retriable() {
		return 0, false
	}

	wait := max(retryWaitForAttempt(attempt), retriable.RetryAfter())

	if spentTime+wait > retryBudget {
		return 0, false
	}

	return wait, true
}

func retryWaitForAttempt(attempt int) time.Duration {
	switch attempt {
	case 1:
		return time.Second
	case 2:
		return 5 * time.Second
	case 3:
		return 10 * time.Second
	case 4:
		return 30 * time.Second
	default:
		return time.Minute
	}
}

func isResumable(err error) bool {
	var resumable Resumable

	return errors.As(err, &resumable) && resumable.Resumable()
}

func faultedCall(err error) (ToolCall, bool) {
	var faultedCall CallFaulted

	if !errors.As(err, &faultedCall) {
		return ToolCall{}, false
	}

	return faultedCall.FaultedCall(), true
}

type rewind struct {
	state State
	items []json.RawMessage
}

func rewindOf(provider Provider) *rewind {
	state, isRewindable := provider.(State)
	if !isRewindable {
		return nil
	}

	return &rewind{state: state, items: cloneState(state.Dump())}
}

func (self *rewind) restore() {
	if self == nil {
		return
	}

	self.state.Load(cloneState(self.items))
}

func (self *Agent) TakeRetryWaitsAtOnce() {
	self.retryWaitsPassAtOnce = true
}

func (self *Agent) TakeTimeFrom(clock func() time.Time) {
	self.now = clock
}

func (self *Agent) waitBeforeRetry(ctx context.Context, wait time.Duration) bool {
	if self.retryWaitsPassAtOnce {
		return ctx.Err() == nil
	}

	return waitFor(ctx, wait)
}

func waitFor(ctx context.Context, wait time.Duration) bool {
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
