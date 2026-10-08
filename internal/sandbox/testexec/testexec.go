package testexec

import (
	"context"
	"errors"
	"os"
	osexec "os/exec"
	"sync"
	"syscall"

	"crdx.org/oh/pkg/sandbox"
)

const StandInVariable = "OH_TEST_STAND_IN"

type Runner struct{}

func New() *Runner { return &Runner{} }

func (self *Runner) RunArgv(
	ctx context.Context,
	directory string,
	arguments []string,
	policy sandbox.Policy,
) (sandbox.Result, error) {
	runningCommand, err := self.StartArgv(ctx, directory, arguments, policy, &testOutput{})
	if err != nil {
		return sandbox.Result{}, err
	}
	return runningCommand.Wait()
}

func (self *Runner) StartArgv(
	ctx context.Context,
	directory string,
	arguments []string,
	_ sandbox.Policy,
	output sandbox.Output,
) (sandbox.Command, error) {
	child := osexec.CommandContext(ctx, arguments[0], arguments[1:]...) //nolint:gosec // test stand-in for the sandbox
	child.Dir = directory
	child.Env = append(os.Environ(), StandInVariable+"=1")
	child.Stdout = output
	child.Stderr = output
	if err := child.Start(); err != nil {
		return nil, err
	}
	return &testCommand{child: child, output: output}, nil
}

type testCommand struct {
	child  *osexec.Cmd
	output sandbox.Output
}

func (self *testCommand) Wait() (sandbox.Result, error) {
	err := self.child.Wait()
	result := sandbox.Result{Output: self.output.String()}
	if exit, isExit := errors.AsType[*osexec.ExitError](err); isExit {
		result.ExitCode = exit.ExitCode()
		return result, nil
	}
	return result, err
}

func (self *testCommand) Signal(signal syscall.Signal) error {
	return self.child.Process.Signal(signal)
}

func (self *testCommand) Stop() { _ = self.child.Process.Kill() }

type testOutput struct {
	mutex  sync.Mutex
	output []byte
}

func (self *testOutput) Write(data []byte) (int, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.output = append(self.output, data...)
	return len(data), nil
}

func (self *testOutput) String() string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return string(self.output)
}
