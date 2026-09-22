package req

import (
	"context"
	"io"
	"sync"
	"time"

	"crdx.org/oh/internal/util"
)

const idleCheckPeriod = 5 * time.Second

type IdleError struct {
	After time.Duration
}

func (self *IdleError) Error() string {
	return "the stream sent nothing for " + util.CompactDuration(self.After)
}

func (*IdleError) Retriable() bool { return true }

func (*IdleError) RetryAfter() time.Duration { return 0 }

type idleWatchdog struct {
	after  time.Duration
	every  time.Duration
	now    func() time.Time
	cancel context.CancelFunc

	mutex      sync.Mutex
	timer      *time.Timer
	quietSince time.Time
	hasExpired bool
}

func newIdleWatchdog(
	after time.Duration, every time.Duration, now func() time.Time, cancel context.CancelFunc,
) *idleWatchdog {
	watchdog := &idleWatchdog{
		after:      after,
		every:      every,
		now:        now,
		cancel:     cancel,
		quietSince: util.WallClock(now()),
	}

	watchdog.timer = time.AfterFunc(min(after, every), watchdog.check)

	return watchdog
}

func (self *idleWatchdog) check() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	silence := util.WallClock(self.now()).Sub(self.quietSince)
	if silence < self.after {
		self.timer.Reset(min(self.after-silence, self.every))

		return
	}

	self.hasExpired = true
	self.cancel()
}

func (self *idleWatchdog) watch(body io.ReadCloser) io.ReadCloser {
	return &idleBody{ReadCloser: body, watchdog: self}
}

func (self *idleWatchdog) extend() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if !self.hasExpired {
		self.quietSince = util.WallClock(self.now())
	}
}

func (self *idleWatchdog) stop() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.timer.Stop()
	self.cancel()
}

func (self *idleWatchdog) explain(err error) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.hasExpired {
		return &IdleError{After: self.after}
	}

	return err
}

type idleBody struct {
	io.ReadCloser

	watchdog *idleWatchdog
}

func (self *idleBody) Read(buffer []byte) (int, error) {
	count, err := self.ReadCloser.Read(buffer)

	if count > 0 {
		self.watchdog.extend()
	}

	if err != nil {
		return count, self.watchdog.explain(err)
	}

	return count, nil
}

func (self *idleBody) Close() error {
	self.watchdog.stop()

	return self.ReadCloser.Close()
}
