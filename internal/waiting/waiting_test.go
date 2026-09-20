package waiting_test

import (
	"sync"
	"testing"
	"time"

	"crdx.org/oh/internal/waiting"
)

func TestWaitingIsAddedUpAcrossACall(t *testing.T) {
	ctx, waitedTime := waiting.Track(t.Context())

	if got := waitedTime(); got != 0 {
		t.Errorf("got %s before anything waited, want nothing", got)
	}

	waiting.Record(ctx, time.Second)
	waiting.Record(ctx, 2*time.Second)

	if got := waitedTime(); got != 3*time.Second {
		t.Errorf("got %s, want every wait counted", got)
	}
}

func TestWaitingNobodyIsTrackingIsDropped(t *testing.T) {
	defer func() {
		if panicValue := recover(); panicValue != nil {
			t.Fatalf("recording an untracked wait panicked: %v", panicValue)
		}
	}()

	waiting.Record(t.Context(), time.Second)
}

func TestWaitingIsCountedOncePerCall(t *testing.T) {
	first, firstWaitedTime := waiting.Track(t.Context())
	second, secondWaitedTime := waiting.Track(t.Context())

	waiting.Record(first, time.Second)

	if got := secondWaitedTime(); got != 0 {
		t.Errorf("got %s on the other call, want nothing", got)
	}
	waiting.Record(second, time.Minute)
	if got := firstWaitedTime(); got != time.Second {
		t.Errorf("got %s, want only its own wait", got)
	}
}

func TestConcurrentWaitsAreAllCounted(t *testing.T) {
	const waits = 64

	ctx, waitedTime := waiting.Track(t.Context())

	var group sync.WaitGroup
	for range waits {
		group.Go(func() { waiting.Record(ctx, time.Second) })
	}
	group.Wait()

	if got := waitedTime(); got != waits*time.Second {
		t.Errorf("got %s, want %s", got, waits*time.Second)
	}
}
