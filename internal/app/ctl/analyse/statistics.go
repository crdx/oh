package analyse

import (
	"time"
)

type SessionStatistics struct {
	Name      string             `json:"name"`
	Provider  string             `json:"provider"`
	Model     string             `json:"model,omitempty"`
	Effort    string             `json:"effort,omitempty"`
	StartedAt time.Time          `json:"started"`
	EndedAt   time.Time          `json:"ended"`
	Cache     CacheStatistics    `json:"cache"`
	Activity  ActivityStatistics `json:"activity"`
	Faults    FaultStatistics    `json:"faults"`
	Tools     []ToolStatistics   `json:"tools,omitempty"`
	Spend     float64            `json:"spend"`
	IsPriced  bool               `json:"isPriced"`
}

type CacheStatistics struct {
	Provider        string `json:"provider,omitempty"`
	Sessions        int    `json:"sessions"`
	Requests        int    `json:"requests"`
	Hits            int    `json:"hits"`
	Misses          int    `json:"misses"`
	InputTokens     int64  `json:"inputTokens"`
	CachedTokens    int64  `json:"cachedTokens"`
	WrittenTokens   int64  `json:"writtenTokens"`
	OutputTokens    int64  `json:"outputTokens"`
	PeakInputTokens int64  `json:"peakInputTokens"`
}

type ActivityStatistics struct {
	Provider        string        `json:"provider,omitempty"`
	Sessions        int           `json:"sessions"`
	Turns           int           `json:"turns"`
	Prompts         int           `json:"prompts"`
	Replies         int           `json:"replies"`
	ReasoningBlocks int           `json:"reasoningBlocks"`
	ToolCalls       int           `json:"toolCalls"`
	TurnTime        time.Duration `json:"turnTime"`
	LongestTurn     time.Duration `json:"longestTurn"`
	SessionTime     time.Duration `json:"sessionTime"`
}

type FaultStatistics struct {
	Provider       string            `json:"provider,omitempty"`
	Sessions       int               `json:"sessions"`
	Retries        int               `json:"retries"`
	Interruptions  int               `json:"interruptions"`
	Failures       int               `json:"failures"`
	SilentTurns    int               `json:"silentTurns"`
	PrefixRewrites int               `json:"prefixRewrites"`
	CacheRebuilds  RebuildStatistics `json:"cacheRebuilds"`
}

type RebuildStatistics struct {
	Reopenings    int   `json:"reopenings"`
	Expiries      int   `json:"expiries"`
	Settlements   int   `json:"settlements"`
	Rebuilds      int   `json:"rebuilds"`
	WrittenTokens int64 `json:"writtenTokens"`
}

type ToolStatistics struct {
	Name          string        `json:"name"`
	Calls         int           `json:"calls"`
	Failures      int           `json:"failures"`
	Cancellations int           `json:"cancellations"`
	Took          time.Duration `json:"took"`
}

type ModelStatistics struct {
	Provider      string  `json:"provider"`
	Model         string  `json:"model"`
	Sessions      int     `json:"sessions"`
	Requests      int     `json:"requests"`
	InputTokens   int64   `json:"inputTokens"`
	CachedTokens  int64   `json:"cachedTokens"`
	WrittenTokens int64   `json:"writtenTokens"`
	OutputTokens  int64   `json:"outputTokens"`
	Spend         float64 `json:"spend"`
	IsPriced      bool    `json:"isPriced"`
}

type usageReport struct {
	inputTokens   int64
	cachedTokens  int64
	writtenTokens int64
	outputTokens  int64
}

func (self *CacheStatistics) FreshTokens() int64 {
	return max(self.InputTokens-self.CachedTokens-self.WrittenTokens, 0)
}

func (self *CacheStatistics) AverageInputTokens() int64 {
	if self.Requests <= 0 {
		return 0
	}

	return self.InputTokens / int64(self.Requests)
}

func (self *CacheStatistics) record(report usageReport) {
	self.Requests++
	self.InputTokens += report.inputTokens
	self.CachedTokens += report.cachedTokens
	self.WrittenTokens += report.writtenTokens
	self.OutputTokens += report.outputTokens
	self.PeakInputTokens = max(self.PeakInputTokens, report.inputTokens)
	if report.cachedTokens > 0 {
		self.Hits++
	} else {
		self.Misses++
	}
}

