package diagnostics

import (
	"bytes"
	"runtime"
	"runtime/pprof"
	"time"
)

type HeapProfiler struct {
	sessionDirectory string
	stop             chan struct{}
	done             chan struct{}
}

func ProfileHeap(sessionDirectory string) *HeapProfiler {
	self := &HeapProfiler{
		sessionDirectory: sessionDirectory,
		stop:             make(chan struct{}),
		done:             make(chan struct{}),
	}

	go self.run()

	return self
}

func (self *HeapProfiler) Close() {
	close(self.stop)
	<-self.done
}

func (self *HeapProfiler) run() {
	defer close(self.done)

	ticker := time.NewTicker(profileSegment)
	defer ticker.Stop()

	for {
		select {
		case <-self.stop:
			self.snapshot()
			return
		case <-ticker.C:
			self.snapshot()
		}
	}
}

func (self *HeapProfiler) snapshot() {
	runtime.GC()

	var buffer bytes.Buffer
	if pprof.Lookup("heap").WriteTo(&buffer, 0) != nil {
		return
	}

	writeEntry(self.sessionDirectory, HeapDirectoryName, time.Now(), ".pprof", buffer.Bytes())
}
