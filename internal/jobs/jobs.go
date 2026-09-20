package jobs

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/internal/util"
)

const (
	NameLengthLimit       = 10
	normalGracePeriod     = 5 * time.Second
	shutdownGracePeriod   = time.Second
	reportedBytePrecision = 3
)

type State string

const (
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateComplete State = "complete"
	StateFailed   State = "failed"
	StateStopped  State = "stopped"
	StateEnded    State = "ended with the session"
)

var (
	ErrClosed   = errors.New("session is closed to new jobs")
	ErrTaken    = errors.New("job already running")
	ErrNotFound = errors.New("job not found")
)

func ValidateName(name string) error {
	if len(name) == 0 || len(name) > NameLengthLimit {
		return fmt.Errorf("job name must match [a-z0-9-]{1,%d}", NameLengthLimit)
	}

	for _, character := range name {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return fmt.Errorf("job name must match [a-z0-9-]{1,%d}", NameLengthLimit)
		}
	}

	return nil
}

type Snapshot struct {
	Name         string    `json:"name"`
	Command      string    `json:"command"`
	State        State     `json:"state"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at,omitzero"`
	ExitCode     int       `json:"code,omitempty"`
	Failure      string    `json:"failure,omitempty"`
	DroppedBytes int       `json:"-"`
}

func (self Snapshot) IsLive() bool { return isLive(self.State) }

func (self Snapshot) Describe() string {
	return self.Name + ": " + self.Outcome()
}

func (self Snapshot) Outcome() string {
	stateWithDuration := string(self.State)

	if self.IsLive() {
		stateWithDuration += " for " + util.CompactDuration(time.Since(self.StartedAt).Round(time.Second))
	} else if !self.EndedAt.IsZero() {
		stateWithDuration += " after " + util.CompactDuration(self.EndedAt.Sub(self.StartedAt).Round(time.Second))
	}

	parts := []string{stateWithDuration}

	if self.ExitCode != 0 {
		parts = append(parts, fmt.Sprintf("exit(%d)", self.ExitCode))
	}

	if self.Failure != "" {
		parts = append(parts, self.Failure)
	}

	return strings.Join(parts, ", ")
}

type Conclusion struct {
	Snapshot     Snapshot `json:"snapshot"`
	Output       string   `json:"output,omitempty"`
	DroppedBytes int      `json:"dropped_bytes,omitempty"`
}

type job struct {
	name           string
	command        string
	state          State
	startedAt      time.Time
	endedAt        time.Time
	code           int
	failure        string
	policy         sandbox.Policy
	output         *spool
	runningCommand sandbox.Command
	over           chan struct{}
	waiters        int
}

type Manager struct {
	runner      sandbox.Runner
	mutex       sync.Mutex
	jobs        map[string]*job
	order       []string
	isClosed    bool
	watchers    sync.WaitGroup
	conclusions chan Conclusion
}

const conclusionsHeld = 64

func New(runner sandbox.Runner) *Manager {
	return &Manager{
		runner:      runner,
		jobs:        make(map[string]*job),
		conclusions: make(chan Conclusion, conclusionsHeld),
	}
}

func (self *Manager) Conclusions() <-chan Conclusion {
	return self.conclusions
}

func (self *Manager) Restore(rememberedJobs []Snapshot) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	for _, snapshot := range rememberedJobs {
		if snapshot.Name == "" || self.jobs[snapshot.Name] != nil {
			continue
		}

		state := snapshot.State
		if isLive(state) {
			state = StateEnded
		}

		self.jobs[snapshot.Name] = &job{
			name:      snapshot.Name,
			command:   snapshot.Command,
			state:     state,
			startedAt: snapshot.StartedAt,
			endedAt:   snapshot.EndedAt,
			code:      snapshot.ExitCode,
			failure:   snapshot.Failure,
			output:    &spool{},
			over:      closedChannel(),
		}
		self.order = append(self.order, snapshot.Name)
	}
}

func closedChannel() chan struct{} {
	over := make(chan struct{})
	close(over)

	return over
}

func (self *Manager) RememberedCommand(name string) (string, bool) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	found, isKnown := self.jobs[name]
	if !isKnown || found.command == "" {
		return "", false
	}

	return found.command, true
}

func (self *Manager) Start(
	ctx context.Context,
	name string,
	directory string,
	command string,
	policy sandbox.Policy,
) (Snapshot, error) {
	openingJob, err := self.claim(name, command, policy)
	if err != nil {
		return Snapshot{}, err
	}

	runningCommand, err := self.runner.Start(context.WithoutCancel(ctx), directory, command, policy, openingJob.output)
	if err != nil {
		self.conclude(openingJob, StateFailed, 0, err.Error())
		close(openingJob.over)

		return self.snapshot(openingJob), fmt.Errorf("the job could not be started: %w", err)
	}

	if !self.settleStarted(openingJob, runningCommand) {
		runningCommand.Stop()
	}

	self.watchers.Add(1)

	go self.watch(openingJob)

	return self.snapshot(openingJob), nil
}

