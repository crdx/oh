package interaction

import (
	"io"
	"os"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/turn"
)

func TestABarWithNothingToSayIsNeverRedrawn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		refresh := newRefreshTimer(func(time.Time) time.Time { return time.Time{} })
		defer refresh.stop()

		refresh.schedule()

		time.Sleep(time.Hour)

		select {
		case at := <-refresh.timer.C:
			t.Errorf("expected a still bar to be left alone, got a redraw at %s", at)
		default:
		}
	})
}

func TestARefreshIsTakenAtTheMomentItWasAskedFor(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		startedAt := time.Now()

		refresh := newRefreshTimer(func(at time.Time) time.Time {
			return at.Truncate(time.Second).Add(time.Second)
		})
		defer refresh.stop()

		refresh.schedule()

		at := <-refresh.timer.C

		if got := at.Sub(startedAt); got != time.Second {
			t.Errorf("expected the redraw a second on, got it %s on", got)
		}
	})
}

func TestALateRescheduleKeepsTheMomentAlreadyDueRatherThanPushingItAway(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		startedAt := time.Now()
		dueAt := startedAt.Add(time.Second)

		refresh := newRefreshTimer(func(time.Time) time.Time { return dueAt })
		defer refresh.stop()

		for range 8 {
			refresh.schedule()
			time.Sleep(100 * time.Millisecond)
		}

		at := <-refresh.timer.C

		if got := at.Sub(startedAt); got != time.Second {
			t.Errorf("expected the redraw to stand at a second on, got it %s on", got)
		}
	})
}

func TestARefreshAlreadyGoneIsTakenAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		startedAt := time.Now()

		refresh := newRefreshTimer(func(at time.Time) time.Time { return at.Add(-time.Minute) })
		defer refresh.stop()

		refresh.schedule()

		at := <-refresh.timer.C

		if got := at.Sub(startedAt); got != soonest {
			t.Errorf("expected the redraw to be put off by %s, got %s", soonest, got)
		}
	})
}

func TestABarThatSettlesStopsBeingRedrawn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dueAt := time.Now().Add(time.Second)

		refresh := newRefreshTimer(func(at time.Time) time.Time {
			if at.After(dueAt) {
				return time.Time{}
			}

			return dueAt
		})
		defer refresh.stop()

		refresh.schedule()
		<-refresh.timer.C

		time.Sleep(time.Millisecond)
		refresh.schedule()
		time.Sleep(time.Hour)

		select {
		case at := <-refresh.timer.C:
			t.Errorf("expected the bar to settle, got a redraw at %s", at)
		default:
		}
	})
}

