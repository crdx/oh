package waiting

import (
	"context"
	"sync"
	"time"
)

type key struct{}

type personWait struct {
	mutex   sync.Mutex
	total   time.Duration
	beganAt time.Time
	holds   int
	changes chan struct{}
}

func (self *personWait) add(waitedTime time.Duration) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.total += waitedTime
	self.changed()
}

func (self *personWait) get() time.Duration {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.holds > 0 {
		return self.total + time.Since(self.beganAt)
	}
	return self.total
}

func Track(ctx context.Context) (context.Context, func() time.Duration) {
	wait := &personWait{changes: make(chan struct{}, 1)}

	return context.WithValue(ctx, key{}, wait), wait.get
}

func Record(ctx context.Context, waitedTime time.Duration) {
	wait, isTracked := ctx.Value(key{}).(*personWait)
	if !isTracked || waitedTime <= 0 {
		return
	}

	wait.add(waitedTime)
}

func Begin(ctx context.Context) func() {
	wait, isTracked := ctx.Value(key{}).(*personWait)
	if !isTracked {
		return func() {}
	}
	wait.mutex.Lock()
	if wait.holds == 0 {
		wait.beganAt = time.Now()
		wait.changed()
	}
	wait.holds++
	wait.mutex.Unlock()
	return func() {
		wait.mutex.Lock()
		defer wait.mutex.Unlock()
		wait.holds--
		if wait.holds == 0 {
			wait.total += time.Since(wait.beganAt)
			wait.beganAt = time.Time{}
			wait.changed()
		}
	}
}

func (self *personWait) changed() {
	close(self.changes)
	self.changes = make(chan struct{})
}

type Observation struct {
	WaitTime  time.Duration
	IsWaiting bool
	Changes   <-chan struct{}
}

func Observe(ctx context.Context) Observation {
	wait, isTracked := ctx.Value(key{}).(*personWait)
	if !isTracked {
		return Observation{}
	}
	wait.mutex.Lock()
	defer wait.mutex.Unlock()
	waitTime := wait.total
	if wait.holds > 0 {
		waitTime += time.Since(wait.beganAt)
	}
	return Observation{WaitTime: waitTime, IsWaiting: wait.holds > 0, Changes: wait.changes}
}
