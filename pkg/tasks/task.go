package tasks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"crdx.org/oh/internal/stop"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/internal/waiting"
	"crdx.org/oh/pkg/tool"
)

const (
	DefaultMaximumRunning = 16
	MaxQueued             = 64
	ForegroundLimit       = 30 * time.Second
	ActivityFlash         = time.Second
	QuickRunLimit         = 2 * time.Second
	QuickRunsTolerated    = 3
	PreviousRunLimit      = 8
	activityPause         = time.Second
)

type State string

const (
	Queued   State = "queued"
	Running  State = "running"
	Stopping State = "stopping"
	Done     State = "done"
	Failed   State = "failed"
	TimedOut State = "timed_out"
	Stopped  State = "stopped"
)

func (self State) Spoken() string {
	return strings.ReplaceAll(string(self), "_", " ")
}

var ErrClosed = errors.New("the task supervisor is closed")

var errSessionClosed = stop.BecauseOf("the session closed", ErrClosed)

type RunOutcome struct {
	Run     int    `json:"run"`
	State   State  `json:"state"`
	Output  string `json:"output"`
	Failure string `json:"failure"`
}

type Invocation struct {
	Arguments    string `json:"arguments"`
	ForwardPort  uint16 `json:"forward_port,omitempty"`
	IsRespawning bool   `json:"respawn,omitempty"`
}

type Snapshot struct {
	Invocation            *Invocation  `json:"invocation,omitempty"`
	Previous              []RunOutcome `json:"previous,omitempty"`
	ID                    string       `json:"id"`
	ToolName              string       `json:"tool_name"`
	Run                   int          `json:"run"`
	IsRespawning          bool         `json:"is_respawning"`
	IsRespawnLimitReached bool         `json:"respawn_limit_reached,omitempty"`
	State                 State        `json:"state"`
	StartedAt             time.Time    `json:"started_at"`
	EndedAt               time.Time    `json:"ended_at"`
	ActivityAt            time.Time    `json:"-"`
	Output                string       `json:"output"`
	Failure               string       `json:"failure"`
}

func RespawnLimitReason() string {
	return fmt.Sprintf("respawning stopped after %d runs in a row each ended within %s", QuickRunsTolerated, QuickRunLimit)
}

type Conclusion struct {
	Snapshot       Snapshot
	Result         tool.ToolCallResult
	Failure        error
	IsFinal        bool
	IsSilent       bool
	IsSessionEnded bool
}

type Execution func(context.Context) (tool.ToolCallResult, error)

type PolicySource func() (any, error)

type executionContextSource interface {
	ForExecution(ctx context.Context) context.Context
}

type entry struct {
	snapshot          Snapshot
	result            tool.ToolCallResult
	failure           error
	over              chan struct{}
	startSignal       chan struct{}
	cancel            context.CancelCauseFunc
	isBackground      bool
	isRespawning      bool
	isStopped         bool
	quickRuns         int
	runClock          activeClock
	slotWait          *slotWait
	clock             activeClock
	users             int
	hasWaited         bool
	policy            any
	output            *tool.OutputBuffer
	publish           Publication
	publication       string
	publicationSignal chan struct{}
	isRestarting      bool
	isDiscarding      bool
	restartCancel     context.CancelCauseFunc
	restartDone       <-chan struct{}
	runLimit          time.Duration
}

type Manager struct {
	mutex           sync.Mutex
	entries         map[string]*entry
	nextID          int
	order           []string
	conclusions     []Conclusion
	changes         chan struct{}
	activity        chan struct{}
	slots           chan struct{}
	done            <-chan struct{}
	cancel          context.CancelFunc
	workers         sync.WaitGroup
	isClosed        bool
	preparations    map[string]Preparation
	now             func() time.Time
	foregroundLimit time.Duration
}

func New(ctx context.Context) *Manager {
	return NewWithMaximumRunning(ctx, DefaultMaximumRunning)
}

