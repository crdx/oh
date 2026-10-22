package tasks

import (
	"sync"
	"time"
)

type slotWait struct {
	mutex   sync.Mutex
	total   time.Duration
	beganAt time.Time
}

func (self *slotWait) begin() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.beganAt = time.Now()
}

func (self *slotWait) end() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if !self.beganAt.IsZero() {
		self.total += time.Since(self.beganAt)
		self.beganAt = time.Time{}
	}
}

func (self *slotWait) get() time.Duration {
	if self == nil {
		return 0
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if !self.beganAt.IsZero() {
		return self.total + time.Since(self.beganAt)
	}
	return self.total
}
