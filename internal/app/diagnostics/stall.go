package diagnostics

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

const (
	threshold   = 250 * time.Millisecond
	checkPeriod = 50 * time.Millisecond
	stackBytes  = 1 << 16
)

type Watchdog struct {
	sessionDirectory string

	mutex     sync.Mutex
	work      string
	startedAt time.Time
	stacks    []byte

	stop chan struct{}
}

func Watch(sessionDirectory string) *Watchdog {
	self := &Watchdog{sessionDirectory: sessionDirectory, stop: make(chan struct{})}

	go self.run()

	return self
}

func (self *Watchdog) Begin(work string) func() {
	self.mutex.Lock()
	self.work = work
	self.startedAt = time.Now()
	self.stacks = nil
	self.mutex.Unlock()

	return self.end
}

func (self *Watchdog) Close() {
	close(self.stop)
}

func (self *Watchdog) end() {
	self.mutex.Lock()
	work, took, stacks := self.work, time.Since(self.startedAt), self.stacks
	self.work = ""
	self.stacks = nil
	self.mutex.Unlock()

	if stacks != nil {
		go self.write(work, took, stacks)
	}
}

func (self *Watchdog) run() {
	ticker := time.NewTicker(checkPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-self.stop:
			return
		case <-ticker.C:
			self.check()
		}
	}
}

func (self *Watchdog) check() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.work == "" || self.stacks != nil || time.Since(self.startedAt) < threshold {
		return
	}

	self.stacks = allStacks()
}

func allStacks() []byte {
	buffer := make([]byte, stackBytes)
	for {
		length := runtime.Stack(buffer, true)
		if length < len(buffer) {
			return buffer[:length]
		}
		buffer = make([]byte, 2*len(buffer))
	}
}

func (self *Watchdog) write(work string, took time.Duration, stacks []byte) {
	endedAt := time.Now()
	entry := fmt.Appendf(nil, "=== %s: %s held the drawing thread for %s\n%s\n",
		endedAt.Format(time.RFC3339Nano), work, took.Round(time.Millisecond), stacks)

	writeEntry(self.sessionDirectory, StallDirectoryName, endedAt, ".txt", entry)
}
