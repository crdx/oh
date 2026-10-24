package tasks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"crdx.org/oh/internal/stop"
)

const (
	NameLengthLimit = 10
	IDLengthLimit   = 32
)

type Publication func(ctx context.Context, id string, port uint16) (string, error)

type PreparationPlan struct {
	Execute  Execution
	Policy   any
	Publish  Publication
	RunLimit time.Duration
}

type Preparation func(context.Context, Invocation) (PreparationPlan, error)

type StartOptions struct {
	Name       string
	Invocation *Invocation
	Publish    Publication
	RunLimit   time.Duration
	replaces   *entry
}

func NamePattern() string {
	return pattern(NameLengthLimit)
}

func IDPattern() string {
	return pattern(IDLengthLimit)
}

func ValidateName(name string) error {
	return validate("task name", name, NameLengthLimit)
}

func ValidateID(id string) error {
	return validate("task id", id, IDLengthLimit)
}

func validate(noun string, text string, limit int) error {
	if len(text) == 0 || len(text) > limit {
		return fmt.Errorf("%s must match %s", noun, pattern(limit))
	}
	for _, character := range text {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return fmt.Errorf("%s must match %s", noun, pattern(limit))
		}
	}
	return nil
}

func pattern(limit int) string {
	return fmt.Sprintf("[a-z0-9-]{1,%d}", limit)
}

func (self *Manager) availableName(name string) (string, error) {
	if self.entries[name] == nil {
		return name, nil
	}
	for number := 1; ; number++ {
		candidate := fmt.Sprintf("%s-%d", name, number)
		if len(candidate) > IDLengthLimit {
			return "", errors.New("task name space exhausted")
		}
		if self.entries[candidate] == nil {
			return candidate, nil
		}
	}
}

func (self *Manager) BindTool(name string, prepare Preparation) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.preparations == nil {
		self.preparations = make(map[string]Preparation)
	}
	self.preparations[name] = prepare
}

