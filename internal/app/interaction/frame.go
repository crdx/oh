package interaction

import "time"

const (
	turnFrame    = time.Second / 30
	refreshFloor = turnFrame
)

type frameGate struct {
	interval   time.Duration
	drawnAt    time.Time
	timer      *time.Timer
	isDrawOwed bool
}

func newFrameGate(interval time.Duration) *frameGate {
	timer := time.NewTimer(time.Hour)
	timer.Stop()

	return &frameGate{interval: interval, timer: timer}
}

func (self *frameGate) shouldWait(at time.Time) bool {
	wait := self.interval - at.Sub(self.drawnAt)
	if wait <= 0 {
		return false
	}

	if !self.isDrawOwed {
		self.timer.Reset(wait)
		self.isDrawOwed = true
	}

	return true
}

func (self *frameGate) due() <-chan time.Time {
	return self.timer.C
}

func (self *frameGate) drawn(at time.Time) {
	self.drawnAt = at

	if self.isDrawOwed {
		self.timer.Stop()
		self.isDrawOwed = false
	}
}

func (self *frameGate) stop() {
	self.timer.Stop()
}
