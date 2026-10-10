package subagents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/interrupt"
	"crdx.org/oh/internal/app/metrics"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/record"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/subagentrecord"
	"crdx.org/oh/internal/app/toolset"
	"crdx.org/oh/internal/req"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/subagent"
)

const (
	maxChildren  = 5
	maxChildLife = 30 * time.Minute
)

type Child struct {
	Name            string
	FrozenTools     []store.ToolDefinition
	History         []agent.Event
	Choice          model.Choice
	Selection       model.Selection
	Workspace       string
	Caps            caps.Set
	Directory       string
	Scratch         string
	ScratchRoot     *os.Root
	SharedPrompt    string
	SystemPrompt    string
	Observer        req.Observer
	EnsurePersisted func() error
}

type Worker struct {
	Agent        *agent.Agent
	Tools        []tool.Tool
	SystemPrompt string
	Changes      []agent.Event
	Close        func()
}

type Factory func(context.Context, Child) (Worker, error)

type Options struct {
	Directory    string
	Scratch      string
	Parent       string
	Choice       model.Choice
	Meta         store.Meta
	Factory      Factory
	PickName     func(int) int
	EnsureParent func() error
	Workspace    func(path string) (string, error)
	Caps         func() caps.Set
	ScratchNote  func(name string) string
}

type Snapshot struct {
	Name      string
	Task      string
	Intent    string
	Workspace string
	SessionID string
	State     subagentrecord.State
	StartedAt time.Time
	EndedAt   time.Time
	Answer    string
	Failure   string
}

type child struct {
	Snapshot

	cancel     context.CancelFunc
	over       chan struct{}
	isReturned bool
	stopReason string
	caps       caps.Set
	choice     model.Choice
	selection  model.Selection
	usage      agent.Usage
}

type Manager struct {
	launchMutex sync.Mutex
	mutex       sync.Mutex
	options     Options
	children    map[string]*child
	order       []string
	scratchRoot *os.Root
	events      chan agent.Event
	closed      bool
	workers     sync.WaitGroup
	closeOnce   sync.Once
}

func New(options Options) (*Manager, error) {
	parentRoot, err := os.OpenRoot(options.Scratch)
	if err != nil {
		return nil, err
	}
	defer func() { _ = parentRoot.Close() }()
	if err := parentRoot.Mkdir(ScratchName, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("create subagent scratch root: %w", err)
	}
	scratchRoot, err := parentRoot.OpenRoot(ScratchName)
	if err != nil {
		return nil, err
	}
	return &Manager{
		options:     options,
		children:    make(map[string]*child),
		scratchRoot: scratchRoot,
		events:      make(chan agent.Event, 256),
	}, nil
}

func (self *Manager) Events() <-chan agent.Event { return self.events }

func (self *Manager) Directory() string { return self.options.Directory }

func (self *Manager) Restore(events []agent.Event) []agent.Event {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for _, event := range events {
		if event.Subagent == "" {
			continue
		}
		if event.Kind == subagentrecord.Started {
			self.restoreStart(event)
			continue
		}
		if current := self.children[event.Subagent]; current != nil {
			current.restoreFact(event)
		}
	}
	finishes := Endings(self.options.Directory, events)
	for _, finish := range finishes {
		if current := self.children[finish.Subagent]; current != nil {
			current.State = subagentrecord.Ended
			current.Answer = finish.Text
			current.Failure = subagentrecord.FailureOf(finish)
		}
	}
	return finishes
}

func (self *child) restoreFact(event agent.Event) {
	if event.Kind == subagentrecord.Sent {
		self.State = subagentrecord.Running
		self.isReturned = false
	}
	if event.Kind == subagentrecord.Finished {
		self.State = subagentrecord.State(event.Name)
		self.Answer = event.Text
		self.Failure = subagentrecord.FailureOf(event)
	}
	if event.Kind == subagentrecord.Returned {
		self.isReturned = true
	}
}

var childLifeKinds = []agent.Kind{subagentrecord.Started, subagentrecord.Sent, subagentrecord.Finished}

