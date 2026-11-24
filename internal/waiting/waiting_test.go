package waiting_test

import (
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/internal/waiting"
)

func TestWaitingIsAddedUpAcrossACall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, waitedTime := waiting.Track(t.Context())

		if got := waitedTime(); got != 0 {
			t.Errorf("got %s before anything waited, want nothing", got)
		}

		finishWaiting := waiting.Begin(ctx)
		time.Sleep(time.Second)
		finishWaiting()
		finishWaiting = waiting.Begin(ctx)
		time.Sleep(2 * time.Second)
		finishWaiting()

		if got := waitedTime(); got != 3*time.Second {
			t.Errorf("got %s, want every wait counted", got)
		}
	})
}

func TestWaitingNobodyIsTrackingIsDropped(t *testing.T) {
	defer func() {
		if panicValue := recover(); panicValue != nil {
			t.Fatalf("recording an untracked wait panicked: %v", panicValue)
		}
	}()

	waiting.Begin(t.Context())()
}

func TestWaitingIsCountedOncePerCall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		first, firstWaitedTime := waiting.Track(t.Context())
		second, secondWaitedTime := waiting.Track(t.Context())

		finishWaiting := waiting.Begin(first)
		time.Sleep(time.Second)
		finishWaiting()

		if got := secondWaitedTime(); got != 0 {
			t.Errorf("got %s on the other call, want nothing", got)
		}
		finishWaiting = waiting.Begin(second)
		time.Sleep(time.Minute)
		finishWaiting()
		if got := firstWaitedTime(); got != time.Second {
			t.Errorf("got %s, want only its own wait", got)
		}
	})
}

func TestConcurrentWaitsDoNotCountTheSameTimeTwice(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const waits = 64

		ctx, waitedTime := waiting.Track(t.Context())

		var group sync.WaitGroup
		for range waits {
			group.Go(func() {
				finishWaiting := waiting.Begin(ctx)
				time.Sleep(time.Second)
				finishWaiting()
			})
		}
		group.Wait()

		if got := waitedTime(); got != time.Second {
			t.Errorf("got %s, want %s", got, time.Second)
		}
	})
}
