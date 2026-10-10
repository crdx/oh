package diagnostics

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"runtime/trace"
	"sync"
	"time"
)

const (
	threshold      = 250 * time.Millisecond
	stackBytes     = 1 << 16
	traceWindow    = 10 * time.Second
	traceByteLimit = 16 << 20
)

type Watchdog struct {
	sessionDirectory string
	recorder         *trace.FlightRecorder

	mutex     sync.Mutex
	timer     *time.Timer
	isClosed  bool
	work      string
	startedAt time.Time
	stacks    []byte
	region    *trace.Region

	entryLock sync.Mutex
}

func Watch(sessionDirectory string) *Watchdog {
	return watchWith(sessionDirectory, startRecorder())
}

func watchWith(sessionDirectory string, recorder *trace.FlightRecorder) *Watchdog {
	return &Watchdog{sessionDirectory: sessionDirectory, recorder: recorder}
}

func startRecorder() *trace.FlightRecorder {
	recorder := trace.NewFlightRecorder(trace.FlightRecorderConfig{
		MinAge:   traceWindow,
		MaxBytes: traceByteLimit,
	})
	if recorder.Start() != nil {
		return nil
	}

	return recorder
}

func (self *Watchdog) Begin(work string) func() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.isClosed {
		return func() {}
	}

	self.work = work
	self.startedAt = time.Now()
	self.stacks = nil
	if self.timer == nil {
		self.timer = time.AfterFunc(threshold, self.capture)
	} else {
		self.timer.Reset(threshold)
	}
	self.region = trace.StartRegion(context.Background(), work)

	return self.end
}

func (self *Watchdog) Close() {
	self.mutex.Lock()
	self.isClosed = true
	if self.timer != nil {
		self.timer.Stop()
	}
	self.mutex.Unlock()

	self.entryLock.Lock()
	defer self.entryLock.Unlock()

	if self.recorder != nil {
		self.recorder.Stop()
	}
}

func (self *Watchdog) end() {
	self.mutex.Lock()
	work, took, stacks, region := self.work, time.Since(self.startedAt), self.stacks, self.region
	self.work = ""
	self.stacks = nil
	self.region = nil
	if self.timer != nil {
		self.timer.Stop()
	}
	self.mutex.Unlock()

	if region != nil {
		region.End()
	}

	if stacks != nil {
		go self.write(work, took, stacks)
	}
}

func (self *Watchdog) capture() {
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
	self.entryLock.Lock()
	defer self.entryLock.Unlock()

	endedAt := time.Now()
	entry := fmt.Appendf(nil, "=== %s: %s held the drawing thread for %s\n%s\n",
		endedAt.Format(time.RFC3339Nano), work, took.Round(time.Millisecond), stacks)

	writeEntry(self.sessionDirectory, StallDirectoryName, endedAt, ".txt", entry)

	if snapshot := self.traceSnapshot(); snapshot != nil {
		writeEntry(self.sessionDirectory, StallDirectoryName, endedAt, ".trace", snapshot)
	}
}

func (self *Watchdog) traceSnapshot() []byte {
	if self.recorder == nil || !self.recorder.Enabled() {
		return nil
	}

	var buffer bytes.Buffer
	if _, err := self.recorder.WriteTo(&buffer); err != nil {
		return nil
	}

	return buffer.Bytes()
}