func NewWithMaximumRunning(ctx context.Context, maximumRunning int) *Manager {
	if maximumRunning < 1 {
		maximumRunning = DefaultMaximumRunning
	}
	sessionContext, cancel := context.WithCancel(ctx)
	return &Manager{
		entries:         make(map[string]*entry),
		changes:         make(chan struct{}, 1),
		activity:        make(chan struct{}, 1),
		slots:           make(chan struct{}, maximumRunning),
		done:            sessionContext.Done(),
		cancel:          cancel,
		now:             time.Now,
		foregroundLimit: ForegroundLimit,
	}
}

func (self *Manager) SetForegroundLimit(limit time.Duration) {
	if limit <= 0 {
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.foregroundLimit = limit
}

func (self *Manager) ForegroundLimit() time.Duration {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.foregroundLimit
}

func (self *Manager) Changes() <-chan struct{}  { return self.changes }
func (self *Manager) Activity() <-chan struct{} { return self.activity }

func (self *Manager) Start(
	ctx context.Context,
	id string,
	toolName string,
	isBackground bool,
	isRespawning bool,
	execute Execution,
	policy any,
	options ...StartOptions,
) (Snapshot, error) {
	if len(options) > 1 {
		return Snapshot{}, errors.New("only one task start option is permitted")
	}
	var settings StartOptions
	if len(options) == 1 {
		settings = options[0]
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.startLocked(ctx, id, toolName, isBackground, isRespawning, execute, policy, settings)
}

func (self *Manager) WaitOrDetach(ctx context.Context, id string) (Conclusion, bool, error) {
	self.mutex.Lock()
	current := self.entries[id]
	if current == nil {
		self.mutex.Unlock()
		return Conclusion{}, false, fmt.Errorf("there is no task named %s", id)
	}
	if current.isDiscarding {
		self.mutex.Unlock()
		return Conclusion{}, false, fmt.Errorf("task %s is being discarded", id)
	}
	current.users++
	current.hasWaited = true
	defer self.release(current)
	startSignal := current.startSignal
	over := current.over
	isBackground := current.isBackground
	foregroundLimit := self.foregroundLimit
	self.mutex.Unlock()
	if err := ctx.Err(); err != nil {
		if isBackground {
			_ = self.Stop(id, stopReason(ctx))
			return Conclusion{}, false, err
		}
		return self.cancelOrResult(id, err)
	}
	if isBackground {
		return Conclusion{}, true, nil
	}
	queueTimer := time.NewTimer(foregroundLimit)
	defer queueTimer.Stop()
	select {
	case <-startSignal:
	case <-over:
		return self.result(id)
	case <-ctx.Done():
		return self.cancelOrResult(id, ctx.Err())
	case <-queueTimer.C:
		self.mutex.Lock()
		if current.snapshot.State == Queued {
			current.isBackground = true
			self.signalChange()
			self.mutex.Unlock()
			return Conclusion{}, true, nil
		}
		self.mutex.Unlock()
		select {
		case <-startSignal:
		case <-over:
			return self.result(id)
		case <-ctx.Done():
			return self.cancelOrResult(id, ctx.Err())
		}
	}
	self.mutex.Lock()
	clock := current.clock.withoutSlotWait()
	self.mutex.Unlock()
	if !activeTimeExpired(ctx, clock, foregroundLimit, over) {
		if err := ctx.Err(); err != nil {
			return self.cancelOrResult(id, err)
		}
		return self.result(id)
	}
	self.mutex.Lock()
	if current.snapshot.State == Done || current.snapshot.State == Failed || current.snapshot.State == TimedOut || current.snapshot.State == Stopped {
		self.mutex.Unlock()
		return self.result(id)
	}
	current.isBackground = true
	self.signalChange()
	self.mutex.Unlock()
	return Conclusion{}, true, nil
}

func (self *Manager) Drain() []Conclusion {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	conclusions := slices.Clone(self.conclusions)
	self.conclusions = nil
	return conclusions
}

func (self *Manager) Status(id string) (Snapshot, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	current := self.entries[id]
	if current == nil {
		return Snapshot{}, fmt.Errorf("there is no task named %s", id)
	}
	return cloneSnapshot(current.snapshot), nil
}

func (self *Manager) List() []Snapshot {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	snapshots := make([]Snapshot, 0, len(self.order))
	for _, id := range self.order {
		if current := self.entries[id]; current.isBackground {
			snapshots = append(snapshots, cloneSnapshot(current.snapshot))
		}
	}
	return snapshots
}

func (self *Manager) Output(id string) (Snapshot, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	current := self.entries[id]
	if current == nil {
		return Snapshot{}, fmt.Errorf("there is no task named %s", id)
	}
	return outputSnapshot(current), nil
}

func (self *Manager) Checkpoint() []Snapshot {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	snapshots := make([]Snapshot, 0, len(self.order))
	for _, id := range self.order {
		if current := self.entries[id]; current.isBackground {
			snapshots = append(snapshots, outputSnapshot(current))
		}
	}
	return snapshots
}

func (self *Manager) PruneFinished() []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	var names []string
	self.order = slices.DeleteFunc(self.order, func(id string) bool {
		current := self.entries[id]
		if current.users > 0 || current.snapshot.State == Queued || current.snapshot.State == Running || current.snapshot.State == Stopping {
			return false
		}
		delete(self.entries, id)
		names = append(names, id)
		return true
	})
	if len(names) > 0 {
		self.signalChange()
	}
	return names
}

func (self *Manager) Hold(id string) (<-chan struct{}, func(), error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	current := self.entries[id]
	if current == nil {
		return nil, nil, fmt.Errorf("there is no task named %s", id)
	}
	if current.isDiscarding {
		return nil, nil, fmt.Errorf("task %s is being discarded", id)
	}
	current.users++
	return current.over, func() { self.release(current) }, nil
}

func (self *Manager) Stop(id string, reason string) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	current := self.entries[id]
	if current == nil {
		return fmt.Errorf("there is no task named %s", id)
	}
	self.stopLocked(current, stop.Because(reason))
	return nil
}