func TestRunStopsAndRedraws(t *testing.T) {
	t.Run("key", func(t *testing.T) {
		keys := make(chan key.Key, 1)
		keys <- key.Key{Code: key.Escape}
		wasHandled := false
		run(keys, make(chan os.Signal), make(chan time.Time), func() {}, nil, Handler{GetTurnEvents: func() <-chan turn.Event { return make(chan turn.Event) }, OnKey: func(key.Key) bool { wasHandled = true; return false }})
		if !wasHandled {
			t.Error("key not handled")
		}
	})
	t.Run("finished turn requests exit", func(t *testing.T) {
		turnEvents := make(chan turn.Event)
		close(turnEvents)
		hasFinished := false
		run(make(chan key.Key), make(chan os.Signal), make(chan time.Time), func() {}, nil, Handler{
			GetTurnEvents: func() <-chan turn.Event { return turnEvents },
			OnTurnFinished: func() bool {
				hasFinished = true
				return false
			},
		})
		if !hasFinished {
			t.Error("finished turn was not handled")
		}
	})
	t.Run("refresh", func(t *testing.T) {
		keys := make(chan key.Key)
		refreshes := make(chan time.Time, 1)
		refreshes <- time.Now()
		scheduled, wasDrawn := 0, false
		run(keys, make(chan os.Signal), refreshes, func() { scheduled++ }, nil, Handler{GetTurnEvents: func() <-chan turn.Event { return make(chan turn.Event) }, OnDraw: func() { wasDrawn = true; close(keys) }, OnKey: func(key.Key) bool { return false }})
		if scheduled < 2 || !wasDrawn {
			t.Errorf("scheduled=%d drawn=%t", scheduled, wasDrawn)
		}
	})
	t.Run("question", func(t *testing.T) {
		keys := make(chan key.Key)
		changes := make(chan struct{}, 1)
		changes <- struct{}{}
		wasHandled, wasDrawn := false, false
		run(keys, make(chan os.Signal), make(chan time.Time), func() {}, nil, Handler{
			GetTurnEvents:   func() <-chan turn.Event { return make(chan turn.Event) },
			OnKey:           func(key.Key) bool { return false },
			QuestionChanges: changes,
			OnQuestionChange: func() {
				wasHandled = true
			},
			OnDraw: func() { wasDrawn = true; close(keys) },
		})
		if !wasHandled || !wasDrawn {
			t.Errorf("handled=%t drawn=%t", wasHandled, wasDrawn)
		}
	})
	t.Run("ignored change", func(t *testing.T) {
		keys := make(chan key.Key)
		changes := make(chan error, 1)
		changes <- nil
		wasDrawn := false
		run(keys, make(chan os.Signal), make(chan time.Time), func() {}, nil, Handler{
			GetTurnEvents: func() <-chan turn.Event { return make(chan turn.Event) },
			OnKey:         func(key.Key) bool { return false },
			Changes:       changes,
			OnChange: func(error) bool {
				close(keys)
				return false
			},
			OnDraw: func() { wasDrawn = true },
		})
		if wasDrawn {
			t.Error("ignored change was drawn")
		}
	})
	t.Run("heartbeat with a redraw already due", func(t *testing.T) {
		keys := make(chan key.Key)
		heartbeats := make(chan time.Time, 1)
		heartbeats <- time.Now()
		refreshes := make(chan time.Time, 1)
		wasBeaten, wasDrawn := false, false
		run(keys, make(chan os.Signal), refreshes, func() {}, heartbeats, Handler{
			GetTurnEvents: func() <-chan turn.Event { return make(chan turn.Event) },
			OnBeat:        func() { wasBeaten = true; refreshes <- time.Now() },
			OnKey:         func(key.Key) bool { return false },
			OnDraw:        func() { wasDrawn = true; close(keys) },
		})
		if !wasBeaten || !wasDrawn {
			t.Errorf("beaten=%t drawn=%t", wasBeaten, wasDrawn)
		}
	})
	t.Run("heartbeat", func(t *testing.T) {
		keys := make(chan key.Key)
		heartbeats := make(chan time.Time, 1)
		heartbeats <- time.Now()
		wasBeaten, wasDrawn := false, false
		run(keys, make(chan os.Signal), make(chan time.Time), func() {}, heartbeats, Handler{
			GetTurnEvents: func() <-chan turn.Event { return make(chan turn.Event) },
			OnBeat:        func() { wasBeaten = true; close(keys) },
			OnKey:         func(key.Key) bool { return false },
			OnDraw:        func() { wasDrawn = true },
		})
		if !wasBeaten || wasDrawn {
			t.Errorf("beaten=%t drawn=%t", wasBeaten, wasDrawn)
		}
	})
}

func TestKeypressesGiveTheTerminalBackWhenTheyAreStopped(t *testing.T) {
	terminal := openTerminal(t)

	keys, stopReading := Keypresses(terminal)
	stopReading()

	select {
	case _, isOpen := <-keys:
		if isOpen {
			t.Error("a stopped reader handed back a keypress")
		}
	case <-time.After(time.Second):
		t.Fatal("the keypress channel stayed open after stopping")
	}

	if _, err := io.WriteString(terminal, "x"); err != nil {
		t.Fatal(err)
	}

	var taken [1]byte
	if _, err := terminal.Read(taken[:]); err != nil {
		t.Fatal(err)
	}
	if taken[0] != 'x' {
		t.Errorf("the next reader got %q, so the keypress reader was still holding the terminal", taken[0])
	}
}

func TestKeypressesAreHandedOnUntilTheyAreStopped(t *testing.T) {
	terminal := openTerminal(t)

	keys, stopReading := Keypresses(terminal)
	t.Cleanup(stopReading)

	if _, err := io.WriteString(terminal, "a"); err != nil {
		t.Fatal(err)
	}

	select {
	case keypress := <-keys:
		if keypress.Code != key.Rune || keypress.Value != 'a' {
			t.Errorf("got %+v", keypress)
		}
	case <-time.After(time.Second):
		t.Fatal("the keypress was never handed on")
	}
}

const decodingPause = 50 * time.Millisecond

func TestStoppingLetsGoOfAKeypressNobodyIsWaitingFor(t *testing.T) {
	terminal := openTerminal(t)

	keys, stopReading := Keypresses(terminal)

	if _, err := io.WriteString(terminal, "a"); err != nil {
		t.Fatal(err)
	}

	time.Sleep(decodingPause)

	stopped := make(chan struct{})
	go func() {
		stopReading()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stopping waited on a keypress nobody wanted")
	}

	if _, isOpen := <-keys; isOpen {
		t.Error("a stopped reader was still handing keypresses on")
	}
}

func openTerminal(t *testing.T) *os.File {
	t.Helper()

	terminal, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal to test against: %v", err)
	}
	t.Cleanup(func() { _ = terminal.Close() })

	return terminal
}
