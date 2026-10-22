package tasks

import (
	"context"
	"time"

	"crdx.org/oh/internal/waiting"
)

type activeClock struct {
	startedAt     time.Time
	waitedAtStart time.Duration
	slots         *slotWait
	queuedAtStart time.Duration
}

func newActiveClock(ctx context.Context) activeClock {
	return activeClock{startedAt: time.Now(), waitedAtStart: waiting.Observe(ctx).WaitTime}
}

func newRunClock(ctx context.Context, slots *slotWait) activeClock {
	clock := newActiveClock(ctx)
	clock.slots = slots
	clock.queuedAtStart = slots.get()
	return clock
}

func (self activeClock) withoutSlotWait() activeClock {
	self.slots = nil
	self.queuedAtStart = 0
	return self
}

func activeTimeExpired(ctx context.Context, clock activeClock, limit time.Duration, over <-chan struct{}) bool {
	for {
		observation := waiting.Observe(ctx)
		active := clock.elapsed(observation)
		timeLeft := limit - active
		if timeLeft <= 0 {
			return ctx.Err() == nil
		}
		if observation.IsWaiting {
			select {
			case <-observation.Changes:
			case <-over:
				return false
			case <-ctx.Done():
				return false
			}
			continue
		}
		timer := time.NewTimer(timeLeft)
		select {
		case <-timer.C:
		case <-observation.Changes:
			timer.Stop()
			continue
		case <-over:
			timer.Stop()
			return false
		case <-ctx.Done():
			timer.Stop()
			return false
		}
		timer.Stop()
	}
}

func (self activeClock) elapsed(observation waiting.Observation) time.Duration {
	slotWaitTime := max(self.slots.get()-self.queuedAtStart, 0)
	return max(time.Since(self.startedAt)-max(observation.WaitTime-self.waitedAtStart, 0)-slotWaitTime, 0)
}