func (self *Manager) StopWhere(holds func(id string, policy any) bool, reason string) []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	names := make([]string, 0)
	for id, current := range self.entries {
		if current.snapshot.State != Running && current.snapshot.State != Queued && !current.isRestarting {
			continue
		}
		if holds(id, current.policy) && self.stopLocked(current, stop.Because(reason)) {
			names = append(names, id)
		}
	}
	slices.Sort(names)
	return names
}

func (self *Manager) Close() {
	self.mutex.Lock()
	self.isClosed = true
	self.cancel()
	for _, current := range self.entries {
		self.stopLocked(current, errSessionClosed)
	}
	self.mutex.Unlock()
	self.workers.Wait()
}

func (self *Manager) Restore(snapshots []Snapshot) []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	var taskNames []string
	for _, snapshot := range snapshots {
		if snapshot.ID == "" || self.entries[snapshot.ID] != nil {
			continue
		}
		if snapshot.State == Queued || snapshot.State == Running || snapshot.State == Stopping {
			taskNames = append(taskNames, snapshot.ID)
			snapshot.State = Stopped
			snapshot.IsRespawning = false
			snapshot.Failure = "the task did not carry over into this session"
		}
		over := make(chan struct{})
		close(over)
		current := &entry{snapshot: cloneSnapshot(snapshot), over: over, startSignal: over, isBackground: true}
		self.entries[snapshot.ID] = current
		self.order = append(self.order, snapshot.ID)
		self.reserveLocked(snapshot.ID)
	}
	return taskNames
}

func (self *Manager) Reserve(ids []string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for _, id := range ids {
		self.reserveLocked(id)
	}
}

func (self *Manager) reserveLocked(id string) {
	var index int
	if _, err := fmt.Sscanf(id, "task-%d", &index); err == nil && index > self.nextID {
		self.nextID = index
	}
}

