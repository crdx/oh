package req

import (
	"context"
	"io"
	"sync/atomic"
	"time"

	"crdx.org/oh/internal/util"
)

type IdleError struct {
	After time.Duration
}

func (self *IdleError) Error() string {
	return "the stream sent nothing for " + util.CompactDuration(self.After)
}

func (*IdleError) Retriable() bool { return true }

func (*IdleError) RetryAfter() time.Duration { return 0 }

type idleWatchdog struct {
	after      time.Duration
	cancel     context.CancelFunc
	timer      *time.Timer
	hasExpired atomic.Bool
}

func newIdleWatchdog(after time.Duration, cancel context.CancelFunc) *idleWatchdog {
	return &idleWatchdog{after: after, cancel: cancel}
}

func (self *idleWatchdog) watch(body io.ReadCloser) io.ReadCloser {
	self.timer = time.AfterFunc(self.after, func() {
		self.hasExpired.Store(true)
		self.cancel()
	})

	return &idleBody{ReadCloser: body, watchdog: self}
}

func (self *idleWatchdog) extend() {
	if self.timer != nil && !self.hasExpired.Load() {
		self.timer.Reset(self.after)
	}
}

func (self *idleWatchdog) stop() {
	if self.timer != nil {
		self.timer.Stop()
	}

	self.cancel()
}

func (self *idleWatchdog) explain(err error) error {
	if self.hasExpired.Load() {
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
