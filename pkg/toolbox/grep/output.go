package grep

import (
	"bytes"
	"context"
	"sync"

	"crdx.org/oh/internal/util"
)

const maxSearchOutput = util.MaxSearchBytes + 4096

type searchOutput struct {
	mutex       sync.Mutex
	buffer      bytes.Buffer
	cancel      context.CancelFunc
	isTruncated bool
}

func newSearchOutput(cancel context.CancelFunc) *searchOutput {
	return &searchOutput{cancel: cancel}
}

func (self *searchOutput) Write(data []byte) (int, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	room := maxSearchOutput - self.buffer.Len()
	if room > 0 {
		_, _ = self.buffer.Write(data[:min(room, len(data))])
	}
	if len(data) > room {
		self.isTruncated = true
		self.cancel()
	}
	return len(data), nil
}

func (self *searchOutput) String() string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.buffer.String()
}

func (self *searchOutput) IsTruncated() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.isTruncated
}
