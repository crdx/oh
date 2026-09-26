package ask

import (
	"context"
	"errors"
	"sync"
	"time"

	"crdx.org/oh/internal/waiting"
)

var (
	ErrCancelled   = errors.New("the question was cancelled")
	ErrUnavailable = errors.New("there is nobody to ask")
)

const noChoice = -1

type Request struct {
	Question      Question
	deadline      time.Time
	lapsesAt      time.Time
	lapseAfter    time.Duration
	hasLapse      bool
	currentSignal chan struct{}
	answer        chan int
	answerOnce    sync.Once
	startOnce     sync.Once
	finishOnce    sync.Once
	finish        func()
}

func (self *Request) Deadline() (time.Time, bool) {
	return self.deadline, !self.deadline.IsZero()
}

func (self *Request) Choose(index int) {
	if index < 0 || index >= len(self.Question.Options) {
		return
	}

	self.settle(index)
}

func (self *Request) Cancel() {
	self.settle(noChoice)
}

func (self *Request) start(at time.Time) {
	self.startOnce.Do(func() {
		if self.hasLapse {
			self.lapsesAt = at.Add(self.lapseAfter)
			if self.deadline.IsZero() || self.lapsesAt.Before(self.deadline) {
				self.deadline = self.lapsesAt
			}
		}
		close(self.currentSignal)
	})
}

func (self *Request) settle(index int) {
	self.answerOnce.Do(func() {
		self.answer <- index
		self.finishRequest()
	})
}

func (self *Request) finishRequest() {
	self.finishOnce.Do(self.finish)
}

type Broker struct {
	mutex         sync.Mutex
	requests      []*Request
	changes       chan struct{}
	isInteractive bool
}

func New() *Broker {
	return &Broker{changes: make(chan struct{}, 1)}
}

func (self *Broker) Open() func() {
	self.mutex.Lock()
	self.isInteractive = true
	self.mutex.Unlock()

	return func() {
		self.mutex.Lock()
		self.isInteractive = false
		self.mutex.Unlock()
	}
}

func (self *Broker) Changes() <-chan struct{} {
	return self.changes
}

func (self *Broker) Current() *Request {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(self.requests) == 0 {
		return nil
	}

	return self.requests[0]
}

func (self *Broker) Ask(ctx context.Context, question Question) (int, error) {
	return self.ask(ctx, question, 0, false)
}

func (self *Broker) AskWithin(
	ctx context.Context,
	question Question,
	lapseAfter time.Duration,
) (int, error) {
	return self.ask(ctx, question, lapseAfter, true)
}

func (self *Broker) ask(
	ctx context.Context,
	question Question,
	lapseAfter time.Duration,
	hasLapse bool,
) (int, error) {
	request := &Request{
		Question:      question,
		lapseAfter:    lapseAfter,
		hasLapse:      hasLapse,
		currentSignal: make(chan struct{}),
		answer:        make(chan int, 1),
	}
	request.finish = func() { self.finish(request) }
	if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
		request.deadline = deadline
	}

	self.mutex.Lock()
	if !self.isInteractive {
		self.mutex.Unlock()
		return noChoice, ErrUnavailable
	}
	self.requests = append(self.requests, request)
	if len(self.requests) == 1 {
		request.start(time.Now())
	}
	self.mutex.Unlock()
	self.changed()

	askedAt := time.Now()
	defer func() { waiting.Record(ctx, time.Since(askedAt)) }()
	defer request.finishRequest()

	select {
	case <-request.currentSignal:
		return request.waitForAnswer(ctx)
	case <-ctx.Done():
		return noChoice, ctx.Err()
	}
}

func (self *Request) waitForAnswer(ctx context.Context) (int, error) {
	var lapse <-chan time.Time
	var timer *time.Timer
	if self.hasLapse {
		timer = time.NewTimer(time.Until(self.lapsesAt))
		lapse = timer.C
		defer timer.Stop()
	}

	select {
	case index := <-self.answer:
		if index == noChoice {
			return noChoice, ErrCancelled
		}
		return index, nil
	case <-lapse:
		return noChoice, context.DeadlineExceeded
	case <-ctx.Done():
		return noChoice, ctx.Err()
	}
}

func (self *Broker) finish(request *Request) {
	self.mutex.Lock()
	for i, current := range self.requests {
		if current != request {
			continue
		}

		wasCurrent := i == 0
		self.requests = append(self.requests[:i], self.requests[i+1:]...)
		if wasCurrent && len(self.requests) > 0 {
			self.requests[0].start(time.Now())
		}
		break
	}
	self.mutex.Unlock()
	self.changed()
}

func (self *Broker) changed() {
	select {
	case self.changes <- struct{}{}:
	default:
	}
}
