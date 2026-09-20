package cycle

import "context"

type TransitionKind int

const (
	Quit TransitionKind = iota
	NewSession
	ResumeSession
	Restart
)

type Transition struct {
	Kind      TransitionKind
	Arguments []string
}

type StopReason int

const (
	StoppedByQuit StopReason = iota
	StoppedForNewSession
	StoppedForResume
	StoppedForRestart
	StoppedByFailure
)

func (self Transition) StopReason() StopReason {
	switch self.Kind {
	case NewSession:
		return StoppedForNewSession
	case ResumeSession:
		return StoppedForResume
	case Restart:
		return StoppedForRestart
	case Quit:
		return StoppedByQuit
	default:
		return StoppedByFailure
	}
}

type Session struct {
	Name         string
	ID           string
	Directory    string
	WorkspaceDir string
	Provider     string
	Model        string
	Effort       string
}

type SessionStarting struct {
	Session Session
}

type SessionStarted struct {
	Session Session
}

type SessionStopping struct {
	Session Session
	Reason  StopReason
}

type SessionStopped struct {
	Session Session
	Reason  StopReason
}

type Hooks struct {
	reportError     func(error)
	sessionStarting []func(context.Context, SessionStarting) error
	sessionStarted  []func(context.Context, SessionStarted) error
	sessionStopping []func(context.Context, SessionStopping) error
	sessionStopped  []func(context.Context, SessionStopped) error
}

func NewHooks(reportError func(error)) *Hooks {
	return &Hooks{reportError: reportError}
}

func (self *Hooks) OnSessionStarting(callback func(context.Context, SessionStarting) error) {
	self.sessionStarting = append(self.sessionStarting, callback)
}

func (self *Hooks) OnSessionStarted(callback func(context.Context, SessionStarted) error) {
	self.sessionStarted = append(self.sessionStarted, callback)
}

func (self *Hooks) OnSessionStopping(callback func(context.Context, SessionStopping) error) {
	self.sessionStopping = append(self.sessionStopping, callback)
}

func (self *Hooks) OnSessionStopped(callback func(context.Context, SessionStopped) error) {
	self.sessionStopped = append(self.sessionStopped, callback)
}

func (self *Hooks) EmitSessionStarting(ctx context.Context, event SessionStarting) {
	for _, callback := range self.sessionStarting {
		self.report(callback(ctx, event))
	}
}

func (self *Hooks) EmitSessionStarted(ctx context.Context, event SessionStarted) {
	for _, callback := range self.sessionStarted {
		self.report(callback(ctx, event))
	}
}

func (self *Hooks) EmitSessionStopping(ctx context.Context, event SessionStopping) {
	for _, callback := range self.sessionStopping {
		self.report(callback(ctx, event))
	}
}

func (self *Hooks) EmitSessionStopped(ctx context.Context, event SessionStopped) {
	for _, callback := range self.sessionStopped {
		self.report(callback(ctx, event))
	}
}

func (self *Hooks) report(err error) {
	if err != nil && self.reportError != nil {
		self.reportError(err)
	}
}
