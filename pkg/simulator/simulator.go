package simulator

import (
	"bytes"
	"net/http"
	"slices"

	"crdx.org/oh/internal/sim"
)

type Scenario = sim.Scenario

type Turn = sim.Turn

type Call = sim.Call

type Duration = sim.Duration

type Request = sim.Request

type Endpoint struct {
	inner *sim.Endpoint
}

func New(scenario *Scenario) *Endpoint {
	if scenario == nil {
		panic("a simulator scenario is required")
	}

	copyScenario := *scenario
	copyScenario.Turns = slices.Clone(scenario.Turns)
	for i := range copyScenario.Turns {
		copyScenario.Turns[i].Think = slices.Clone(copyScenario.Turns[i].Think)
		copyScenario.Turns[i].Calls = slices.Clone(copyScenario.Turns[i].Calls)
	}

	return &Endpoint{inner: sim.New(&copyScenario)}
}

func (self *Endpoint) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	self.inner.ServeHTTP(writer, request)
}

func (self *Endpoint) Requests() []Request {
	requests := self.inner.Requests()
	for i := range requests {
		requests[i].Input = slices.Clone(requests[i].Input)
		for entry := range requests[i].Input {
			requests[i].Input[entry].Raw = bytes.Clone(requests[i].Input[entry].Raw)
		}
		requests[i].Tools = slices.Clone(requests[i].Tools)
	}
	return requests
}

func (self *Endpoint) Addresses(base string) map[string]string {
	return self.inner.Addresses(base)
}
