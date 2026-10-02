package harness

import (
	"context"
	"errors"
	"time"

	"crdx.org/oh/internal/app/experimental"
	"crdx.org/oh/internal/app/feedback"
	"crdx.org/oh/internal/app/hostcommand"
	"crdx.org/oh/internal/app/painter"
	"crdx.org/oh/internal/app/spinner"
	"crdx.org/oh/pkg/agent"
)

const hostCommandEndGrace = 5 * time.Second

var errHostCommandUnderway = errors.New("a command is already running; press escape to stop it")

type hostCommandState struct {
	run        func(context.Context, string, string, *hostcommand.Output) (hostcommand.Result, error)
	command    string
	output     *hostcommand.Output
	startedAt  time.Time
	stop       context.CancelFunc
	isStopping bool
	outcomes   chan hostcommand.Outcome
}

func (self *App) hostCommandOutcomes() chan hostcommand.Outcome {
	if self.hostCommand.outcomes == nil {
		self.hostCommand.outcomes = make(chan hostcommand.Outcome, 1)
	}

	return self.hostCommand.outcomes
}

func (self *App) isHostCommandUnderway() bool {
	return self.hostCommand.stop != nil
}

func (self *App) startHostCommand(directory string, command string) error {
	if self.isHostCommandUnderway() {
		return errHostCommandUnderway
	}

	run := self.hostCommand.run
	if run == nil {
		run = hostcommand.Run
	}

	stopContext, stop := context.WithCancel(context.Background())
	output := &hostcommand.Output{}
	self.hostCommand.command = command
	self.hostCommand.output = output
	self.hostCommand.startedAt = self.getNow()
	self.hostCommand.stop = stop
	self.hostCommand.isStopping = false

	outcomes := self.hostCommandOutcomes()
	go func() {
		result, err := run(stopContext, directory, command, output)
		outcomes <- hostcommand.Outcome{Result: result, Failure: err}
	}()

	return nil
}

func (self *App) stopHostCommand() bool {
	if !self.isHostCommandUnderway() || self.hostCommand.isStopping {
		return false
	}

	self.hostCommand.isStopping = true
	self.hostCommand.stop()

	return true
}

func (self *App) hostCommandEnded(outcome hostcommand.Outcome) {
	if self.hostCommand.stop != nil {
		self.hostCommand.stop()
	}
	self.hostCommand = hostCommandState{run: self.hostCommand.run, outcomes: self.hostCommand.outcomes}

	if outcome.Failure != nil {
		self.showFeedback(feedback.Command, feedback.Message{
			Text:   "The command could not run: " + outcome.Failure.Error(),
			Status: agent.ErrorStatus,
		})
		return
	}

	result := outcome.Result
	result.Output = self.withinToolOutputLimit(result.Output)
	self.hostCommandRan(hostcommand.RanEvent(result))
}

func (self *App) hostCommandRan(event agent.Event) {
	if self.holdNotice(event) &&
		!hostcommand.IsStoppedByUser(event) &&
		self.experimental.IsEnabled(experimental.CommandStartsTurn) {
		self.startTurn()
	}
}

func (self *App) awaitHostCommand() {
	if !self.isHostCommandUnderway() {
		return
	}

	for {
		select {
		case outcome := <-self.hostCommandOutcomes():
			self.hostCommandEnded(outcome)
			return
		case receivedSignal := <-self.termination.signals:
			self.termination.receivedSignal = receivedSignal
			self.stopHostCommand()
		}
	}
}

func (self *App) endHostCommand() {
	if !self.isHostCommandUnderway() {
		return
	}

	self.stopHostCommand()
	select {
	case <-self.hostCommandOutcomes():
	case <-time.After(hostCommandEndGrace):
	}
	self.hostCommand = hostCommandState{run: self.hostCommand.run, outcomes: self.hostCommand.outcomes}
}

func (self *App) hostCommandRows(columns int) []string {
	if !self.isHostCommandUnderway() {
		return nil
	}

	elapsedTime := self.getNow().Sub(self.hostCommand.startedAt)

	return painter.RenderHostCommand(self.hostCommand.command, self.hostCommand.output.LatestLine(), elapsedTime, columns)
}

func (self *App) nextHostCommandRefresh(at time.Time) time.Time {
	if !self.isHostCommandUnderway() {
		return time.Time{}
	}

	return at.Add(spinner.Activity.RefreshInterval())
}
