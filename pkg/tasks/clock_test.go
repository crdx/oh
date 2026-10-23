package tasks

import (
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/internal/waiting"
)

func TestAnActiveClockExcludesApprovalTimeBeforeItsTimerStarts(t *testing.T) {
	for _, limit := range []time.Duration{ForegroundLimit, 45 * time.Minute} {
		t.Run(limit.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, _ := waiting.Track(t.Context())
				clock := newActiveClock(ctx)
				time.Sleep(time.Second)
				finishWaiting := waiting.Begin(ctx)
				time.Sleep(limit)
				expired := make(chan bool, 1)
				go func() { expired <- activeTimeExpired(ctx, clock, limit, make(chan struct{})) }()
				synctest.Wait()
				select {
				case <-expired:
					t.Fatal("approval time before the timer started consumed the active limit")
				default:
				}
				finishWaiting()
				time.Sleep(limit - 2*time.Second)
				synctest.Wait()
				select {
				case <-expired:
					t.Fatal("the timer ended before the remaining active allowance")
				default:
				}
				time.Sleep(time.Second)
				if !<-expired {
					t.Fatal("the timer did not expire at the active limit")
				}
			})
		})
	}
}