func (self *CacheStatistics) recordLateOutput(outputTokens int64) {
	if outputTokens <= 0 || self.Requests == 0 {
		return
	}

	self.OutputTokens += outputTokens
}

func (self *CacheStatistics) add(statisticsToAdd CacheStatistics) {
	self.Sessions += statisticsToAdd.Sessions
	self.Requests += statisticsToAdd.Requests
	self.Hits += statisticsToAdd.Hits
	self.Misses += statisticsToAdd.Misses
	self.InputTokens += statisticsToAdd.InputTokens
	self.CachedTokens += statisticsToAdd.CachedTokens
	self.WrittenTokens += statisticsToAdd.WrittenTokens
	self.OutputTokens += statisticsToAdd.OutputTokens
	self.PeakInputTokens = max(self.PeakInputTokens, statisticsToAdd.PeakInputTokens)
}

func (self *ActivityStatistics) AverageTurn() time.Duration {
	if self.Turns <= 0 {
		return 0
	}

	return self.TurnTime / time.Duration(self.Turns)
}

func (self *ActivityStatistics) add(statisticsToAdd ActivityStatistics) {
	self.Sessions += statisticsToAdd.Sessions
	self.Turns += statisticsToAdd.Turns
	self.Prompts += statisticsToAdd.Prompts
	self.Replies += statisticsToAdd.Replies
	self.ReasoningBlocks += statisticsToAdd.ReasoningBlocks
	self.ToolCalls += statisticsToAdd.ToolCalls
	self.TurnTime += statisticsToAdd.TurnTime
	self.LongestTurn = max(self.LongestTurn, statisticsToAdd.LongestTurn)
	self.SessionTime += statisticsToAdd.SessionTime
}

func (self *FaultStatistics) IsQuiet() bool {
	return self.Retries == 0 &&
		self.Interruptions == 0 &&
		self.Failures == 0 &&
		self.SilentTurns == 0 &&
		self.PrefixRewrites == 0 &&
		self.CacheRebuilds.Count() == 0
}

func (self *FaultStatistics) add(statisticsToAdd FaultStatistics) {
	self.Sessions += statisticsToAdd.Sessions
	self.Retries += statisticsToAdd.Retries
	self.Interruptions += statisticsToAdd.Interruptions
	self.Failures += statisticsToAdd.Failures
	self.SilentTurns += statisticsToAdd.SilentTurns
	self.PrefixRewrites += statisticsToAdd.PrefixRewrites
	self.CacheRebuilds.add(statisticsToAdd.CacheRebuilds)
}

func (self *RebuildStatistics) Count() int {
	return self.Reopenings + self.Expiries + self.Settlements + self.Rebuilds
}

func (self *RebuildStatistics) add(statisticsToAdd RebuildStatistics) {
	self.Reopenings += statisticsToAdd.Reopenings
	self.Expiries += statisticsToAdd.Expiries
	self.Settlements += statisticsToAdd.Settlements
	self.Rebuilds += statisticsToAdd.Rebuilds
	self.WrittenTokens += statisticsToAdd.WrittenTokens
}

func (self *ToolStatistics) AverageCall() time.Duration {
	if self.Calls <= 0 {
		return 0
	}

	return self.Took / time.Duration(self.Calls)
}

func (self *ToolStatistics) add(statisticsToAdd ToolStatistics) {
	self.Calls += statisticsToAdd.Calls
	self.Failures += statisticsToAdd.Failures
	self.Cancellations += statisticsToAdd.Cancellations
	self.Took += statisticsToAdd.Took
}

func (self *ModelStatistics) FreshTokens() int64 {
	return max(self.InputTokens-self.CachedTokens-self.WrittenTokens, 0)
}

func (self *ModelStatistics) add(statisticsToAdd ModelStatistics) {
	self.Sessions += statisticsToAdd.Sessions
	self.Requests += statisticsToAdd.Requests
	self.InputTokens += statisticsToAdd.InputTokens
	self.CachedTokens += statisticsToAdd.CachedTokens
	self.WrittenTokens += statisticsToAdd.WrittenTokens
	self.OutputTokens += statisticsToAdd.OutputTokens
	self.Spend += statisticsToAdd.Spend
	self.IsPriced = self.IsPriced || statisticsToAdd.IsPriced
}

func (self *SessionStatistics) Duration() time.Duration {
	if self.EndedAt.Before(self.StartedAt) {
		return 0
	}

	return self.EndedAt.Sub(self.StartedAt)
}
