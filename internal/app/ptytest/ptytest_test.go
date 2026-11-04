package ptytest

import (
	"testing"
	"time"
)

const (
	settleQuiet   = 200 * time.Millisecond
	settleLimit   = 300 * time.Millisecond
	drawingPeriod = 10 * time.Millisecond
	settleMargin  = 5 * time.Second
)

func TestASettleGivesUpWhileTheScreenNeverFallsQuiet(t *testing.T) {
	self := &Transcript{lastWriteAt: time.Now()}
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(drawingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				self.mutex.Lock()
				self.lastWriteAt = time.Now()
				self.mutex.Unlock()
			}
		}
	}()

	startedAt := time.Now()
	self.WaitToSettle(settleQuiet, settleLimit)
	if elapsed := time.Since(startedAt); elapsed > settleMargin {
		t.Fatalf("a settle took %v while the screen kept drawing, want it to give up after %v", elapsed, settleLimit)
	}
}

func TestASettleReturnsOnceTheScreenFallsQuiet(t *testing.T) {
	self := &Transcript{lastWriteAt: time.Now().Add(-settleQuiet)}

	startedAt := time.Now()
	self.WaitToSettle(settleQuiet, settleMargin)
	if elapsed := time.Since(startedAt); elapsed >= settleMargin {
		t.Fatalf("a settle on a quiet screen took %v", elapsed)
	}
}