func (self *Manager) Restart(ctx context.Context, id string) (Snapshot, error) {
	self.mutex.Lock()
	current := self.entries[id]
	if current == nil {
		self.mutex.Unlock()
		return Snapshot{}, fmt.Errorf("there is no task named %s", id)
	}
	if current.isRestarting || current.users != 0 {
		self.mutex.Unlock()
		return Snapshot{}, fmt.Errorf("task %s is still in use", id)
	}
	invocation := cloneInvocation(current.snapshot.Invocation)
	prepare := self.preparations[current.snapshot.ToolName]
	if invocation == nil || prepare == nil {
		self.mutex.Unlock()
		return Snapshot{}, fmt.Errorf("task %s has no compatible remembered invocation", id)
	}
	if self.queueSize(current) >= MaxQueued {
		self.mutex.Unlock()
		return Snapshot{}, fmt.Errorf("%d tasks are queued already", MaxQueued)
	}
	restartContext, cancelRestart := context.WithCancelCause(ctx)
	defer cancelRestart(nil)
	current.isRestarting = true
	current.restartCancel = cancelRestart
	current.restartDone = restartContext.Done()
	current.users++
	self.mutex.Unlock()

	plan, err := prepare(restartContext, *invocation)
	if err == nil {
		err = restartContext.Err()
	}
	if source, isSource := plan.Policy.(PolicySource); err == nil && isSource {
		plan.Policy, err = source()
	}
	if err == nil {
		self.mutex.Lock()
		if current.snapshot.State == Running || current.snapshot.State == Queued {
			current.isStopped = true
			current.snapshot.State = Stopping
			current.snapshot.IsRespawning = false
			current.cancel(stop.Because("the task restarted"))
			self.signalChange()
		}
		over := current.over
		self.mutex.Unlock()
		select {
		case <-over:
		case <-restartContext.Done():
			err = restartContext.Err()
		}
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	current.isRestarting = false
	current.restartCancel = nil
	current.restartDone = nil
	current.users--
	if restartContext.Err() != nil {
		return Snapshot{}, fmt.Errorf("task restart stopped (%w): %w", context.Cause(restartContext), restartContext.Err())
	}
	if err != nil {
		return Snapshot{}, err
	}
	invocation = cloneInvocation(current.snapshot.Invocation)
	return self.startLocked(restartContext, id, current.snapshot.ToolName, true, invocation.IsRespawning,
		plan.Execute, plan.Policy, StartOptions{
			Invocation: invocation,
			Publish:    plan.Publish,
			RunLimit:   plan.RunLimit,
			replaces:   current,
		})
}

func (self *Manager) Discard(ctx context.Context, id string) (Snapshot, error) {
	self.mutex.Lock()
	current := self.entries[id]
	if current == nil {
		self.mutex.Unlock()
		return Snapshot{}, fmt.Errorf("there is no task named %s", id)
	}
	if current.isRestarting || current.isDiscarding || current.users != 0 {
		self.mutex.Unlock()
		return Snapshot{}, fmt.Errorf("task %s is still in use", id)
	}
	current.isDiscarding = true
	current.users++
	if current.snapshot.State == Running || current.snapshot.State == Queued {
		current.isStopped = true
		current.snapshot.State = Stopping
		current.snapshot.IsRespawning = false
		current.cancel(stop.Because("the task was discarded"))
		self.signalChange()
	}
	over := current.over
	self.mutex.Unlock()
	select {
	case <-over:
	case <-ctx.Done():
		self.mutex.Lock()
		current.isDiscarding = false
		current.users--
		self.mutex.Unlock()
		return Snapshot{}, ctx.Err()
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	current.isDiscarding = false
	current.users--
	if current.users != 0 {
		return Snapshot{}, fmt.Errorf("task %s is still in use", id)
	}
	snapshot := cloneSnapshot(current.snapshot)
	delete(self.entries, id)
	self.order = slices.DeleteFunc(self.order, func(name string) bool { return name == id })
	for index := range self.conclusions {
		if self.conclusions[index].Snapshot.ID == id {
			self.conclusions[index].IsSilent = true
		}
	}
	self.signalChange()
	return snapshot, nil
}

func (self *Manager) publish(ctx context.Context, current *entry) error {
	self.mutex.Lock()
	port := forwardPort(current)
	id := current.snapshot.ID
	self.mutex.Unlock()
	if port == 0 {
		return nil
	}
	publication, err := current.publish(ctx, id, port)
	if err != nil {
		return fmt.Errorf("port %d could not be forwarded: %w", port, err)
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if forwardPort(current) == port {
		current.publication = publication
	}
	signalPublication(current)
	return nil
}

type PublicationState int

const (
	PublicationPending PublicationState = iota
	PublicationDone
	PublicationWithdrawn
	PublicationEnded
	PublicationStopped
)

func (self *Manager) AwaitPublication(ctx context.Context, id string, limit time.Duration) (string, PublicationState) {
	self.mutex.Lock()
	current := self.entries[id]
	if current == nil || current.publish == nil || forwardPort(current) == 0 {
		self.mutex.Unlock()
		return "", PublicationWithdrawn
	}
	publicationSignal := current.publicationSignal
	over := current.over
	self.mutex.Unlock()
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case <-publicationSignal:
	case <-over:
	case <-timer.C:
	case <-ctx.Done():
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	switch {
	case current.publication != "":
		return current.publication, PublicationDone
	case forwardPort(current) == 0 || self.entries[id] != current:
		return "", PublicationWithdrawn
	}
	select {
	case <-over:
		if current.isStopped {
			return "", PublicationStopped
		}
		return "", PublicationEnded
	default:
	}
	return "", PublicationPending
}

func PublicationReceipt(publication string, state PublicationState, port uint16) string {
	switch state {
	case PublicationDone:
		return publication
	case PublicationWithdrawn:
		return fmt.Sprintf("port %d is not forwarded because its forward was removed", port)
	case PublicationEnded:
		return fmt.Sprintf("port %d was not forwarded because the task ended; its notice gives the reason", port)
	case PublicationStopped:
		return fmt.Sprintf("port %d was not forwarded because the task was stopped", port)
	case PublicationPending:
	}
	return fmt.Sprintf("port %d is forwarded once the task starts; a notice gives its address", port)
}

func (self *Manager) ForgetPort(id string, port uint16) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	current := self.entries[id]
	if current == nil || forwardPort(current) != port {
		return false
	}
	current.snapshot.Invocation.ForwardPort = 0
	current.publication = ""
	signalPublication(current)
	self.signalChange()
	return true
}

func forwardPort(current *entry) uint16 {
	if current.snapshot.Invocation == nil {
		return 0
	}
	return current.snapshot.Invocation.ForwardPort
}

func signalPublication(current *entry) {
	if current.publicationSignal == nil {
		return
	}
	select {
	case <-current.publicationSignal:
	default:
		close(current.publicationSignal)
	}
}

func isLive(state State) bool {
	return state == Queued || state == Running || state == Stopping
}

func cloneInvocation(invocation *Invocation) *Invocation {
	if invocation == nil {
		return nil
	}
	copyOfInvocation := *invocation
	return &copyOfInvocation
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.Previous = slices.Clone(snapshot.Previous)
	snapshot.Invocation = cloneInvocation(snapshot.Invocation)
	return snapshot
}

func previousRuns(snapshot Snapshot) []RunOutcome {
	previous := append(slices.Clone(snapshot.Previous), RunOutcome{
		Run: snapshot.Run, State: snapshot.State, Output: snapshot.Output, Failure: snapshot.Failure,
	})
	if len(previous) > PreviousRunLimit {
		previous = previous[len(previous)-PreviousRunLimit:]
	}
	return previous
}

func (self *Manager) TakeTimeFrom(now func() time.Time) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.now = now
}

func (self *Manager) queueSize(replacement *entry) int {
	count := 0
	for _, current := range self.entries {
		if current != replacement && (current.snapshot.State == Queued || current.isRestarting) {
			count++
		}
	}
	return count
}

func (self *Manager) allocateID(toolName string) string {
	name := generatedName(toolName)
	if self.nextIDs == nil {
		self.nextIDs = make(map[string]int)
	}
	for {
		self.nextIDs[name]++
		id := fmt.Sprintf("%s-%d", name, self.nextIDs[name])
		if self.entries[id] == nil {
			return id
		}
	}
}

func generatedName(toolName string) string {
	if ValidateName(toolName) != nil {
		return "task"
	}
	return toolName
}

func (self *Manager) stopLocked(current *entry, cause error) bool {
	hasStopped := false
	if current.isRestarting && current.restartCancel != nil {
		select {
		case <-current.restartDone:
		default:
			current.restartCancel(cause)
			hasStopped = true
		}
	}
	if current.snapshot.State == Running || current.snapshot.State == Queued {
		current.isStopped = true
		current.snapshot.State = Stopping
		current.snapshot.IsRespawning = false
		current.cancel(cause)
		hasStopped = true
	}
	if hasStopped && current.isBackground {
		self.signalChange()
	}
	return hasStopped
}

func outputSnapshot(current *entry) Snapshot {
	snapshot := cloneSnapshot(current.snapshot)
	if current.output != nil && isLive(snapshot.State) {
		snapshot.Output = current.output.Report()
	}
	return snapshot
}