func Endings(directory string, events []agent.Event) []agent.Event {
	isLive := map[string]bool{}
	var order []string
	for _, event := range events {
		if event.Kind == subagentrecord.Started && !slices.Contains(order, event.Subagent) {
			order = append(order, event.Subagent)
		}
		if slices.Contains(childLifeKinds, event.Kind) {
			isLive[event.Subagent] = event.Kind != subagentrecord.Finished
		}
	}
	var finishes []agent.Event
	for _, name := range order {
		if !isLive[name] {
			continue
		}
		answer, usage := unfinishedRun(directory, name)
		finishes = append(finishes, subagentrecord.FinishedEvent(
			name, subagentrecord.Ended, answer, subagentrecord.Failure(unfinishedReason), usage,
		))
	}
	return finishes
}

const unfinishedReason = "parent process ended before the subagent completed"

func unfinishedRun(directory string, name string) (string, *agent.Usage) {
	storedSession, err := store.Read(directory, name)
	if err != nil {
		return "", nil
	}
	start := 0
	for index, event := range storedSession.Events {
		if event.Kind == agent.UserMessageEvent {
			start = index
		}
	}
	answer := ""
	usage := agent.Usage{}
	for _, event := range storedSession.Events[start:] {
		if event.Kind == agent.ModelMessageEvent {
			answer = event.Text
		}
		if subagentrecord.IsCounted(event) {
			subagentrecord.AddUsage(&usage, *event.Usage)
		}
	}
	return answer, &usage
}

func closedOver() chan struct{} {
	over := make(chan struct{})
	close(over)
	return over
}

func (self *Manager) Start(ctx context.Context, sharedPrompt string, tasks []subagent.Task) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	self.launchMutex.Lock()
	defer self.launchMutex.Unlock()
	self.mutex.Lock()
	isClosed, liveCount, siblings := self.closed, self.liveCount(), slices.Clone(self.order)
	self.mutex.Unlock()
	if isClosed {
		return "", errors.New("subagent manager is closed")
	}
	if len(tasks) == 0 || liveCount+len(tasks) > maxChildren {
		return "", fmt.Errorf("at most %d subagents may run at once", maxChildren)
	}
	if self.options.EnsureParent != nil {
		if err := self.options.EnsureParent(); err != nil {
			return "", err
		}
	}
	type reservation struct {
		current *child
		writer  *store.Writer
		start   func()
	}
	var reservations []reservation
	discard := func() {
		for _, place := range reservations {
			_ = place.writer.Close()
		}
	}
	for _, task := range tasks {
		workspace, err := self.options.Workspace(task.Workspace)
		if err != nil {
			discard()
			return "", err
		}
		name, err := self.pickName(siblings)
		if err != nil {
			discard()
			return "", err
		}
		siblings = append(siblings, name)
		writer, err := store.CreateNamed(self.options.Directory, name, self.childMeta(workspace))
		if err != nil {
			discard()
			return "", err
		}
		current := &child{choice: self.options.Choice, selection: self.configuredSelection(), Snapshot: Snapshot{
			Name: name, Task: task.Prompt, Intent: task.Intent, Workspace: workspace, SessionID: writer.ID(), State: subagentrecord.Running, StartedAt: time.Now(),
		}}
		childContext := self.prepare(ctx, current)
		reservations = append(reservations, reservation{writer: writer, current: current, start: func() {
			self.launch(childContext, current, writer, nil, sharedPrompt, current.Task)
		}})
	}
	var names []string
	self.mutex.Lock()
	for _, place := range reservations {
		self.children[place.current.Name] = place.current
		self.order = append(self.order, place.current.Name)
		names = append(names, place.current.Name)
	}
	self.mutex.Unlock()
	for _, place := range reservations {
		current := place.current
		self.events <- subagentrecord.StartedEvent(current.Name, current.SessionID, current.Task, subagentrecord.Origin{
			Choice: current.choice, Workspace: current.Workspace, Intent: current.Intent,
		})
		place.start()
	}
	return "started " + strings.Join(names, ", "), nil
}

