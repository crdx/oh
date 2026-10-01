package interaction

import (
	"time"

	"crdx.org/oh/internal/app/hostcommand"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/turn"
	"crdx.org/oh/internal/jobs"
	"crdx.org/oh/pkg/agent"
)

func watched(handler Handler) Handler {
	watch := handler.Watch
	if watch == nil {
		return handler
	}

	if onKey := handler.OnKey; onKey != nil {
		handler.OnKey = func(keypress key.Key) bool { defer watch("keypress")(); return onKey(keypress) }
	}
	if onTurn := handler.OnTurn; onTurn != nil {
		handler.OnTurn = func(event turn.Event) { defer watch("turn event")(); onTurn(event) }
	}
	if onTurnFinished := handler.OnTurnFinished; onTurnFinished != nil {
		handler.OnTurnFinished = func() bool { defer watch("turn finished")(); return onTurnFinished() }
	}
	if onResize := handler.OnResize; onResize != nil {
		handler.OnResize = func() { defer watch("resize")(); onResize() }
	}
	if onBeat := handler.OnBeat; onBeat != nil {
		handler.OnBeat = func() { defer watch("beat")(); onBeat() }
	}
	if onDraw := handler.OnDraw; onDraw != nil {
		handler.OnDraw = func() { defer watch("draw")(); onDraw() }
	}
	if onJobEnded := handler.OnJobEnded; onJobEnded != nil {
		handler.OnJobEnded = func(conclusion jobs.Conclusion) { defer watch("job ended")(); onJobEnded(conclusion) }
	}
	if onPortChange := handler.OnForwardChange; onPortChange != nil {
		handler.OnForwardChange = func(event agent.Event) { defer watch("port change")(); onPortChange(event) }
	}
	if onQuestionChange := handler.OnQuestionChange; onQuestionChange != nil {
		handler.OnQuestionChange = func() { defer watch("question change")(); onQuestionChange() }
	}
	if onTriggerChange := handler.OnTriggerChange; onTriggerChange != nil {
		handler.OnTriggerChange = func() { defer watch("trigger change")(); onTriggerChange() }
	}
	if onHostCommand := handler.OnHostCommand; onHostCommand != nil {
		handler.OnHostCommand = func(outcome hostcommand.Outcome) { defer watch("host command")(); onHostCommand(outcome) }
	}
	if onChange := handler.OnChange; onChange != nil {
		handler.OnChange = func(failure error) bool { defer watch("config change")(); return onChange(failure) }
	}

	return handler
}

func watchedSchedule(getNextRefresh func(time.Time) time.Time, watch func(string) func()) func(time.Time) time.Time {
	if watch == nil {
		return getNextRefresh
	}

	return func(at time.Time) time.Time {
		defer watch("schedule")()
		return getNextRefresh(at)
	}
}