func (self *Manager) Stop(name string) (Snapshot, error) {
	self.mutex.Lock()
	found, isKnown := self.jobs[name]
	self.mutex.Unlock()

	if !isKnown {
		return Snapshot{}, ErrNotFound
	}

	self.end(found, normalGracePeriod)

	return self.snapshot(found), nil
}

func (self *Manager) Status(name string) (Snapshot, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	found, isKnown := self.jobs[name]
	if !isKnown {
		return Snapshot{}, ErrNotFound
	}

	return self.describe(found), nil
}

func (self *Manager) Output(name string) (string, Snapshot, error) {
	self.mutex.Lock()
	found, isKnown := self.jobs[name]
	self.mutex.Unlock()

	if !isKnown {
		return "", Snapshot{}, ErrNotFound
	}

	return found.output.String(), self.snapshot(found), nil
}

func (self *Manager) Wait(ctx context.Context, names []string) (string, error) {
	self.mutex.Lock()
	watchedJobs := make([]*job, 0, len(names))
	for _, name := range names {
		found, isKnown := self.jobs[name]
		if !isKnown {
			self.mutex.Unlock()
			if len(names) == 1 {
				return "", ErrNotFound
			}
			return "", fmt.Errorf("%w: %s", ErrNotFound, name)
		}
		watchedJobs = append(watchedJobs, found)
	}
	if len(watchedJobs) == 0 {
		self.mutex.Unlock()
		return "", errors.New("at least one job name is required")
	}

	for _, watchedJob := range watchedJobs {
		watchedJob.waiters++
	}
	defer self.releaseWaiters(watchedJobs)

	if endedName, isEnded := getFirstEndedName(watchedJobs); isEnded {
		self.mutex.Unlock()
		return endedName, nil
	}

	cases := make([]reflect.SelectCase, 0, len(watchedJobs)+1)
	for _, watchedJob := range watchedJobs {
		cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(watchedJob.over)})
	}
	cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ctx.Done())})
	self.mutex.Unlock()

	reflect.Select(cases)

	self.mutex.Lock()
	endedName, isEnded := getFirstEndedName(watchedJobs)
	self.mutex.Unlock()
	if isEnded {
		return endedName, nil
	}

	return "", ctx.Err()
}

func getFirstEndedName(watchedJobs []*job) (string, bool) {
	for _, watchedJob := range watchedJobs {
		if !isLive(watchedJob.state) {
			return watchedJob.name, true
		}
	}

	return "", false
}

func (self *Manager) List() []Snapshot {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	listing := make([]Snapshot, 0, len(self.order))
	for _, name := range self.order {
		listing = append(listing, self.describe(self.jobs[name]))
	}

	return listing
}

func (self *Manager) Discard(name string) (Snapshot, error) {
	self.mutex.Lock()
	found, isKnown := self.jobs[name]
	self.mutex.Unlock()

	if !isKnown {
		return Snapshot{}, ErrNotFound
	}

	self.end(found, normalGracePeriod)

	discardedJob := self.snapshot(found)

	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.remove(name)

	return discardedJob, nil
}

func (self *Manager) PruneFinished() []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	var prunedNames []string

	for _, name := range slices.Clone(self.order) {
		if isLive(self.jobs[name].state) {
			continue
		}

		prunedNames = append(prunedNames, name)
		self.remove(name)
	}

	return prunedNames
}

func (self *Manager) StopHolding(holds func(sandbox.Policy) bool) []string {
	self.mutex.Lock()
	var stoppingJobs []*job
	for _, name := range self.order {
		found := self.jobs[name]
		if isStoppable(found.state) && holds(found.policy) {
			stoppingJobs = append(stoppingJobs, found)
		}
	}
	self.mutex.Unlock()

	names := make([]string, 0, len(stoppingJobs))
	for _, endingJob := range stoppingJobs {
		_ = self.beginEnd(endingJob)
		self.watchers.Go(func() { self.end(endingJob, normalGracePeriod) })
		names = append(names, endingJob.name)
	}

	return names
}

func (self *Manager) Close() error {
	self.mutex.Lock()
	self.isClosed = true
	live := make([]*job, 0, len(self.jobs))
	for _, found := range self.jobs {
		if isLive(found.state) {
			live = append(live, found)
		}
	}
	self.mutex.Unlock()

	var endingJobs sync.WaitGroup
	for _, found := range live {
		endingJobs.Go(func() { self.end(found, shutdownGracePeriod) })
	}
	endingJobs.Wait()

	self.watchers.Wait()

	return nil
}

func (self *Manager) releaseWaiters(watchedJobs []*job) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	for _, watchedJob := range watchedJobs {
		watchedJob.waiters--
	}
}

func (self *Manager) settleStarted(openingJob *job, runningCommand sandbox.Command) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	openingJob.runningCommand = runningCommand

	if openingJob.state != StateStarting {
		return false
	}

	openingJob.state = StateRunning

	return true
}

