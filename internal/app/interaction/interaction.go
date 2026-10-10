package interaction

import (
	"bufio"
	"os"
	"os/signal"
	"syscall"
	"time"

	"crdx.org/oh/internal/app/editor"
	"crdx.org/oh/internal/app/hostcommand"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/tty"
	"crdx.org/oh/internal/app/turn"
	"crdx.org/oh/internal/jobs"
	"crdx.org/oh/internal/money"
	"crdx.org/oh/pkg/agent"
)

const (
	heartRate = 15 * time.Second
	soonest   = time.Millisecond
)

type Handler struct {
	GetTurnEvents     func() <-chan turn.Event
	OnKey             func(key.Key) bool
	OnTurn            func(turn.Event)
	OnTurnFinished    func() bool
	OnResize          func()
	OnBeat            func()
	Changes           <-chan error
	OnChange          func(error) bool
	Conclusions       <-chan jobs.Conclusion
	OnJobEnded        func(jobs.Conclusion)
	SubagentEvents    <-chan agent.Event
	OnSubagentEvent   func(agent.Event)
	ForwardChanges    <-chan agent.Event
	OnForwardChange   func(agent.Event)
	QuestionChanges   <-chan struct{}
	OnQuestionChange  func()
	TriggerChanges    <-chan struct{}
	OnTriggerChange   func()
	HostCommands      <-chan hostcommand.Outcome
	OnHostCommand     func(hostcommand.Outcome)
	EditorOutcomes    <-chan editor.Outcome
	OnEditorEnded     func(editor.Outcome)
	CurrencyRefreshes <-chan money.Refresh
	OnCurrencyRefresh func(money.Refresh)
	OnDraw            func()
	Watch             func(work string) func()
}

func Run(keyboard *Keyboard, getNextRefresh func(time.Time) time.Time, handler Handler) {
	resizeSignals := Resizes()
	defer signal.Stop(resizeSignals)

	refresh := newRefreshTimer(watchedSchedule(getNextRefresh, handler.Watch))
	defer refresh.stop()

	beater := time.NewTicker(heartRate)
	defer beater.Stop()

	defer keyboard.Release()

	run(keyboard.Keys(), resizeSignals, refresh.timer.C, refresh.schedule, beater.C, watched(handler))
}

func run(keys <-chan key.Key, resizeSignals <-chan os.Signal, refreshes <-chan time.Time, schedule func(), beats <-chan time.Time, handler Handler) {
	handler = handler.withDefaults()
	changes := handler.Changes
	conclusions := handler.Conclusions
	subagentEvents := handler.SubagentEvents
	forwardChanges := handler.ForwardChanges
	resizes := NewResizeBatch()
	defer resizes.Stop()
	questionChanges := handler.QuestionChanges
	triggerChanges := handler.TriggerChanges
	hostCommands := handler.HostCommands
	editorOutcomes := handler.EditorOutcomes
	currencyRefreshes := handler.CurrencyRefreshes
	frames := newFrameGate(turnFrame)
	defer frames.stop()
	for {
		schedule()

		select {
		case keypress, isOpen := <-keys:
			if !isOpen || !handler.OnKey(keypress) {
				return
			}
		case event, isRunning := <-handler.GetTurnEvents():
			step := takeTurnEvent(handler, frames, event, isRunning)
			if step == stopLoop {
				return
			}
			if step == skipDraw {
				continue
			}
		case <-resizeSignals:
			if !resizes.Signal(resizeSignals) {
				continue
			}
			handler.OnResize()
		case <-resizes.Ready():
			if !resizes.Finish(resizeSignals) {
				continue
			}
			handler.OnResize()
		case <-beats:
			handler.OnBeat()
			if !hasArrived(refreshes) {
				continue
			}
		case <-refreshes:
		case <-frames.due():
		case conclusion, isOpen := <-conclusions:
			if !isOpen {
				conclusions = nil
				continue
			}
			handler.OnJobEnded(conclusion)
		case event, isOpen := <-subagentEvents:
			if !isOpen {
				subagentEvents = nil
				continue
			}
			handler.OnSubagentEvent(event)
		case event, isOpen := <-forwardChanges:
			if !isOpen {
				forwardChanges = nil
				continue
			}
			handler.OnForwardChange(event)
		case _, isOpen := <-questionChanges:
			if !isOpen {
				questionChanges = nil
				continue
			}
			handler.OnQuestionChange()
		case _, isOpen := <-triggerChanges:
			if !isOpen {
				triggerChanges = nil
				continue
			}
			handler.OnTriggerChange()
		case outcome := <-hostCommands:
			handler.OnHostCommand(outcome)
		case outcome := <-editorOutcomes:
			handler.OnEditorEnded(outcome)
		case refresh := <-currencyRefreshes:
			handler.OnCurrencyRefresh(refresh)
		case failure, isOpen := <-changes:
			if !isOpen {
				changes = nil
				continue
			}
			if !handler.OnChange(failure) {
				continue
			}
		}

		handler.OnDraw()
		frames.drawn(time.Now())
	}
}

type loopStep int

const (
	drawFrame loopStep = iota
	skipDraw
	stopLoop
)

func takeTurnEvent(handler Handler, frames *frameGate, event turn.Event, isRunning bool) loopStep {
	if !isRunning {
		if handler.OnTurnFinished() {
			return drawFrame
		}
		return stopLoop
	}
	handler.OnTurn(event)
	if frames.shouldWait(time.Now()) {
		return skipDraw
	}
	return drawFrame
}

func hasArrived(refreshes <-chan time.Time) bool {
	select {
	case <-refreshes:
		return true
	default:
		return false
	}
}

func (self Handler) withDefaults() Handler {
	if self.OnChange == nil {
		self.OnChange = func(error) bool { return true }
	}
	return self
}

type refreshTimer struct {
	getNextRefresh func(time.Time) time.Time
	timer          *time.Timer
	firesAt        time.Time
	firedAt        time.Time
}

func newRefreshTimer(getNextRefresh func(time.Time) time.Time) *refreshTimer {
	timer := time.NewTimer(time.Hour)
	timer.Stop()

	return &refreshTimer{getNextRefresh: getNextRefresh, timer: timer}
}

func (self *refreshTimer) schedule() {
	at := time.Now()

	dueAt := self.getNextRefresh(at)
	if dueAt.IsZero() {
		self.stop()
		return
	}

	if !self.firesAt.IsZero() && !self.firesAt.After(at) {
		self.firedAt = self.firesAt
	}

	delay := max(latest(dueAt, self.firedAt.Add(refreshFloor)).Sub(at), soonest)
	self.firesAt = at.Add(delay)
	self.timer.Reset(delay)
}

func latest(first time.Time, second time.Time) time.Time {
	if first.After(second) {
		return first
	}

	return second
}

func (self *refreshTimer) stop() {
	self.firesAt = time.Time{}
	self.timer.Stop()
}

func Keypresses(terminal *os.File) (<-chan key.Key, func()) {
	reader := tty.NewReader(terminal)
	keys := make(chan key.Key)
	finishedChannel := make(chan struct{})

	go func() {
		defer close(finishedChannel)
		defer close(keys)

		decoder := key.NewTerminalDecoder(bufio.NewReader(reader), terminal)
		for {
			keypress, err := decoder.Next()
			if err != nil {
				return
			}

			select {
			case keys <- keypress:
			case <-reader.Stopping():
				return
			}
		}
	}()

	return keys, func() {
		reader.Stop()
		<-finishedChannel
		reader.Close()
	}
}

func Resizes() chan os.Signal {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGWINCH)
	return signals
}
