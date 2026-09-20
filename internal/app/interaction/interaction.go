package interaction

import (
	"bufio"
	"os"
	"os/signal"
	"syscall"
	"time"

	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/tty"
	"crdx.org/oh/internal/app/turn"
	"crdx.org/oh/internal/jobs"
	"crdx.org/oh/pkg/agent"
)

const (
	settling  = 100 * time.Millisecond
	heartRate = 15 * time.Second
	soonest   = time.Millisecond
)

type Handler struct {
	GetTurnEvents         func() <-chan turn.Event
	OnKey                 func(key.Key) bool
	OnTurn                func(turn.Event)
	OnTurnFinished        func() bool
	OnResize              func()
	OnBeat                func()
	Changes               <-chan error
	OnChange              func(error) bool
	Conclusions           <-chan jobs.Conclusion
	OnJobEnded            func(jobs.Conclusion)
	HostToSandboxChanges  <-chan agent.Event
	OnHostToSandboxChange func(agent.Event)
	QuestionChanges       <-chan struct{}
	OnQuestionChange      func()
	OnDraw                func()
}

func Run(terminal *os.File, getNextRefresh func(time.Time) time.Time, handler Handler) {
	resizeSignals := Resizes()
	defer signal.Stop(resizeSignals)

	refresh := newRefreshTimer(getNextRefresh)
	defer refresh.stop()

	beater := time.NewTicker(heartRate)
	defer beater.Stop()

	keys, stopReading := Keypresses(terminal)
	defer stopReading()

	run(keys, resizeSignals, refresh.timer.C, refresh.schedule, beater.C, handler)
}

func run(keys <-chan key.Key, resizeSignals <-chan os.Signal, refreshes <-chan time.Time, schedule func(), beats <-chan time.Time, handler Handler) {
	changes := handler.Changes
	conclusions := handler.Conclusions
	hostToSandboxChanges := handler.HostToSandboxChanges
	questionChanges := handler.QuestionChanges
	for {
		schedule()

		select {
		case keypress, isOpen := <-keys:
			if !isOpen || !handler.OnKey(keypress) {
				return
			}
		case event, isRunning := <-handler.GetTurnEvents():
			if isRunning {
				handler.OnTurn(event)
			} else if !handler.OnTurnFinished() {
				return
			}
		case <-resizeSignals:
			Settle(resizeSignals)
			handler.OnResize()
		case <-beats:
			handler.OnBeat()

			select {
			case <-refreshes:
			default:
				continue
			}
		case <-refreshes:
		case conclusion, isOpen := <-conclusions:
			if !isOpen {
				conclusions = nil
				continue
			}
			handler.OnJobEnded(conclusion)
		case event, isOpen := <-hostToSandboxChanges:
			if !isOpen {
				hostToSandboxChanges = nil
				continue
			}
			handler.OnHostToSandboxChange(event)
		case _, isOpen := <-questionChanges:
			if !isOpen {
				questionChanges = nil
				continue
			}
			handler.OnQuestionChange()
		case failure, isOpen := <-changes:
			if !isOpen {
				changes = nil
				continue
			}
			if handler.OnChange != nil && !handler.OnChange(failure) {
				continue
			}
		}

		handler.OnDraw()
	}
}

type refreshTimer struct {
	getNextRefresh func(time.Time) time.Time
	timer          *time.Timer
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

	if delay := dueAt.Sub(at); delay > 0 {
		self.timer.Reset(delay)
	} else {
		self.timer.Reset(soonest)
	}
}

func (self *refreshTimer) stop() {
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

func Settle(signals <-chan os.Signal) {
	time.Sleep(settling)
	for {
		select {
		case <-signals:
		default:
			return
		}
	}
}
