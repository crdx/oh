package harness

import (
	"slices"
	"strings"
	"time"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/metrics"
	"crdx.org/oh/internal/app/model"
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
	rotate             func(config.Subagent, model.Defaults) (subagents.Rotation, error)
	directory          string
	reportsDue         []agent.Event
	deliveryAt         time.Time
	runningNames       []string
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

func (self *App) stopChildrenLosing(withdrawnCaps caps.Set) {
	if self.children.manager == nil {
		return
	}
	names := self.children.manager.StopLosing(withdrawnCaps)
	if len(names) == 0 {
		return
	}
	self.pendingNotices.add(subagentrecord.AccessWithdrawnStopEvent(names, withdrawnCaps))
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

func (self *App) subagentActivity() <-chan struct{} {
	if self.children.manager == nil {
		return nil
	}
	return self.children.manager.Activity()
}

func (self *App) subagentEvent(event agent.Event) {
	self.recordChildFact(event)
	if isReported(event) {
		if len(self.children.reportsDue) == 0 {
			self.children.deliveryAt = self.getNow().Add(childDebounce)
		}
		self.children.reportsDue = append(self.children.reportsDue, event)
	}
	self.reportProgress()
}

func (self *App) reportProgress() {
	self.screen.ReportProgress(self.currentTurn.Running() || self.children.isBusy())
}

func (self childState) isBusy() bool {
	return len(self.runningNames) > 0 || len(self.reportsDue) > 0
}

func (self *App) trackRunningChild(event agent.Event) {
	if event.Kind == subagentrecord.Finished {
		self.children.runningNames = slices.DeleteFunc(self.children.runningNames, func(name string) bool { return name == event.Subagent })
		return
	}
	isOpening := event.Kind == subagentrecord.Started || event.Kind == subagentrecord.Sent
	if isOpening && !slices.Contains(self.children.runningNames, event.Subagent) {
		self.children.runningNames = append(self.children.runningNames, event.Subagent)
	}
}

func isReported(event agent.Event) bool {
	return event.Kind == subagentrecord.Received ||
		event.Kind == subagentrecord.Finished && event.Name != string(subagentrecord.Stopped)
}

func (self *App) recordChildFact(event agent.Event) {
	painter.IntroduceSubagent(self.introductions, event)
	self.children.spend.record(event)
	self.trackRunningChild(event)
	self.recordedEvents = append(self.recordedEvents, event)
	if self.currentTurn.Running() && self.currentTurn.painter != nil {
		self.currentTurn.painter.CountUndrawnEvent()
	}
	self.storeEvent(event)
}

func (self *App) nextChildDelivery(time.Time) time.Time {
	if len(self.children.reportsDue) == 0 {
		return time.Time{}
	}
	return self.children.deliveryAt
}

func (self *App) deliverChildCompletions() {
	if self.queueReadyChildCompletions() {
		self.startTurn()
	}
	self.reportProgress()
}

func (self *App) queueReadyChildCompletions() bool {
	if len(self.children.reportsDue) == 0 || self.getNow().Before(self.children.deliveryAt) {
		return false
	}
	due := self.children.reportsDue
	self.children.reportsDue = nil
	self.children.deliveryAt = time.Time{}
	return self.holdNotice(self.childDelivery(due))
}

func (self *App) childDelivery(due []agent.Event) agent.Event {
	var names []string
	for _, event := range due {
		if !slices.Contains(names, event.Subagent) {
			names = append(names, event.Subagent)
		}
	}
	message := completionMessage(due, self.children.manager.ScratchNote, self.children.manager.Alias)
	return subagentrecord.DeliveryEvent(names, message, completionReports(due))
}

func completionMessage(due []agent.Event, scratchNote func(string) string, aliasOf func(string) string) string {
	var sections, finishedTitles, results []string
	for _, event := range due {
		title := subagentrecord.Title(event.Subagent, aliasOf(event.Subagent))
		if event.Kind == subagentrecord.Received {
			sections = append(sections, subagentrecord.MessageHeading(title)+"\n\n"+event.Text)
			continue
		}
		finishedTitles = append(finishedTitles, title)
		results = append(results, describeCompletion(
			event.Subagent, event.Name+scratchNote(event.Subagent), event.Text, subagentrecord.FailureOf(event),
		))
	}
	if len(finishedTitles) > 0 {
		sections = append(sections, subagentrecord.CompletionHeading(finishedTitles)+"\n\n"+strings.Join(results, "\n\n"))
	}
	return strings.Join(sections, "\n\n")
}

func completionReports(due []agent.Event) []subagentrecord.Report {
	reports := make([]subagentrecord.Report, len(due))
	for index, event := range due {
		if event.Kind == subagentrecord.Received {
			reports[index] = subagentrecord.Report{Name: event.Subagent, Message: event.Text}
			continue
		}
		reports[index] = subagentrecord.Report{
			Name:    event.Subagent,
			State:   subagentrecord.State(event.Name),
			Answer:  event.Text,
			Failure: subagentrecord.FailureOf(event),
		}
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
	if len(self.children.reportsDue) > 0 {
		self.pendingNotices.add(self.childDelivery(self.children.reportsDue))
		self.children.reportsDue = nil
	}
}

func (self *App) queueUndeliveredReports(events []agent.Event) {
	var pendingReports []agent.Event
	for _, event := range events {
		if isReported(event) {
			pendingReports = append(pendingReports, event)
		}
		if event.Kind == subagentrecord.ReportsDelivered {
			pendingReports = withoutDelivered(pendingReports, event)
		}
	}
	self.children.reportsDue = append(self.children.reportsDue, pendingReports...)
}

func withoutDelivered(pendingReports []agent.Event, delivery agent.Event) []agent.Event {
	names := strings.Split(delivery.Name, ",")
	pendingReports = slices.DeleteFunc(pendingReports, func(event agent.Event) bool {
		return event.Kind == subagentrecord.Finished && slices.Contains(names, event.Subagent)
	})
	reports, _ := subagentrecord.ReportsOf(delivery)
	for _, report := range reports {
		if !report.IsMessage() {
			continue
		}
		index := slices.IndexFunc(pendingReports, func(event agent.Event) bool {
			return event.Kind == subagentrecord.Received && event.Subagent == report.Name
		})
		if index >= 0 {
			pendingReports = slices.Delete(pendingReports, index, index+1)
		}
	}
	return pendingReports
}

func (self childState) reconfigure(live config.LiveConfig) error {
	if self.manager == nil || self.rotate == nil {
		return nil
	}
	rotation, err := self.rotate(live.Subagent, live.SelectionDefaults)
	if err != nil {
		return err
	}
	self.manager.Reconfigure(rotation)
	return nil
}

func (self childState) reachOf(setting string) config.Reach {
	if strings.HasPrefix(setting, subagentTable) && self.manager == nil {
		return config.ReachNextRun
	}
	return config.ReachOf(setting)
}

const subagentTable = "subagent."