func (self *Manager) Send(ctx context.Context, name string, message string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	self.launchMutex.Lock()
	defer self.launchMutex.Unlock()
	self.mutex.Lock()
	current := self.children[name]
	err := self.refuseSend(name, current)
	sessionID := ""
	if current != nil {
		sessionID = current.SessionID
	}
	self.mutex.Unlock()
	if err != nil {
		return "", err
	}
	storedSession, err := store.Read(self.options.Directory, name)
	if errors.Is(err, session.ErrNotFound) {
		return "", fmt.Errorf("the conversation of %s is gone, so it cannot take a follow-up", name)
	}
	if err != nil {
		return "", err
	}
	if storedSession.ID != sessionID {
		return "", fmt.Errorf("the conversation stored as %s is not the one this session started, so it cannot take a follow-up", name)
	}
	if storedSession.Meta.ModelChoice == nil {
		return "", fmt.Errorf("the conversation of %s records no model, so it cannot take a follow-up", name)
	}
	writer, err := store.Open(self.options.Directory, name)
	if err != nil {
		return "", err
	}
	self.mutex.Lock()
	current.choice = *storedSession.Meta.ModelChoice
	current.selection = recordedSelection(storedSession.Meta)
	current.State = subagentrecord.Running
	current.Answer = ""
	current.Failure = ""
	current.isReturned = false
	current.stopReason = ""
	current.usage = agent.Usage{}
	current.StartedAt = time.Now()
	current.EndedAt = time.Time{}
	childContext := self.prepare(ctx, current)
	self.mutex.Unlock()
	self.events <- subagentrecord.SentEvent(name, message)
	self.launch(childContext, current, writer, storedSession, "", message)
	return "sent to " + name, nil
}

func (self *Manager) ListSnapshots() []Snapshot {
	snapshots, _, _ := self.snapshots(nil)
	return snapshots
}

func (self *Manager) ScratchNote(name string) string {
	if self == nil || self.options.ScratchNote == nil {
		return ""
	}
	return " (" + self.options.ScratchNote(name) + ")"
}

func (self *Manager) Model() string {
	if self.options.Choice.Name != "" {
		return self.options.Choice.Name
	}
	return self.options.Choice.ID
}

func (self *Manager) LiveSpend() (float64, bool) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	spend, isPriced := 0.0, true
	for _, current := range self.children {
		if !current.State.IsLive() || current.usage == (agent.Usage{}) {
			continue
		}
		prices := current.choice.Prices
		if prices == nil || !prices.IsKnown() {
			isPriced = false
			continue
		}
		spend += metrics.Spend(*prices, current.usage)
	}
	return spend, isPriced
}

func describe(snapshot Snapshot) string {
	result := snapshot.Name + ": " + string(snapshot.State)
	if snapshot.Failure != "" {
		result += " — " + snapshot.Failure
	}
	return result
}

func describeAll(snapshots []Snapshot) string {
	if len(snapshots) == 0 {
		return "no subagents"
	}
	lines := make([]string, len(snapshots))
	for index, snapshot := range snapshots {
		lines[index] = describe(snapshot)
	}
	return strings.Join(lines, "\n")
}

func (self *Manager) List() string { return describeAll(self.ListSnapshots()) }

func (self *Manager) Status(names []string) (string, error) {
	snapshots, _, err := self.snapshots(names)
	if err != nil {
		return "", err
	}
	return describeAll(snapshots), nil
}

func (self *Manager) Output(names []string) (string, error) {
	snapshots, _, err := self.snapshots(names)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, snapshot := range snapshots {
		lines = append(lines, describe(snapshot)+self.ScratchNote(snapshot.Name)+"\n"+snapshot.Answer)
	}
	self.announceReturned(snapshots)
	return strings.Join(lines, "\n"), nil
}

func (self *Manager) WasReturned(name string) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	current := self.children[name]
	return current != nil && current.isReturned
}

func (self *Manager) Wait(ctx context.Context, names []string, limit time.Duration) (string, error) {
	_, overs, err := self.snapshots(names)
	if err != nil {
		return "", err
	}
	deadline := time.NewTimer(limit)
	defer deadline.Stop()
	for _, over := range overs {
		select {
		case <-over:
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			return self.gaveUp(names, limit)
		}
	}
	return self.Output(names)
}

func pluralIs(count int) string {
	if count == 1 {
		return "is"
	}
	return "are"
}

func (self *Manager) Stop(names []string) (string, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if err := self.requireKnown(names); err != nil {
		return "", err
	}
	stoppedNames := self.stopLocked(names, "the parent stopped it")
	if len(stoppedNames) == 0 && len(names) == 0 {
		return "", errors.New("no subagent is running")
	}
	if len(stoppedNames) == 0 {
		return "", errors.New("none of the named subagents is running")
	}
	return "stopping " + strings.Join(stoppedNames, ", "), nil
}

func (self *Manager) StopRunning() []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	var holders []string
	for _, childName := range self.order {
		if self.children[childName].caps.Has(caps.Shell) {
			holders = append(holders, childName)
		}
	}
	if len(holders) == 0 {
		return nil
	}
	return self.stopLocked(holders, "shell execution was withdrawn")
}

