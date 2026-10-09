package interaction

import (
	"os"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/turn"
)

func TestABurstOfTurnEventsIsDrawnOnceAtOnceAndOnceAFrameLater(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		keys := make(chan key.Key)
		turnEvents := make(chan turn.Event, 10)
		for range cap(turnEvents) {
			turnEvents <- turn.Event{}
		}

		startedAt := time.Now()
		handled := 0
		var drawnAfter []time.Duration

		go func() {
			time.Sleep(time.Second)
			keys <- key.Key{Code: key.Escape}
		}()

		run(keys, make(chan os.Signal), make(chan time.Time), func() {}, nil, Handler{
			GetTurnEvents: func() <-chan turn.Event { return turnEvents },
			OnTurn:        func(turn.Event) { handled++ },
			OnKey:         func(key.Key) bool { return false },
			OnDraw:        func() { drawnAfter = append(drawnAfter, time.Since(startedAt)) },
		})

		if handled != cap(turnEvents) {
			t.Errorf("handled %d turn events, want %d", handled, cap(turnEvents))
		}
		if want := []time.Duration{0, turnFrame}; !slices.Equal(drawnAfter, want) {
			t.Errorf("drawn after %v, want %v", drawnAfter, want)
		}
	})
}

func TestAnythingButATurnEventIsDrawnAtOnceAndSettlesTheFrameOwed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		keys := make(chan key.Key)
		turnEvents := make(chan turn.Event, 2)
		turnEvents <- turn.Event{}
		turnEvents <- turn.Event{}

		startedAt := time.Now()
		keypresses := 0
		var drawnAfter []time.Duration

		go func() {
			time.Sleep(turnFrame / 2)
			keys <- key.Key{Code: 'a'}
			time.Sleep(time.Second)
			keys <- key.Key{Code: key.Escape}
		}()

		run(keys, make(chan os.Signal), make(chan time.Time), func() {}, nil, Handler{
			GetTurnEvents: func() <-chan turn.Event { return turnEvents },
			OnTurn:        func(turn.Event) {},
			OnKey: func(keypress key.Key) bool {
				keypresses++
				return keypress.Code != key.Escape
			},
			OnDraw: func() { drawnAfter = append(drawnAfter, time.Since(startedAt)) },
		})

		if want := []time.Duration{0, turnFrame / 2}; !slices.Equal(drawnAfter, want) {
			t.Errorf("drawn after %v, want %v", drawnAfter, want)
		}
		if keypresses != 2 {
			t.Errorf("handled %d keypresses, want 2", keypresses)
		}
	})
}

func TestARefresherThatNeverSettlesIsRedrawnNoFasterThanTheFloor(t *testing.T) {
	for name, getNextRefresh := range map[string]func(time.Time) time.Time{
		"due at once":    func(at time.Time) time.Time { return at },
		"due just after": func(at time.Time) time.Time { return at.Add(time.Millisecond) },
		"already gone":   func(at time.Time) time.Time { return at.Add(-time.Minute) },
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				refresh := newRefreshTimer(getNextRefresh)
				defer refresh.stop()

				endsAt := time.Now().Add(time.Second)
				refreshes := 0
				for time.Now().Before(endsAt) {
					refresh.schedule()
					<-refresh.timer.C
					refreshes++
				}

				if limit := int(time.Second/refreshFloor) + 1; refreshes > limit {
					t.Errorf("redrawn %d times in a second, want at most %d", refreshes, limit)
				}
			})
		})
	}
}

func TestAReschedulingBetweenRefreshesNeverPushesTheFloorAway(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		refresh := newRefreshTimer(func(at time.Time) time.Time { return at })
		defer refresh.stop()

		refresh.schedule()
		firstAt := <-refresh.timer.C

		for range 10 {
			refresh.schedule()
			time.Sleep(refreshFloor / 20)
		}

		if got := (<-refresh.timer.C).Sub(firstAt); got != refreshFloor {
			t.Errorf("expected the next redraw a floor after the last, got it %s after", got)
		}
	})
}
