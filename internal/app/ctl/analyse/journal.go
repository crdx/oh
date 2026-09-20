package analyse

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
)

const unknownProvider = "unknown"

type journalMeta struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Effort   string `json:"effort"`
}

type journalTally struct {
	statistics   SessionStatistics
	tools        map[string]*ToolStatistics
	usageReports int
	untimedTurns int
}

func analyseJournal(directory string, name string) (SessionStatistics, bool, error) {
	tally := journalTally{
		statistics: SessionStatistics{Name: name},
		tools:      map[string]*ToolStatistics{},
	}

	if err := session.Records(directory, name, tally.record); err != nil {
		return SessionStatistics{}, false, fmt.Errorf("could not analyse %s: %w", name, err)
	}

	statistics, isWhole := tally.finish()

	return statistics, isWhole, nil
}

func (self *journalTally) record(line session.Line) error {
	if line.Time.After(self.statistics.EndedAt) {
		self.statistics.EndedAt = line.Time
	}

	switch line.Kind {
	case session.Head:
		var meta journalMeta
		if err := json.Unmarshal(line.Meta, &meta); err != nil {
			return err
		}
		self.statistics.Provider = meta.Provider
		self.statistics.Model = meta.Model
		self.statistics.Effort = meta.Effort
		self.statistics.StartedAt = line.Time
	case session.TurnCompletion:
		self.statistics.Activity.Turns++
		if line.Turn == nil || line.Turn.Took <= 0 {
			self.untimedTurns++
			break
		}
		self.statistics.Activity.TurnTime += line.Turn.Took
		self.statistics.Activity.LongestTurn = max(self.statistics.Activity.LongestTurn, line.Turn.Took)
	case session.Event:
		if line.Event != nil {
			self.recordEvent(*line.Event)
		}
	case session.Item:
	}

	return nil
}

func (self *journalTally) recordEvent(event agent.Event) {
	switch event.Kind {
	case agent.UserMessageEvent:
		self.statistics.Activity.Prompts++
	case agent.ModelMessageEvent:
		self.statistics.Activity.Replies++
	case agent.ModelReasoningEvent:
		self.statistics.Activity.ReasoningBlocks++
	case agent.ToolCallRequestEvent:
		self.statistics.Activity.ToolCalls++
	case agent.ToolCallResultEvent:
		self.recordToolCall(event)
	case agent.RetryingEvent:
		self.statistics.Faults.Retries++
	case agent.InterruptionEvent:
		self.statistics.Faults.Interruptions++
	case agent.FailureEvent:
		self.statistics.Faults.Failures++
	case agent.SilentTurnEvent:
		self.statistics.Faults.SilentTurns++
	case agent.PrefixRewriteEvent:
		self.statistics.Faults.PrefixRewrites++
	case agent.CacheRebuildEvent:
		self.recordRebuild(event)
		return
	case agent.StartupEvent, agent.StateChangeEvent:
	}

	self.recordUsage(event)
}

func (self *journalTally) recordToolCall(event agent.Event) {
	statistics := self.tools[event.Name]
	if statistics == nil {
		statistics = &ToolStatistics{Name: event.Name}
		self.tools[event.Name] = statistics
	}

	statistics.Calls++
	statistics.Took += event.Took

	switch event.Status {
	case agent.ErrorStatus:
		statistics.Failures++
	case agent.CancelledStatus:
		statistics.Cancellations++
	case agent.InfoStatus, agent.SuccessStatus, agent.WarningStatus:
	}
}

func (self *journalTally) recordRebuild(event agent.Event) {
	rebuilds := &self.statistics.Faults.CacheRebuilds

	switch agent.CacheCause(event.Name) {
	case agent.CacheReopened:
		rebuilds.Reopenings++
	case agent.CacheExpired:
		rebuilds.Expiries++
	case agent.CacheSettling:
		rebuilds.Settlements++
	case agent.CacheRebuilt:
		rebuilds.Rebuilds++
	default:
		rebuilds.Rebuilds++
	}

	if event.Usage != nil && event.Usage.Cache != nil {
		rebuilds.WrittenTokens += int64(event.Usage.Cache.WriteTokens)
	}
}

func (self *journalTally) recordUsage(event agent.Event) {
	if event.Usage == nil {
		return
	}

	if event.Usage.InputTokens <= 0 {
		self.statistics.Cache.recordLateOutput(int64(event.Usage.OutputTokens))
		return
	}

	self.usageReports++
	if event.Usage.Cache == nil {
		return
	}

	self.statistics.Cache.record(usageReport{
		inputTokens:   int64(event.Usage.InputTokens),
		cachedTokens:  int64(event.Usage.Cache.ReadTokens),
		writtenTokens: int64(event.Usage.Cache.WriteTokens),
		outputTokens:  int64(event.Usage.OutputTokens),
	})
}

func (self *journalTally) finish() (SessionStatistics, bool) {
	statistics := self.statistics
	if statistics.Provider == "" {
		statistics.Provider = unknownProvider
	}

	statistics.Activity.Sessions = 1
	statistics.Activity.SessionTime = statistics.Duration()
	statistics.Faults.Sessions = 1

	statistics.Tools = make([]ToolStatistics, 0, len(self.tools))
	for _, toolStatistics := range self.tools {
		statistics.Tools = append(statistics.Tools, *toolStatistics)
	}
	slices.SortFunc(statistics.Tools, func(first ToolStatistics, second ToolStatistics) int {
		return strings.Compare(first.Name, second.Name)
	})

	isWhole := self.usageReports > 0 &&
		statistics.Cache.Requests == self.usageReports &&
		statistics.Cache.OutputTokens > 0 &&
		self.untimedTurns == 0

	if isWhole {
		statistics.Cache.Sessions = 1
	}

	return statistics, isWhole
}
