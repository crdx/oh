package tasks

import "context"

func (self *Manager) SetMaximumRunning(maximumRunning int) {
	if maximumRunning < 1 {
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.maximumRunning == maximumRunning {
		return
	}
	self.maximumRunning = maximumRunning
	self.notifySlotChange()
}

func (self *Manager) takeSlot(ctx context.Context) error {
	for {
		self.mutex.Lock()
		if err := context.Cause(ctx); err != nil {
			self.mutex.Unlock()
			return err
		}
		select {
		case <-self.done:
			self.mutex.Unlock()
			return errSessionClosed
		default:
		}
		if self.isClosed {
			self.mutex.Unlock()
			return errSessionClosed
		}
		if self.runningCount < self.maximumRunning {
			self.runningCount++
			self.mutex.Unlock()
			return nil
		}
		changes := self.slotChanges
		self.mutex.Unlock()
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-self.done:
			return errSessionClosed
		case <-changes:
		}
	}
}

func (self *Manager) releaseSlot() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.runningCount--
	self.notifySlotChange()
}

func (self *Manager) notifySlotChange() {
	close(self.slotChanges)
	self.slotChanges = make(chan struct{})
}