func (self *Manager) remove(name string) {
	delete(self.jobs, name)
	self.order = slices.DeleteFunc(self.order, func(remainingName string) bool { return remainingName == name })
}

func (self *Manager) claim(name string, command string, policy sandbox.Policy) (*job, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.isClosed {
		return nil, ErrClosed
	}

	if found, isKnown := self.jobs[name]; isKnown && isLive(found.state) {
		return nil, ErrTaken
	}

	openingJob := &job{
		name:      name,
		command:   command,
		state:     StateStarting,
		startedAt: time.Now(),
		policy:    policy,
		output:    &spool{},
		over:      make(chan struct{}),
	}

	if !slices.Contains(self.order, name) {
		self.order = append(self.order, name)
	}
	self.jobs[name] = openingJob

	return openingJob, nil
}

func (self *Manager) watch(endingJob *job) {
	defer self.watchers.Done()
	defer close(endingJob.over)

	result, err := endingJob.runningCommand.Wait()
	failure := resultFailure(result, endingJob.policy, err)

	switch {
	case err != nil || result.ExitCode != 0:
		self.conclude(endingJob, self.endingState(endingJob, StateFailed), result.ExitCode, failure)
	default:
		self.conclude(endingJob, self.endingState(endingJob, StateComplete), 0, "")
	}

	self.announceEnd(endingJob)
}

func (self *Manager) announceEnd(endedJob *job) {
	self.mutex.Lock()
	isAnnounced := endedJob.waiters == 0 &&
		(endedJob.state == StateComplete || endedJob.state == StateFailed)
	snapshot := self.describe(endedJob)
	self.mutex.Unlock()

	if !isAnnounced {
		return
	}

	conclusion := Conclusion{
		Snapshot:     snapshot,
		Output:       endedJob.output.String(),
		DroppedBytes: snapshot.DroppedBytes,
	}

	select {
	case self.conclusions <- conclusion:
	default:
	}
}

func resultFailure(result sandbox.Result, policy sandbox.Policy, waitFailure error) string {
	var parts []string
	if waitFailure != nil {
		parts = append(parts, waitFailure.Error())
	}
	if killNotice := sandbox.KillNotice(result, policy); killNotice != "" {
		parts = append(parts, killNotice)
	}

	return strings.Join(parts, "\n")
}

func (self *Manager) endingState(endingJob *job, natural State) State {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if endingJob.state == StateStopping {
		return StateStopped
	}

	return natural
}

func (self *Manager) conclude(endingJob *job, state State, code int, failure string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	endingJob.state = state
	endingJob.code = code
	endingJob.failure = failure
	endingJob.endedAt = time.Now()
}

func (self *Manager) beginEnd(endingJob *job) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if !isLive(endingJob.state) {
		return false
	}

	endingJob.state = StateStopping

	return true
}

func (self *Manager) end(endingJob *job, patience time.Duration) {
	if !self.beginEnd(endingJob) {
		return
	}

	self.mutex.Lock()
	runningCommand := endingJob.runningCommand
	self.mutex.Unlock()

	if runningCommand == nil {
		self.conclude(endingJob, StateStopped, 0, "")

		return
	}

	_ = runningCommand.Signal(syscall.SIGTERM)

	select {
	case <-endingJob.over:
		return
	case <-time.After(patience):
	}

	runningCommand.Stop()
	<-endingJob.over
}

func (self *Manager) snapshot(subject *job) Snapshot {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.describe(subject)
}

func (self *Manager) describe(subject *job) Snapshot {
	return Snapshot{
		Name:         subject.name,
		Command:      subject.command,
		State:        subject.state,
		StartedAt:    subject.startedAt,
		EndedAt:      subject.endedAt,
		ExitCode:     subject.code,
		Failure:      subject.failure,
		DroppedBytes: subject.output.DroppedBytes(),
	}
}

func Report(status string, output string, droppedBytes int) string {
	lines := []string{status}

	if droppedBytes > 0 {
		lines = append(lines, fmt.Sprintf(
			"note: the oldest %s of output was dropped to keep the spool bounded.",
			util.FormatBytes(int64(droppedBytes), reportedBytePrecision),
		))
	}

	if strings.TrimSpace(output) == "" {
		marker := " (no output)"
		for _, punctuation := range []string{".", "!", "?"} {
			withoutPunctuation, found := strings.CutSuffix(lines[0], punctuation)
			if found {
				lines[0] = withoutPunctuation + marker + punctuation
				return strings.Join(lines, "\n")
			}
		}
		lines[0] += marker
		return strings.Join(lines, "\n")
	}

	return strings.Join(append(lines, strings.TrimRight(output, "\n")), "\n")
}

func isLive(state State) bool {
	return state == StateStarting || state == StateRunning || state == StateStopping
}

func isStoppable(state State) bool {
	return state == StateStarting || state == StateRunning
}
