package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"crdx.org/oh/internal/sandbox/keeper"
	"crdx.org/oh/internal/sandbox/testnamespace"
)

type Output interface {
	io.Writer
	String() string
}

type Runner interface {
	Run(ctx context.Context, directory string, command string, policy Policy) (Result, error)
	Start(
		ctx context.Context,
		directory string,
		command string,
		policy Policy,
		output Output,
	) (Command, error)
}

type Command interface {
	Wait() (Result, error)
	Signal(signal syscall.Signal) error
	Stop()
}

func Direct() Runner {
	return runner{spawn: spawnAlone}
}

func Wrapped(keeperProcess *keeper.Keeper) Runner {
	return runner{spawn: spawn(keeperProcess)}
}

type process interface {
	Wait() (keeper.Status, error)
	Signal(signal syscall.Signal) error
}

type spawner func(
	ctx context.Context,
	directory string,
	environment []string,
	output Output,
) (process, error)

type runner struct {
	spawn spawner
}

func (self runner) Run(ctx context.Context, directory string, command string, policy Policy) (Result, error) {
	if policy.Yolo {
		return runYolo(ctx, directory, command, policy)
	}

	runningCommand, err := self.Start(ctx, directory, command, policy, &boundedBuffer{})
	if err != nil {
		return Result{}, err
	}

	return runningCommand.Wait()
}

func (self runner) Start(
	ctx context.Context,
	directory string,
	command string,
	policy Policy,
	output Output,
) (Command, error) {
	if err := validate(ctx, policy); err != nil {
		return nil, err
	}
	if len(policy.Deny) > 0 && policy.DenyPaths == nil {
		denyPaths, err := policy.DiscoverDenyPaths()
		if err != nil {
			return nil, err
		}
		policy.DenyPaths = append([]string{}, denyPaths...)
	}

	if err := ensureSane(directory, command); err != nil {
		return nil, err
	}

	encodedPolicy, err := json.Marshal(policy)
	if err != nil {
		return nil, fmt.Errorf("could not write the policy: %w", err)
	}

	if policy.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, policy.Timeout)

		return self.begin(ctx, cancel, directory, command, policy, string(encodedPolicy), output)
	}

	ctx, cancel := context.WithCancel(ctx)

	return self.begin(ctx, cancel, directory, command, policy, string(encodedPolicy), output)
}

func ensureSane(directory string, command string) error {
	for _, crossing := range []struct {
		name  string
		value string
	}{
		{name: "working directory", value: directory},
		{name: "command", value: command},
	} {
		if strings.ContainsRune(crossing.value, 0) {
			return fmt.Errorf("the %s carries a null byte, so it cannot be passed on whole", crossing.name)
		}

		if !utf8.ValidString(crossing.value) {
			return fmt.Errorf(
				"the %s is not valid UTF-8, so the command would run as something else", crossing.name,
			)
		}
	}

	return nil
}

func commandEnvironment(policy Policy, encodedPolicy string, command string) []string {
	environment := getEnvironment(policy.Env)
	environment = append(environment, envPolicy+"="+encodedPolicy, envCommand+"="+command)

	return append(environment, testnamespace.Environment()...)
}

func (self runner) spawnerFor(policy Policy) spawner {
	if policy.Network {
		return spawnOnHostNetwork
	}

	return self.spawn
}

func (self runner) begin(
	ctx context.Context,
	cancel context.CancelFunc,
	directory string,
	command string,
	policy Policy,
	encodedPolicy string,
	output Output,
) (Command, error) {
	startedAt := time.Now()

	environment := commandEnvironment(policy, encodedPolicy, command)

	spawn := self.spawnerFor(policy)

	child, err := spawn(ctx, directory, environment, output)
	if err != nil {
		defer cancel()

		if ctx.Err() != nil {
			_, refusal := stoppedResult(ctx, policy, Result{}, startedAt)
			return nil, refusal
		}

		return nil, fmt.Errorf("could not run the command: %w", err)
	}

	return &startedCommand{
		process:   child,
		ctx:       ctx,
		cancel:    cancel,
		policy:    policy,
		output:    output,
		startedAt: startedAt,
	}, nil
}

type startedCommand struct {
	process   process
	ctx       context.Context //nolint:containedctx // the command outlives the call that started it
	cancel    context.CancelFunc
	policy    Policy
	output    Output
	startedAt time.Time
}

func (self *startedCommand) Wait() (Result, error) {
	defer self.cancel()

	status, err := self.process.Wait()

	result := Result{
		Output:     self.output.String(),
		ExitCode:   status.ExitCode,
		Signal:     status.Signal,
		CPUTime:    status.CPUTime,
		PeakMemory: status.PeakMemory,
	}

	if self.ctx.Err() != nil {
		return stoppedResult(self.ctx, self.policy, result, self.startedAt)
	}

	if err != nil {
		return Result{}, fmt.Errorf("could not run the command: %w", err)
	}

	if result.ExitCode == notStarted && strings.HasPrefix(result.Output, notice) {
		return Result{}, fmt.Errorf(
			"the sandbox could not start: %s",
			strings.TrimSpace(strings.TrimPrefix(result.Output, notice)),
		)
	}

	return result, nil
}

func (self *startedCommand) Signal(signal syscall.Signal) error { return self.process.Signal(signal) }

func (self *startedCommand) Stop() { self.cancel() }

func spawnAlone(
	ctx context.Context,
	directory string,
	environment []string,
	output Output,
) (process, error) {
	return spawnStub(ctx, directory, environment, output, namespaceAttributes())
}

func spawnOnHostNetwork(
	ctx context.Context,
	directory string,
	environment []string,
	output Output,
) (process, error) {
	return spawnStub(ctx, directory, environment, output, hostNetworkAttributes())
}

func spawnStub(
	ctx context.Context,
	directory string,
	environment []string,
	output Output,
	attributes *syscall.SysProcAttr,
) (process, error) {
	stub := exec.CommandContext(ctx, executable)
	stub.Args = []string{commandName}
	stub.Dir = directory
	stub.Stdout = output
	stub.Stderr = output
	stub.Env = environment
	stub.SysProcAttr = attributes
	stub.Cancel = func() error {
		return syscall.Kill(-stub.Process.Pid, syscall.SIGKILL)
	}

	if err := stub.Start(); err != nil {
		return nil, err
	}

	return alone{stub: stub}, nil
}

type alone struct {
	stub *exec.Cmd
}

func (self alone) Wait() (keeper.Status, error) {
	err := self.stub.Wait()
	status := collect(self.stub)

	var exitError *exec.ExitError
	if err != nil && !errors.As(err, &exitError) {
		return status, err
	}

	return status, nil
}

func (self alone) Signal(signal syscall.Signal) error {
	if self.stub.Process == nil {
		return errors.New("the command is not running")
	}

	return syscall.Kill(-self.stub.Process.Pid, signal)
}

func spawn(keeperProcess *keeper.Keeper) spawner {
	return func(
		ctx context.Context,
		directory string,
		environment []string,
		output Output,
	) (process, error) {
		child, err := keeperProcess.Spawn(ctx, directory, environment, output)
		if err != nil {
			return nil, err
		}

		go func() {
			<-ctx.Done()
			_ = child.Signal(syscall.SIGKILL)
		}()

		return child, nil
	}
}
