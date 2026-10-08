package diagnostics

import (
	"bytes"
	"runtime/pprof"
	"time"
)

const profileSegment = 15 * time.Minute

type Profiler struct {
	sessionDirectory string
	buffer           bytes.Buffer
	startedAt        time.Time
	stop             chan struct{}
	done             chan struct{}
}

func Profile(sessionDirectory string) (*Profiler, error) {
	self := &Profiler{
		sessionDirectory: sessionDirectory,
		stop:             make(chan struct{}),
		done:             make(chan struct{}),
	}

	if err := self.begin(); err != nil {
		return nil, err
	}

	go self.run()

	return self, nil
}

func (self *Profiler) Close() {
	close(self.stop)
	<-self.done
}

func (self *Profiler) begin() error {
	self.buffer.Reset()
	self.startedAt = time.Now()

	return pprof.StartCPUProfile(&self.buffer)
}

func (self *Profiler) run() {
	defer close(self.done)

	ticker := time.NewTicker(profileSegment)
	defer ticker.Stop()

	for {
		select {
		case <-self.stop:
			self.finishSegment()
			return
		case <-ticker.C:
			if !self.rotate() {
				<-self.stop
				return
			}
		}
	}
}

func (self *Profiler) rotate() bool {
	self.finishSegment()

	return self.begin() == nil
}

func (self *Profiler) finishSegment() {
	pprof.StopCPUProfile()

	writeEntry(self.sessionDirectory, CPUDirectoryName, self.startedAt, ".pprof", self.buffer.Bytes())
}
