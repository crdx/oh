package tasks

import (
	"context"
	"time"
)

func ActiveTimeout(ctx context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	boundedContext, cancel := context.WithCancelCause(ctx)
	workFinished := make(chan struct{})
	clock := newActiveClock(boundedContext)
	go func() {
		if activeTimeExpired(boundedContext, clock, limit, workFinished) {
			cancel(context.DeadlineExceeded)
		}
	}()
	return boundedContext, func() {
		close(workFinished)
		cancel(nil)
	}
}
