package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"crdx.org/oh/internal/sandbox/keeper"
)

const strayOutputGrace = time.Second

func startYolo(
	ctx context.Context,
	directory string,
	command string,
	policy Policy,
	output Output,
) (Command, error) {
	if err := requireShell(shell); err != nil {
		return nil, err
	}

	if err := ensureSane(directory, command); err != nil {
		return nil, err
	}

	var cancel context.CancelFunc
	if policy.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, policy.Timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}

	startedAt := time.Now()

	child := exec.CommandContext(ctx, shell, "-c", command) //nolint:gosec // the whole of what --yolo asks for
	child.Dir = directory
	reader, writer, err := os.Pipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("could not run the command: %w", err)
	}
	child.Stdout = writer
	child.Stderr = writer
	child.Env = configuredEnvironment(policy.Env, policy.SetEnv)
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	child.Cancel = func() error {
		return syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
	}

	err = child.Start()
	_ = writer.Close()
	if err != nil {
		_ = reader.Close()
		defer cancel()

		if ctx.Err() != nil {
			_, refusal := stoppedResult(ctx, policy, Result{}, startedAt)
			return nil, refusal
		}

		return nil, fmt.Errorf("could not run the command: %w", err)
	}

	endOfOutput := make(chan struct{})
	go func() {
		defer close(endOfOutput)
		_, _ = io.Copy(output, reader)
	}()

	return &startedCommand{
		process:   hostProcess{child: child, reader: reader, endOfOutput: endOfOutput},
		ctx:       ctx,
		cancel:    cancel,
		policy:    policy,
		output:    output,
		startedAt: startedAt,
	}, nil
}

type hostProcess struct {
	child       *exec.Cmd
	reader      *os.File
	endOfOutput chan struct{}
}

func (self hostProcess) Wait() (keeper.Status, error) {
	err := self.child.Wait()
	_ = syscall.Kill(-self.child.Process.Pid, syscall.SIGKILL)

	select {
	case <-self.endOfOutput:
	case <-time.After(strayOutputGrace):
		_ = self.reader.Close()
		<-self.endOfOutput
	}
	_ = self.reader.Close()

	status := collect(self.child)

	var exitError *exec.ExitError
	if err != nil && !errors.As(err, &exitError) {
		return status, err
	}

	return status, nil
}

func (self hostProcess) Signal(signal syscall.Signal) error {
	return syscall.Kill(-self.child.Process.Pid, signal)
}