func (self *Manager) Close() {
	self.closeOnce.Do(func() {
		self.launchMutex.Lock()
		self.mutex.Lock()
		self.closed = true
		for _, current := range self.children {
			if current.State.IsLive() {
				current.cancel()
			}
		}
		self.mutex.Unlock()
		self.launchMutex.Unlock()
		self.workers.Wait()
		close(self.events)
		_ = self.scratchRoot.Close()
	})
}

func (self *Manager) gaveUp(names []string, limit time.Duration) (string, error) {
	reports, err := self.Output(names)
	if err != nil {
		return "", err
	}
	var stillLive []string
	for _, snapshot := range self.ListSnapshots() {
		if (len(names) == 0 || slices.Contains(names, snapshot.Name)) && snapshot.State.IsLive() {
			stillLive = append(stillLive, snapshot.Name)
		}
	}
	if len(stillLive) == 0 {
		return reports, nil
	}
	return strings.TrimRight(reports, "\n") + fmt.Sprintf(
		"\n\nnote: the wait gave up after %s, and %s %s still running.",
		util.CompactDuration(limit), strings.Join(stillLive, ", "), pluralIs(len(stillLive)),
	), nil
}

func (self *Manager) restoreStart(event agent.Event) {
	if self.children[event.Subagent] != nil {
		return
	}
	origin, _ := subagentrecord.DecodeOrigin(event)
	self.children[event.Subagent] = &child{Snapshot: Snapshot{
		Name: event.Subagent, Task: event.Text, Intent: origin.Intent, Workspace: origin.Workspace, SessionID: event.ID, State: subagentrecord.Running,
	}, over: closedOver()}
	self.order = append(self.order, event.Subagent)
}

func (self *Manager) childMeta(workspace string) store.Meta {
	meta := self.options.Meta
	meta.WorkspaceDir = workspace
	return meta
}

func (self *Manager) liveCount() int {
	count := 0
	for _, current := range self.children {
		if current.State.IsLive() {
			count++
		}
	}
	return count
}

func (self *Manager) stopLocked(names []string, reason string) []string {
	var stoppedNames []string
	for _, childName := range self.order {
		current := self.children[childName]
		if len(names) > 0 && !slices.Contains(names, current.Name) {
			continue
		}
		if current.State == subagentrecord.Running {
			current.State = subagentrecord.Stopping
			current.stopReason = reason
			current.cancel()
			stoppedNames = append(stoppedNames, current.Name)
		}
	}
	return stoppedNames
}

func (self *Manager) run(ctx context.Context, current *child, writer *store.Writer, storedSession *store.Session, sharedPrompt string, message string) {
	defer self.workers.Done()
	defer current.cancel()
	answer, err := self.converse(ctx, current, writer, storedSession, sharedPrompt, message)
	state, failure := self.outcome(ctx, current, err)
	self.recordOutcome(writer, current, state, failure)
	_ = writer.Close()

	self.mutex.Lock()
	usage := current.usage
	self.mutex.Unlock()
	self.events <- subagentrecord.FinishedEvent(current.Name, state, answer, failure, &usage)

	self.mutex.Lock()
	current.State = state
	current.Answer = answer
	current.Failure = subagentrecord.FailureOf(agent.Event{Failure: failure})
	current.EndedAt = time.Now()
	close(current.over)
	self.mutex.Unlock()
}

