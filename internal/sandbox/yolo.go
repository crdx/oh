package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

func runYolo(ctx context.Context, directory string, command string, policy Policy) (Result, error) {
	if err := requireShell(shell); err != nil {
		return Result{}, err
	}

	if policy.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, policy.Timeout)
		defer cancel()
	}

	var output boundedBuffer

	startedAt := time.Now()

	child := exec.CommandContext(ctx, shell, "-c", command) //nolint:gosec // the whole of what --yolo asks for
	child.Dir = directory
	child.Stdout = &output
	child.Stderr = &output
	child.Env = configuredEnvironment(policy.Env, policy.SetEnv)
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	child.Cancel = func() error {
		return syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
	}

	err := child.Run()

	status := collect(child)
	result := Result{
		Output:     output.String(),
		ExitCode:   status.ExitCode,
		Signal:     status.Signal,
		CPUTime:    status.CPUTime,
		PeakMemory: status.PeakMemory,
	}

	if ctx.Err() != nil {
		return stoppedResult(ctx, policy, result, startedAt)
	}

	var exitError *exec.ExitError
	if err != nil && !errors.As(err, &exitError) {
		return Result{}, fmt.Errorf("could not run the command: %w", err)
	}

	return result, nil
}
