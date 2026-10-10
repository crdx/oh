package harness

import (
	"slices"
	"strings"
	"time"

	"crdx.org/oh/internal/app/metrics"
	"crdx.org/oh/internal/app/painter"
	"crdx.org/oh/internal/app/subagentrecord"
	"crdx.org/oh/internal/app/subagents"
	"crdx.org/oh/pkg/agent"
)

const (
	childDebounce = 5 * time.Second
)

type childState struct {
	manager            *subagents.Manager
	directory          string
	reportsDue         []string
	deliveryAt         time.Time
	spend              spendLedger
	viewedConversation string
}

type spendLedger struct {
	prices     map[string]*agent.TokenPrices
	spend      float64
	isUnpriced bool
}

func (self *spendLedger) record(event agent.Event) {
	if origin, isDecoded := subagentrecord.DecodeOrigin(event); isDecoded {
		if self.prices == nil {
			self.prices = map[string]*agent.TokenPrices{}
		}
		self.prices[event.Subagent] = origin.Choice.Prices
		return
	}
	if event.Kind != subagentrecord.Finished {
		return
	}
	prices := self.prices[event.Subagent]
	usage := subagentrecord.UsageOf(event)
	if usage == nil || prices == nil || !prices.IsKnown() {
		self.isUnpriced = true
		return
	}
	self.spend += metrics.Spend(*prices, *usage)
}

func (self *App) getSubagents() []subagents.Snapshot {
	if self.children.manager == nil {
		return nil
	}
	return self.children.manager.ListSnapshots()
}

func (self *App) childSpend() (float64, bool) {
	spend, isPriced := self.children.spend.spend, !self.children.spend.isUnpriced
	if self.children.manager == nil {
		return spend, isPriced
	}
	liveSpend, isLivePriced := self.children.manager.LiveSpend()
	return spend + liveSpend, isPriced && isLivePriced
}

func (self *App) stopChildrenLosingShell() {
	if self.children.manager == nil {
		return
	}
	names := self.children.manager.StopRunning()
	if len(names) == 0 {
		return
	}
	self.pendingNotices.add(agent.Event{Kind: subagentrecord.ShellWithdrawnStop, Name: strings.Join(names, ",")})
	if !self.currentTurn.Running() {
		self.refreshPendingMessages()
	}
}

func (self *App) subagentEvents() <-chan agent.Event {
	if self.children.manager == nil {
		return nil
	}
	return self.children.manager.Events()
}

func (self *App) subagentEvent(event agent.Event) {
	self.recordChildFact(event)
	if event.Kind == subagentrecord.Returned {
		self.children.reportsDue = slices.DeleteFunc(self.children.reportsDue, func(name string) bool { return name == event.Subagent })
		return
	}
	if event.Kind != subagentrecord.Finished || event.Name == string(subagentrecord.Stopped) || hasReturned(self.recordedEvents, event.Subagent) {
		return
	}
	if len(self.children.reportsDue) == 0 {
		self.children.deliveryAt = self.getNow().Add(childDebounce)
	}
	self.children.reportsDue = append(self.children.reportsDue, event.Subagent)
}

func (self *App) recordChildFact(event agent.Event) {
	painter.IntroduceSubagent(self.introductions, event)
	self.children.spend.record(event)
	self.recordedEvents = append(self.recordedEvents, event)
	self.storeEvent(event)
}

func hasReturned(events []agent.Event, name string) bool {
	isReturned := false
	for _, event := range events {
		if event.Subagent != name {
			continue
		}
		if event.Kind == subagentrecord.Returned || event.Kind == subagentrecord.Sent {
			isReturned = event.Kind == subagentrecord.Returned
		}
	}
	return isReturned
}

func (self *App) nextChildDelivery(time.Time) time.Time {
	if self.currentTurn.Running() || len(self.children.reportsDue) == 0 {
		return time.Time{}
	}
	return self.children.deliveryAt
}

func (self *App) deliverChildCompletions() {
	if self.queueReadyChildCompletions() {
		self.startTurn()
	}
}

func (self *App) queueReadyChildCompletions() bool {
	if self.currentTurn.Running() || len(self.children.reportsDue) == 0 || self.getNow().Before(self.children.deliveryAt) {
		return false
	}
	names := slices.DeleteFunc(self.children.reportsDue, func(name string) bool {
		return self.children.manager != nil && self.children.manager.WasReturned(name)
	})
	self.children.reportsDue = nil
	self.children.deliveryAt = time.Time{}
	if len(names) == 0 {
		return false
	}
	message := completionMessage(self.recordedEvents, names, self.children.manager.ScratchNote)
	self.pendingNotices.add(subagentrecord.DeliveryEvent(names, message, completionReports(self.recordedEvents, names)))
	self.refreshPendingMessages()
	return true
}

func completionMessage(events []agent.Event, names []string, scratchNote func(string) string) string {
	var results []string
	for _, name := range names {
		var answer, state, failure string
		for _, event := range events {
			if event.Kind == subagentrecord.Finished && event.Subagent == name {
				answer = event.Text
				state = event.Name
				failure = subagentrecord.FailureOf(event)
			}
		}
		results = append(results, describeCompletion(name, state+scratchNote(name), answer, failure))
	}
	return subagentrecord.CompletionHeading(names) + "\n\n" + strings.Join(results, "\n\n")
}

func completionReports(events []agent.Event, names []string) []subagentrecord.Report {
	var reports []subagentrecord.Report
	for _, name := range names {
		report := subagentrecord.Report{Name: name}
		for _, event := range events {
			if event.Kind == subagentrecord.Finished && event.Subagent == name {
				report.State = subagentrecord.State(event.Name)
				report.Answer = event.Text
				report.Failure = subagentrecord.FailureOf(event)
			}
		}
		reports = append(reports, report)
	}
	return reports
}

func describeCompletion(name string, state string, answer string, failure string) string {
	switch {
	case answer != "" && failure != "":
		return name + " " + state + ": " + answer + " (" + failure + ")"
	case answer != "":
		return name + " " + state + ": " + answer
	case failure != "":
		return name + " " + state + ": " + failure
	}
	return name + " " + state + " without an answer"
}

func (self *App) endChildren() {
	if self.children.manager == nil {
		return
	}
	go self.children.manager.Close()
	for event := range self.children.manager.Events() {
		self.recordChildFact(event)
	}
}

func (self *App) restoreChildren(events []agent.Event) {
	for _, event := range events {
		self.children.spend.record(event)
	}
	finishes := subagents.Endings(self.children.directory, events)
	if self.children.manager != nil {
		finishes = self.children.manager.Restore(events)
	}
	for _, finish := range finishes {
		self.recordChildFact(finish)
	}
	self.queueUndeliveredReports(self.recordedEvents)
}

func (self *App) queueUndeliveredReports(events []agent.Event) {
	pendingChildren := map[string]bool{}
	var order []string
	for _, event := range events {
		if event.Kind == subagentrecord.Finished && event.Name != string(subagentrecord.Stopped) {
			pendingChildren[event.Subagent] = true
			order = append(order, event.Subagent)
		}
		if event.Kind == subagentrecord.ReportsDelivered {
			for name := range strings.SplitSeq(event.Name, ",") {
				delete(pendingChildren, name)
			}
		}
	}
	for _, name := range order {
		if pendingChildren[name] && !hasReturned(events, name) && !slices.Contains(self.children.reportsDue, name) {
			self.children.reportsDue = append(self.children.reportsDue, name)
		}
	}
	if len(self.children.reportsDue) > 0 {
		self.children.deliveryAt = self.getNow()
	}
}
