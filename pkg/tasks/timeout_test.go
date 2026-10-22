package tasks_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/internal/waiting"
	"crdx.org/oh/pkg/tasks"
)

func TestActiveTimeoutPausesAcrossAnApproval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tracked, _ := waiting.Track(t.Context())
		bounded, cancel := tasks.ActiveTimeout(tracked, 5*time.Minute)
		defer cancel()
		time.Sleep(time.Minute)
		finishWaiting := waiting.Begin(bounded)
		time.Sleep(30 * time.Minute)
		if bounded.Err() != nil {
			t.Fatal("an approval consumed the active timeout")
		}
		finishWaiting()
		<-bounded.Done()
		if !errors.Is(context.Cause(bounded), context.DeadlineExceeded) {
			t.Errorf("timer ended with %v", context.Cause(bounded))
		}
	})
}

func TestAnActiveTimeoutDoesNotCountAnEarlierApproval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tracked, _ := waiting.Track(t.Context())
		finishWaiting := waiting.Begin(tracked)
		time.Sleep(time.Hour)
		finishWaiting()
		bounded, cancel := tasks.ActiveTimeout(tracked, time.Minute)
		defer cancel()
		time.Sleep(time.Minute - time.Second)
		if bounded.Err() != nil {
			t.Fatal("an earlier approval changed a new timeout")
		}
		time.Sleep(time.Second)
		<-bounded.Done()
		if !errors.Is(context.Cause(bounded), context.DeadlineExceeded) {
			t.Errorf("timer ended with %v", context.Cause(bounded))
		}
	})
}
