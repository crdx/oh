package chatcompletions_test

import "crdx.org/io/internal/req"

type countingObserver struct {
	requests int
}

func (self *countingObserver) Start(req.Request) req.ExchangeObserver {
	self.requests++

	return nil
}
