package interaction

import (
	"slices"
	"testing"
	"time"

	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/turn"
	"crdx.org/oh/internal/jobs"
	"crdx.org/oh/pkg/agent"
)

func TestEveryPieceOfWorkIsWatchedFromItsStartToItsEnd(t *testing.T) {
	var watchedWork []string
	watch := func(work string) func() {
		watchedWork = append(watchedWork, "begin "+work)
		return func() { watchedWork = append(watchedWork, "end "+work) }
	}

	handler := watched(Handler{
		OnKey:  func(key.Key) bool { watchedWork = append(watchedWork, "handle keypress"); return true },
		OnDraw: func() { watchedWork = append(watchedWork, "handle draw") },
		Watch:  watch,
	})
	handler.OnKey(key.Key{Code: key.Enter})
	handler.OnDraw()
	watchedSchedule(func(at time.Time) time.Time { return at }, watch)(time.Time{})

	want := []string{
		"begin keypress", "handle keypress", "end keypress",
		"begin draw", "handle draw", "end draw",
		"begin schedule", "end schedule",
	}
	if !slices.Equal(watchedWork, want) {
		t.Errorf("got %q, want %q", watchedWork, want)
	}
}

func TestEveryHandlerIsWatchedUnderItsOwnName(t *testing.T) {
	var watchedWork []string
	handler := watched(Handler{
		OnKey:                 func(key.Key) bool { return true },
		OnTurn:                func(turn.Event) {},
		OnTurnFinished:        func() bool { return true },
		OnResize:              func() {},
		OnBeat:                func() {},
		OnDraw:                func() {},
		OnJobEnded:            func(jobs.Conclusion) {},
		OnHostToSandboxChange: func(agent.Event) {},
		OnQuestionChange:      func() {},
		OnTriggerChange:       func() {},
		OnChange:              func(error) bool { return true },
		Watch: func(work string) func() {
			watchedWork = append(watchedWork, work)
			return func() {}
		},
	})

	handler.OnKey(key.Key{})
	handler.OnTurn(turn.Event{})
	handler.OnTurnFinished()
	handler.OnResize()
	handler.OnBeat()
	handler.OnDraw()
	handler.OnJobEnded(jobs.Conclusion{})
	handler.OnHostToSandboxChange(agent.Event{})
	handler.OnQuestionChange()
	handler.OnTriggerChange()
	handler.OnChange(nil)

	want := []string{
		"keypress", "turn event", "turn finished", "resize", "beat", "draw",
		"job ended", "port change", "question change", "trigger change", "config change",
	}
	if !slices.Equal(watchedWork, want) {
		t.Errorf("got %q, want %q", watchedWork, want)
	}
}

func TestNothingIsWatchedWithoutAWatcher(t *testing.T) {
	onDraw := func() {}
	handler := watched(Handler{OnDraw: onDraw})

	if handler.OnKey != nil || handler.OnBeat != nil {
		t.Error("expected missing work to stay missing")
	}
}