func (self *Manager) startLocked(
	ctx context.Context,
	id string,
	toolName string,
	isBackground bool,
	isRespawning bool,
	execute Execution,
	policy any,
	options StartOptions,
) (Snapshot, error) {
	if isRespawning && !isBackground {
		return Snapshot{}, errors.New("respawn requires a background task")
	}
	if toolName == "" || execute == nil {
		return Snapshot{}, errors.New("tool name and execution are required")
	}
	if self.isClosed {
		return Snapshot{}, ErrClosed
	}
	select {
	case <-self.done:
		return Snapshot{}, ErrClosed
	default:
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if options.Name != "" {
		if err := ValidateName(options.Name); err != nil {
			return Snapshot{}, err
		}
		var err error
		id, err = self.availableName(options.Name)
		if err != nil {
			return Snapshot{}, err
		}
	} else if id == "" {
		id = self.allocateID()
	}
	if found := self.entries[id]; found != nil && found != options.replaces {
		return Snapshot{}, fmt.Errorf("task %s already exists", id)
	}
	if options.replaces != nil && self.entries[id] != options.replaces {
		return Snapshot{}, errors.New("the task changed while its restart was prepared")
	}
	if queueCount := self.queueSize(options.replaces); queueCount >= MaxQueued {
		return Snapshot{}, fmt.Errorf("%d tasks are queued already", MaxQueued)
	}
	if source, isSource := policy.(PolicySource); isSource {
		var err error
		policy, err = source()
		if err != nil {
			return Snapshot{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	callContext, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	current := &entry{
		snapshot:          Snapshot{ID: id, ToolName: toolName, State: Queued, Run: 1, IsRespawning: isRespawning, Invocation: cloneInvocation(options.Invocation)},
		over:              make(chan struct{}),
		startSignal:       make(chan struct{}),
		cancel:            cancel,
		isBackground:      isBackground,
		isRespawning:      isRespawning,
		policy:            policy,
		slotWait:          &slotWait{},
		publish:           options.Publish,
		publicationSignal: make(chan struct{}),
		runLimit:          options.RunLimit,
	}
	current.output = self.outputFor(current)
	self.entries[id] = current
	if options.replaces == nil {
		self.order = append(self.order, id)
	} else {
		current.snapshot.Run = options.replaces.snapshot.Run + 1
		current.snapshot.Previous = previousRuns(options.replaces.snapshot)
	}
	self.workers.Add(1)
	go self.run(callContext, current, execute)
	if isBackground {
		self.signalChange()
	}
	return cloneSnapshot(current.snapshot), nil
}

func (self *Manager) signalChange() {
	select {
	case self.changes <- struct{}{}:
	default:
	}
}

func (self *Manager) outputFor(current *entry) *tool.OutputBuffer {
	return tool.NewOutputBuffer(func() { self.markActivity(current) })
}

func (self *Manager) markActivity(current *entry) {
	self.mutex.Lock()
	now := self.now()
	if now.Before(current.snapshot.ActivityAt.Add(ActivityFlash + activityPause)) {
		self.mutex.Unlock()
		return
	}
	current.snapshot.ActivityAt = now
	isBackground := current.isBackground
	self.mutex.Unlock()
	if !isBackground {
		return
	}
	select {
	case self.activity <- struct{}{}:
	default:
	}
}

func (self *Manager) run(ctx context.Context, current *entry, execute Execution) {
	defer self.workers.Done()
	select {
	case self.slots <- struct{}{}:
		defer func() { <-self.slots }()
	case <-ctx.Done():
		self.finish(current, context.Cause(ctx))
		return
	case <-self.done:
		cause := context.Cause(ctx)
		if cause == nil {
			cause = errSessionClosed
		}
		self.finish(current, cause)
		return
	}
	self.mutex.Lock()
	select {
	case <-self.done:
		current.cancel(errSessionClosed)
	default:
	}
	if current.snapshot.State == Queued && ctx.Err() == nil && !self.isClosed {
		current.snapshot.State = Running
		current.clock = newRunClock(ctx, current.slotWait)
		current.snapshot.StartedAt = self.now()
		current.runClock = current.clock
		if current.isBackground {
			self.signalChange()
		}
	}
	isRunning := current.snapshot.State == Running
	self.mutex.Unlock()
	if isRunning {
		close(current.startSignal)
	}
	if !isRunning {
		self.finish(current, context.Cause(ctx))
		return
	}
	runContext, cancel := context.WithCancelCause(ctx)
	runFinished := make(chan struct{})
	if current.runLimit > 0 {
		go func() {
			if activeTimeExpired(runContext, current.clock, current.runLimit, runFinished) {
				cancel(stop.BecauseOf("the task reached its "+util.CompactDuration(current.runLimit)+" time limit", context.DeadlineExceeded))
			}
		}()
	}
	stopSession := make(chan struct{})
	go func() {
		select {
		case <-self.done:
			cancel(errSessionClosed)
		case <-stopSession:
		}
	}()
	defer close(stopSession)
	defer close(runFinished)
	defer cancel(nil)
	for {
		if runContext.Err() != nil {
			self.finish(current, context.Cause(runContext))
			return
		}
		executionContext := tool.WithOutput(markManaged(runContext), current.output)
		executionContext = tool.WithQueue(executionContext, func() func() {
			return self.awaitSlot(runContext, current)
		})
		if current.publish != nil {
			executionContext = tool.WithLaunch(executionContext, func(ctx context.Context) error {
				return self.publish(ctx, current)
			})
		}
		if source, isSource := current.policy.(executionContextSource); isSource {
			executionContext = source.ForExecution(executionContext)
		}
		result, err := execute(executionContext)
		if cause := context.Cause(runContext); errors.Is(cause, context.DeadlineExceeded) && ctx.Err() == nil {
			err = cause
		}
		if !self.finishRun(executionContext, current, result, err, runContext.Err() == nil) {
			return
		}
	}
}

func (self *Manager) awaitSlot(ctx context.Context, current *entry) func() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if current.snapshot.State != Running {
		return func() {}
	}
	current.snapshot.State = Queued
	current.slotWait.begin()
	if current.isBackground {
		self.signalChange()
	}
	return func() {
		self.mutex.Lock()
		defer self.mutex.Unlock()
		current.slotWait.end()
		if current.snapshot.State == Queued && ctx.Err() == nil && !self.isClosed {
			current.snapshot.State = Running
			if current.isBackground {
				self.signalChange()
			}
		}
	}
}

func (self *Manager) finishRun(ctx context.Context, current *entry, result tool.ToolCallResult, err error, isRunning bool) bool {
	self.mutex.Lock()
	if result.Output == "" && current.output != nil {
		result.Output = current.output.Report()
	}
	current.result = result
	current.failure = err
	current.snapshot.EndedAt = self.now()
	current.snapshot.Output = result.Output
	current.snapshot.Failure = ""
	if err != nil {
		current.snapshot.Failure = err.Error()
		current.snapshot.State = Failed
		if errors.Is(err, context.DeadlineExceeded) {
			current.snapshot.State = TimedOut
		}
	} else {
		current.snapshot.State = Done
	}
	if current.isStopped || self.isClosed && err != nil {
		current.snapshot.State = Stopped
	}
	if current.runClock.elapsed(waiting.Observe(ctx)) < QuickRunLimit {
		current.quickRuns++
	} else {
		current.quickRuns = 0
	}
	isRespawnWanted := current.isRespawning && !current.isStopped && !self.isClosed && isRunning
	isRespawning := isRespawnWanted && current.quickRuns < QuickRunsTolerated
	isRespawnLimitReached := isRespawnWanted && !isRespawning
	if !isRespawning {
		current.snapshot.IsRespawning = false
	}
	current.snapshot.IsRespawnLimitReached = isRespawnLimitReached
	conclusion := Conclusion{
		Snapshot: cloneSnapshot(current.snapshot), Result: result, Failure: err,
		IsFinal: !isRespawning, IsSilent: current.isStopped, IsSessionEnded: errors.Is(context.Cause(ctx), ErrClosed),
	}
	if current.isBackground {
		self.conclusions = append(self.conclusions, conclusion)
	}
	if isRespawning {
		current.snapshot.Previous = append(slices.Clone(current.snapshot.Previous), RunOutcome{
			Run: current.snapshot.Run, State: current.snapshot.State,
			Output: current.snapshot.Output, Failure: current.snapshot.Failure,
		})
		if len(current.snapshot.Previous) > PreviousRunLimit {
			current.snapshot.Previous = current.snapshot.Previous[len(current.snapshot.Previous)-PreviousRunLimit:]
		}
		current.snapshot.Run++
		current.snapshot.State = Running
		current.snapshot.EndedAt = time.Time{}
		current.snapshot.Output = ""
		current.snapshot.Failure = ""
		current.snapshot.ActivityAt = time.Time{}
		current.runClock = newRunClock(ctx, current.slotWait)
		current.output = self.outputFor(current)
	} else {
		current.snapshot.IsRespawning = false
		self.forgetForeground(current)
	}
	self.mutex.Unlock()
	if !isRespawning {
		close(current.over)
	}
	self.signalChange()
	return isRespawning
}

func (self *Manager) finish(current *entry, err error) {
	result := tool.ToolCallResult{}
	self.mutex.Lock()
	current.result = result
	current.failure = err
	current.snapshot.EndedAt = self.now()
	current.snapshot.IsRespawning = false
	current.snapshot.Output = result.Output
	if err != nil {
		current.snapshot.Failure = err.Error()
		current.snapshot.State = Failed
		if errors.Is(err, context.DeadlineExceeded) {
			current.snapshot.State = TimedOut
		}
	} else {
		current.snapshot.State = Done
	}
	if current.isStopped || self.isClosed && err != nil {
		current.snapshot.State = Stopped
	}
	conclusion := Conclusion{Snapshot: cloneSnapshot(current.snapshot), Result: result, Failure: err, IsFinal: true, IsSilent: current.isStopped, IsSessionEnded: errors.Is(err, ErrClosed)}
	if current.isBackground {
		self.conclusions = append(self.conclusions, conclusion)
	}
	self.forgetForeground(current)
	self.mutex.Unlock()
	close(current.over)
	self.signalChange()
}

func (self *Manager) result(id string) (Conclusion, bool, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	current := self.entries[id]
	return Conclusion{Snapshot: cloneSnapshot(current.snapshot), Result: current.result, Failure: current.failure}, false, nil
}

func (self *Manager) cancelOrResult(id string, cause error) (Conclusion, bool, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	current := self.entries[id]
	if current.snapshot.State == Done || current.snapshot.State == Failed ||
		current.snapshot.State == TimedOut || current.snapshot.State == Stopped {
		return Conclusion{Snapshot: cloneSnapshot(current.snapshot), Result: current.result, Failure: current.failure}, false, nil
	}
	if current.snapshot.State == Queued || current.snapshot.State == Running {
		current.isStopped = true
		current.snapshot.State = Stopping
		current.cancel(cause)
	}
	return Conclusion{}, false, cause
}

func (self *Manager) release(current *entry) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	current.users--
	self.forgetForeground(current)
}

func (self *Manager) forgetForeground(current *entry) {
	if self.entries[current.snapshot.ID] != current || !current.hasWaited || current.isBackground || current.users != 0 ||
		current.snapshot.State == Queued || current.snapshot.State == Running || current.snapshot.State == Stopping {
		return
	}
	delete(self.entries, current.snapshot.ID)
	self.order = slices.DeleteFunc(self.order, func(id string) bool { return id == current.snapshot.ID })
}

func stopReason(ctx context.Context) string {
	if sentence := stop.Sentence(ctx); sentence != "" {
		return sentence
	}
	return "its request stopped"
}
