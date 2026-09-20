package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"

	"crdx.org/oh/internal/sandbox/keeper"
	"crdx.org/oh/internal/stop"
	"crdx.org/oh/internal/util"
)

const (
	envPolicy  = "IO_SANDBOX_POLICY"
	envCommand = "IO_SANDBOX_COMMAND"
	envProbe   = "IO_SANDBOX_PROBE"
)

const (
	executable  = "/proc/self/exe"
	shell       = "/bin/bash"
	commandName = "oh (command)"
	probeName   = "oh (probe)"
)

const (
	notice         = "sandbox: "
	notStarted     = 125
	probeSucceeded = "sandbox probe succeeded"
)

func Init() {
	keeper.Init()

	if os.Getenv(envProbe) != "" {
		if err := applyNetwork(); err != nil {
			fmt.Fprint(os.Stderr, notice, err, "\n")
			os.Exit(notStarted)
		}

		if _, err := fmt.Fprintln(os.Stdout, probeSucceeded); err != nil {
			os.Exit(notStarted)
		}
		os.Exit(0)
	}

	encodedPolicy := os.Getenv(envPolicy)
	if encodedPolicy == "" {
		return
	}

	command := os.Getenv(envCommand)

	if err := execSandboxed(encodedPolicy, command); err != nil {
		fmt.Fprint(os.Stderr, notice, err, "\n")
		os.Exit(notStarted)
	}

	os.Exit(0)
}

func execSandboxed(encodedPolicy string, command string) error {
	runtime.LockOSThread()

	var policy Policy
	if err := json.Unmarshal([]byte(encodedPolicy), &policy); err != nil {
		return fmt.Errorf("could not read the policy: %w", err)
	}

	if err := applyMounts(policy); err != nil {
		return err
	}

	if !policy.Network {
		if err := applyNetwork(); err != nil {
			return err
		}
	}

	if err := dropCapabilities(); err != nil {
		return err
	}

	isUnixSocketScoped, err := applyLandlock(policy)
	if err != nil {
		return err
	}

	if err := applySeccomp(isUnixSocketScoped); err != nil {
		return err
	}

	environment := configuredEnvironment(policy.Env, policy.SetEnv)

	if err := applyLimits(policy); err != nil {
		return err
	}

	//nolint:gosec // running the command is the point, and the sandbox is why that is safe to do
	return syscall.Exec(shell, []string{shell, "-c", command}, environment)
}

func getEnvironment(allowedNames []string) []string {
	return configuredEnvironment(allowedNames, nil)
}

func configuredEnvironment(allowedNames []string, set map[string]string) []string {
	environment := make([]string, 0, len(allowedNames)+len(set))

	for _, name := range allowedNames {
		if _, isOverridden := set[name]; isOverridden {
			continue
		}

		if value, ok := os.LookupEnv(name); ok {
			environment = append(environment, name+"="+value)
		}
	}

	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}

	slices.Sort(names)

	for _, name := range names {
		environment = append(environment, name+"="+set[name])
	}

	return environment
}

const maxOutput = 8 << 20

type boundedBuffer struct {
	buffer bytes.Buffer
}

func (self *boundedBuffer) Write(data []byte) (int, error) {
	if room := maxOutput - self.buffer.Len(); room > 0 {
		self.buffer.Write(data[:min(room, len(data))])
	}

	return len(data), nil
}

func (self *boundedBuffer) String() string {
	return self.buffer.String()
}

type Result struct {
	Output     string
	ExitCode   int
	Signal     syscall.Signal
	CPUTime    time.Duration
	PeakMemory uint64
}

func Run(ctx context.Context, directory string, command string, policy Policy) (Result, error) {
	return Direct().Run(ctx, directory, command, policy)
}

func collect(child *exec.Cmd) keeper.Status {
	var status keeper.Status
	if child.ProcessState == nil {
		return status
	}

	status.ExitCode = child.ProcessState.ExitCode()
	status.CPUTime = child.ProcessState.UserTime() + child.ProcessState.SystemTime()
	if waitStatus, ok := child.ProcessState.Sys().(syscall.WaitStatus); ok && waitStatus.Signaled() {
		status.Signal = waitStatus.Signal()
	}
	if usage, ok := child.ProcessState.SysUsage().(*syscall.Rusage); ok && usage.Maxrss > 0 {
		status.PeakMemory = uint64(usage.Maxrss) * 1024
	}

	return status
}

func validate(ctx context.Context, policy Policy) error {
	if err := policy.sane(); err != nil {
		return err
	}

	if err := policy.grantPathsSafe(); err != nil {
		return err
	}

	if absent := policy.missingPaths(); len(absent) > 0 {
		return fmt.Errorf(
			"the policy grants paths that do not exist: %s", strings.Join(absent, ", "),
		)
	}

	return Supported(ctx)
}

func stoppedResult(ctx context.Context, policy Policy, result Result, startedAt time.Time) (Result, error) {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return result, fmt.Errorf("the command did not finish within %s", policy.Timeout)
	}

	return result, fmt.Errorf(
		"the command was stopped after %s%s",
		util.CompactDuration(time.Since(startedAt).Round(100*time.Millisecond)),
		stop.Phrase(ctx),
	)
}
