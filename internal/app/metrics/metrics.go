package metrics

import (
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
)

const tokensPerPricedUnit = 1_000_000

type Settings struct {
	ContextWindowTokens int
	Prices              *agent.TokenPrices
}

type Tracker struct {
	contextWindowTokens int
	prices              *agent.TokenPrices
	inputTokens         int
	turnsTaken          int
	cacheReadTokens     int
	cacheAskedTokens    int
	spend               float64
}

func New(settings Settings) Tracker {
	return Tracker{contextWindowTokens: settings.ContextWindowTokens, prices: settings.Prices}
}

func (self *Tracker) BeginTurn() {
	self.turnsTaken++
}

func (self *Tracker) Record(event agent.Event) {
	if event.Kind == agent.CacheRebuildEvent {
		return
	}
	if event.Usage != nil && event.Usage.InputTokens > 0 {
		self.inputTokens = event.Usage.InputTokens
	}
	if event.Usage != nil && event.Usage.InputTokens > 0 {
		self.cacheAskedTokens += event.Usage.InputTokens
		if event.Usage.Cache != nil {
			self.cacheReadTokens += event.Usage.Cache.ReadTokens
		}
	}

	self.spend += self.spendOn(event)
}

func (self *Tracker) CacheUsage() (int, int) {
	return self.cacheReadTokens, self.cacheAskedTokens
}

func (self *Tracker) Spend() (float64, bool) {
	if self.prices == nil {
		return 0, false
	}

	return self.spend, true
}

func (self *Tracker) Restore(events []agent.Event, turns []session.TurnSummary) {
	self.inputTokens = 0
	self.turnsTaken = len(turns)
	self.spend = 0

	for _, event := range events {
		if event.Kind == agent.CacheRebuildEvent {
			continue
		}

		self.spend += self.spendOn(event)
	}

	if len(turns) > 0 && turns[len(turns)-1].InputTokens > 0 {
		self.inputTokens = turns[len(turns)-1].InputTokens
		return
	}

	for _, event := range events {
		if event.Usage != nil && event.Usage.InputTokens > 0 {
			self.inputTokens = event.Usage.InputTokens
		}
	}
}

func (self *Tracker) TurnCount() int {
	return self.turnsTaken
}

func (self *Tracker) ContextUsage() (int, int) {
	return self.inputTokens, self.contextWindowTokens
}

func (self *Tracker) spendOn(event agent.Event) float64 {
	if self.prices == nil || event.Usage == nil {
		return 0
	}

	return Spend(*self.prices, *event.Usage)
}

func Spend(prices agent.TokenPrices, usage agent.Usage) float64 {
	var readTokens, writeTokens int
	if usage.Cache != nil {
		readTokens = usage.Cache.ReadTokens
		writeTokens = usage.Cache.WriteTokens
	}

	freshTokens := max(usage.InputTokens-readTokens-writeTokens, 0)

	dollars := float64(freshTokens)*prices.Input +
		float64(readTokens)*prices.CacheRead +
		float64(writeTokens)*prices.CacheWrite +
		float64(usage.OutputTokens)*prices.Output

	return dollars / tokensPerPricedUnit
}
