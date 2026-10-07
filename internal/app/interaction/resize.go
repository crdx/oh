package interaction

import (
	"os"
	"time"
)

const resizeQuiet = 100 * time.Millisecond

type ResizeBatch struct {
	timer    *time.Timer
	isActive bool
	isDirty  bool
}

func NewResizeBatch() *ResizeBatch {
	timer := time.NewTimer(resizeQuiet)
	timer.Stop()
	return &ResizeBatch{timer: timer}
}

func (self *ResizeBatch) Ready() <-chan time.Time {
	return self.timer.C
}

func (self *ResizeBatch) Stop() {
	self.timer.Stop()
}

func (self *ResizeBatch) Signal(signals <-chan os.Signal) bool {
	for range len(signals) {
		<-signals
	}
	isFirst := !self.isActive
	self.isActive = true
	if !isFirst {
		self.isDirty = true
	}
	self.timer.Stop()
	self.timer.Reset(resizeQuiet)
	return isFirst
}

func (self *ResizeBatch) Finish(signals <-chan os.Signal) bool {
	select {
	case <-signals:
		self.Signal(signals)
		return false
	default:
	}

	isDirty := self.isDirty
	self.isActive = false
	self.isDirty = false
	return isDirty
}
