package interaction

import (
	"os"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/turn"
)

func TestResizeBatchDrawsTheFirstSignalWithoutWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		batch := NewResizeBatch()
		defer batch.Stop()
		resizes := make(chan os.Signal, 3)
		for range 3 {
			resizes <- syscall.SIGWINCH
		}
		<-resizes

		startedAt := time.Now()
		if !batch.Signal(resizes) {
			t.Fatal("the first resize did not ask for an immediate draw")
		}
		if elapsedTime := time.Since(startedAt); elapsedTime != 0 {
			t.Errorf("the first resize waited %s", elapsedTime)
		}
		<-batch.Ready()
		if batch.Finish(resizes) {
			t.Error("signals already in the initial batch caused a second draw")
		}
	})
}

func TestResizeBatchDrawsAgainAfterTheLastFollowUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		batch := NewResizeBatch()
		defer batch.Stop()
		resizes := make(chan os.Signal, 1)
		if !batch.Signal(resizes) {
			t.Fatal("first signal did not draw")
		}
		time.Sleep(40 * time.Millisecond)
		resizes <- syscall.SIGWINCH
		<-resizes
		if batch.Signal(resizes) {
			t.Error("follow-up signal redrew before the batch settled")
		}
		time.Sleep(60 * time.Millisecond)
		resizes <- syscall.SIGWINCH
		<-resizes
		if batch.Signal(resizes) {
			t.Error("another follow-up signal redrew before the batch settled")
		}
		time.Sleep(resizeQuiet - time.Millisecond)
		select {
		case <-batch.Ready():
			t.Error("batch ended before the last signal settled")
		default:
		}
		<-batch.Ready()
		if !batch.Finish(resizes) {
			t.Error("the last size was never drawn")
		}
		if !batch.Signal(resizes) {
			t.Error("a new burst did not draw promptly")
		}
	})
}

func TestResizeBatchKeepsASignalArrivingAtTheQuietBoundary(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		batch := NewResizeBatch()
		defer batch.Stop()
		resizes := make(chan os.Signal, 1)
		batch.Signal(resizes)
		time.Sleep(resizeQuiet)
		resizes <- syscall.SIGWINCH
		<-batch.Ready()
		if batch.Finish(resizes) {
			t.Error("a queued resize was declared settled")
		}
		<-batch.Ready()
		if !batch.Finish(resizes) {
			t.Error("the last queued resize was lost")
		}
	})
}

func TestResizeBatchDoesNotHoldKeysOrTurnEvents(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		resizes := make(chan os.Signal, 1)
		resizes <- syscall.SIGWINCH
		keys := make(chan key.Key, 1)
		turnEvents := make(chan turn.Event, 1)
		startedAt := time.Now()
		hasSeenTurn := false
		run(keys, resizes, make(chan time.Time), func() {}, nil, Handler{
			GetTurnEvents: func() <-chan turn.Event { return turnEvents },
			OnResize: func() {
				turnEvents <- turn.Event{}
			},
			OnTurn: func(turn.Event) {
				hasSeenTurn = true
				keys <- key.Key{Code: key.Escape}
			},
			OnKey: func(key.Key) bool {
				if elapsedTime := time.Since(startedAt); elapsedTime != 0 {
					t.Errorf("keypress and turn were delayed by %s", elapsedTime)
				}
				return false
			},
			OnDraw: func() {},
		})
		if !hasSeenTurn {
			t.Error("the turn event was not handled before the key")
		}
	})
}