func (self *Manager) converse(ctx context.Context, current *child, writer *store.Writer, storedSession *store.Session, sharedPrompt string, message string) (string, error) {
	scratchRoot, err := self.childScratch(current.Name)
	if err != nil {
		return "", err
	}
	defer func() { _ = scratchRoot.Close() }()
	systemPrompt := ""
	var frozenTools []store.ToolDefinition
	var history []agent.Event
	if storedSession != nil {
		systemPrompt = storedSession.Meta.SystemPrompt
		frozenTools = storedSession.Meta.ToolDefinitions
		history = storedSession.Events
	}
	worker, err := self.options.Factory(ctx, Child{
		Name:            current.Name,
		FrozenTools:     frozenTools,
		History:         history,
		Choice:          current.choice,
		Selection:       current.selection,
		Workspace:       current.Workspace,
		Caps:            current.caps,
		Directory:       session.Dir(self.options.Directory, current.Name),
		Scratch:         filepath.Join(self.options.Scratch, ScratchName, current.Name),
		ScratchRoot:     scratchRoot,
		SharedPrompt:    sharedPrompt,
		SystemPrompt:    systemPrompt,
		Observer:        writer.Observer(),
		EnsurePersisted: writer.EnsurePersisted,
	})
	if err != nil {
		return "", err
	}
	defer worker.Close()

	recorder := record.New(writer)
	prepare := self.begin
	if storedSession != nil {
		prepare = resume
	}
	if err := prepare(worker, writer, recorder, current, storedSession); err != nil {
		return "", err
	}

	startedAt := time.Now()
	answer := ""
	var streamError error
	for update, failure := range worker.Agent.Stream(ctx, message, &agent.Interjections{}) {
		if failure != nil {
			streamError = failure
			break
		}
		if update.Event == nil {
			continue
		}
		event := *update.Event
		if event.Kind == agent.ModelMessageEvent {
			answer = event.Text
		}
		if subagentrecord.IsCounted(event) {
			self.mutex.Lock()
			subagentrecord.AddUsage(&current.usage, *event.Usage)
			self.mutex.Unlock()
		}
		if err := recorder.Event(event); err != nil {
			return answer, err
		}
	}
	if ctx.Err() != nil {
		worker.Agent.AddNotes([]agent.Note{{Kind: agent.InterruptionNote, Text: self.interruptionNote(ctx, current)}})
	}
	if items, err := worker.Agent.Dump(); err == nil {
		if err := recorder.StoreItems(items); err == nil {
			_ = recorder.CompleteTurn(session.TurnSummary{Took: time.Since(startedAt)})
		}
	}
	return answer, streamError
}

func (self *Manager) childScratch(name string) (*os.Root, error) {
	if err := self.scratchRoot.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	return self.scratchRoot.OpenRoot(name)
}

func (self *Manager) outcome(ctx context.Context, current *child, err error) (subagentrecord.State, *agent.Failure) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return subagentrecord.Failed, subagentrecord.Failure("ran longer than " + maxChildLife.String())
	case ctx.Err() != nil && current.State == subagentrecord.Stopping:
		return subagentrecord.Stopped, nil
	case ctx.Err() != nil && self.closed:
		return subagentrecord.Ended, subagentrecord.Failure("parent session closed")
	case ctx.Err() != nil:
		return subagentrecord.Stopped, nil
	case err != nil:
		return subagentrecord.Failed, agent.FailureFrom(err)
	}
	return subagentrecord.Done, nil
}

func (self *Manager) recordOutcome(writer *store.Writer, current *child, state subagentrecord.State, failure *agent.Failure) {
	switch state {
	case subagentrecord.Failed:
		_ = writer.Event(agent.Event{Kind: agent.FailureEvent, Failure: failure})
	case subagentrecord.Stopped:
		self.mutex.Lock()
		reason := current.stopReason
		self.mutex.Unlock()
		_ = writer.Event(agent.Event{Kind: agent.InterruptionEvent, Text: reason})
	case subagentrecord.Ended:
		_ = writer.Event(interrupt.Event(interrupt.SessionClose))
	case subagentrecord.Running, subagentrecord.Stopping, subagentrecord.Done:
	}
}

func (self *Manager) requireKnown(names []string) error {
	for _, name := range names {
		if self.children[name] == nil {
			return fmt.Errorf("no subagent named %s", name)
		}
	}
	return nil
}

func (self *Manager) snapshots(names []string) ([]Snapshot, []<-chan struct{}, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if err := self.requireKnown(names); err != nil {
		return nil, nil, err
	}
	var found []Snapshot
	var overs []<-chan struct{}
	for _, childName := range self.order {
		current := self.children[childName]
		if len(names) == 0 || slices.Contains(names, current.Name) {
			found = append(found, current.Snapshot)
			overs = append(overs, current.over)
		}
	}
	return found, overs, nil
}

func (self *Manager) announceReturned(snapshots []Snapshot) {
	var returnedNames []string
	self.mutex.Lock()
	for _, snapshot := range snapshots {
		current := self.children[snapshot.Name]
		if current != nil && !snapshot.State.IsLive() && !current.isReturned {
			current.isReturned = true
			returnedNames = append(returnedNames, snapshot.Name)
		}
	}
	if self.closed || len(returnedNames) == 0 {
		self.mutex.Unlock()
		return
	}
	self.workers.Add(1)
	self.mutex.Unlock()
	defer self.workers.Done()
	for _, name := range returnedNames {
		self.events <- subagentrecord.ReturnedEvent(name)
	}
}

func tellModeChange(worker *agent.Agent, recorder *record.Recorder, events []agent.Event, currentCaps caps.Set) error {
	lastCaps, isRecorded := caps.LastRecordedMode(events)
	if isRecorded && lastCaps == currentCaps {
		return nil
	}
	event := caps.ModeToggleEvent(lastCaps^currentCaps, currentCaps)
	if !isRecorded {
		event = caps.ModeEvent(currentCaps)
	}
	if notices, isSaid := caps.ModeNotice(event); isSaid {
		worker.AddNotes([]agent.Note{{Kind: agent.EnvironmentNote, Text: strings.Join(notices, "\n")}})
	}
	return recorder.Event(event)
}

func (self *Manager) begin(worker Worker, writer *store.Writer, recorder *record.Recorder, current *child, _ *store.Session) error {
	meta := self.childMeta(current.Workspace)
	meta.SystemPrompt = worker.SystemPrompt
	meta.ToolDefinitions = store.FreezeTools(worker.Tools)
	for _, offeredTool := range worker.Tools {
		meta.Tools = append(meta.Tools, offeredTool.Name())
	}
	if err := writer.SetMeta(meta); err != nil {
		return err
	}
	return recorder.Event(caps.ModeEvent(current.caps))
}

func resume(worker Worker, _ *store.Writer, recorder *record.Recorder, current *child, storedSession *store.Session) error {
	if err := worker.Agent.RestoreState(storedSession.Events); err != nil {
		return err
	}
	if err := worker.Agent.Load(storedSession.Items); err != nil {
		return err
	}
	worker.Agent.RestoreCache(storedSession.CacheReading)
	recorder.Resume(len(storedSession.Items))
	for _, change := range worker.Changes {
		if notices, isSaid := toolset.AvailabilityNotice(change); isSaid {
			worker.Agent.AddNotes([]agent.Note{{Kind: agent.EnvironmentNote, Text: strings.Join(notices, "\n")}})
		}
		if err := recorder.Event(change); err != nil {
			return err
		}
	}
	return tellModeChange(worker.Agent, recorder, storedSession.Events, current.caps)
}

func (self *Manager) refuseSend(name string, current *child) error {
	switch {
	case self.closed:
		return errors.New("subagent manager is closed")
	case current == nil:
		return fmt.Errorf("no subagent named %s", name)
	case current.State.IsLive():
		return fmt.Errorf("%s is still running; wait for it before sending it anything", name)
	case self.liveCount()+1 > maxChildren:
		return fmt.Errorf("at most %d subagents may run at once", maxChildren)
	}
	return nil
}

func (self *Manager) prepare(ctx context.Context, current *child) context.Context {
	childContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), maxChildLife)
	current.cancel = cancel
	current.over = make(chan struct{})
	current.caps = self.options.Caps()
	return childContext
}

func (self *Manager) launch(
	childContext context.Context,
	current *child,
	writer *store.Writer,
	storedSession *store.Session,
	sharedPrompt string,
	message string,
) {
	self.workers.Add(1)
	go self.run(childContext, current, writer, storedSession, sharedPrompt, message)
}

func (self *Manager) pickName(siblings []string) (string, error) {
	siblings = slices.Clone(siblings)
	if storedNames, err := session.AllNames(self.options.Directory); err == nil {
		siblings = append(siblings, storedNames...)
	}
	return session.ChildName(self.options.Parent, siblings, self.options.PickName)
}

func (self *Manager) interruptionNote(ctx context.Context, current *child) string {
	state, _ := self.outcome(ctx, current, nil)
	self.mutex.Lock()
	reason := current.stopReason
	self.mutex.Unlock()
	switch state {
	case subagentrecord.Ended:
		reason = "the parent session closed"
	case subagentrecord.Failed:
		reason = "it ran longer than " + util.CompactDuration(maxChildLife)
	case subagentrecord.Running, subagentrecord.Stopping, subagentrecord.Stopped, subagentrecord.Done:
	}
	if reason == "" {
		return "Turn stopped."
	}
	return "Turn stopped because " + reason + "."
}

func (self *Manager) configuredSelection() model.Selection {
	return recordedSelection(self.options.Meta)
}

func recordedSelection(meta store.Meta) model.Selection {
	return model.Selection{Provider: meta.Provider, Model: meta.Model, Effort: meta.Effort, IsFast: meta.IsFast}
}
