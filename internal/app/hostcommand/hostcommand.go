package hostcommand

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"crdx.org/oh/internal/util"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/agent"
)

const Ran agent.Kind = "host_command_ran"

const (
	TimeLimit = 30 * time.Second
	waitDelay = time.Second

	stopPrecision = 100 * time.Millisecond

	shortestFence      = 3
	shellLanguage      = "bash"
	commandPrompt      = "$ "
	continuationPrompt = "> "
)

type Result struct {
	Command         string        `json:"command"`
	Output          string        `json:"output,omitempty"`
	ExitCode        int           `json:"exit_code,omitempty"`
	StoppedAfter    time.Duration `json:"stopped_after,omitempty"`
	IsStoppedByUser bool          `json:"stopped_by_user,omitempty"`
}

type Outcome struct {
	Result  Result
	Failure error
}

func Run(stopContext context.Context, directory string, command string, output *Output) (Result, error) {
	startedAt := time.Now()
	runContext, cancel := context.WithTimeout(stopContext, TimeLimit)
	defer cancel()

	//nolint:gosec // the person at the keyboard typed this command themselves
	shell := exec.CommandContext(runContext, "bash", "-c", command)
	shell.Dir = directory
	shell.Stdin = nil
	shell.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	shell.Cancel = func() error {
		return syscall.Kill(-shell.Process.Pid, syscall.SIGKILL)
	}
	shell.WaitDelay = waitDelay
	shell.Stdout = output
	shell.Stderr = output

	err := runBesideTerminal(shell, output)
	result := Result{Command: command, Output: output.Settled()}

	switch {
	case stopContext.Err() != nil:
		result.StoppedAfter = time.Since(startedAt).Round(stopPrecision)
		result.IsStoppedByUser = true

		return result, nil
	case errors.Is(runContext.Err(), context.DeadlineExceeded):
		result.StoppedAfter = TimeLimit

		return result, nil
	}

	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		result.ExitCode = exit.ExitCode()
	case err != nil:
		return Result{}, err
	}

	return result, nil
}

func runBesideTerminal(shell *exec.Cmd, output *Output) error {
	commandTerminal, err := openTerminal()
	if err != nil {
		return shell.Run()
	}
	defer commandTerminal.close()

	shell.ExtraFiles = []*os.File{commandTerminal.device}
	shell.SysProcAttr.Setctty = true
	shell.SysProcAttr.Ctty = terminalDescriptor
	commandTerminal.copyTo(output)

	if err := shell.Start(); err != nil {
		return err
	}
	commandTerminal.started()

	return shell.Wait()
}

func RanEvent(result Result) agent.Event {
	encodedResult, err := json.Marshal(result)
	if err != nil {
		return agent.Event{}
	}

	return agent.Event{Kind: Ran, Name: result.Command, State: encodedResult}
}

func IsStoppedByUser(event agent.Event) bool {
	result, isRead := read(event)

	return isRead && result.IsStoppedByUser
}

func read(event agent.Event) (Result, bool) {
	var result Result
	if err := json.Unmarshal(event.State, &result); err != nil || result.Command == "" {
		return Result{}, false
	}

	return result, true
}

func Notice(event agent.Event) (string, bool) {
	result, isRead := read(event)
	if !isRead {
		return "", false
	}

	output := strings.TrimRight(result.Output, "\n")
	hasOutput := strings.TrimSpace(output) != ""
	paragraphs := []string{lede(result, hasOutput), fenced(shellLanguage, prompted(result.Command))}

	if hasOutput {
		paragraphs = append(paragraphs, outputHeading(result), fenced("", output))
	}

	return strings.Join(append(paragraphs, statusNote(result)), "\n\n"), true
}

func lede(result Result, hasOutput bool) string {
	switch {
	case result.IsStoppedByUser && hasOutput:
		return "The user started the following command on the host, then interrupted it before it finished:"
	case result.IsStoppedByUser:
		return "The user started the following command on the host, then interrupted it before it finished or printed anything:"
	case result.StoppedAfter > 0 && hasOutput:
		return "The user started the following command on the host, which was killed at its time limit before it finished:"
	case result.StoppedAfter > 0:
		return "The user started the following command on the host, which was killed at its time limit before it finished or printed anything:"
	case hasOutput:
		return "The user ran the following command on the host:"
	}

	return "The user ran the following command on the host, producing no output:"
}

func outputHeading(result Result) string {
	if result.StoppedAfter > 0 || result.IsStoppedByUser {
		return "Output up to the point it was killed, which may be incomplete:"
	}

	return "Output:"
}

func fenced(language string, body string) string {
	mark := strings.Repeat("`", max(shortestFence, longestBacktickRun(body)+1))

	return mark + language + "\n" + body + "\n" + mark
}

func longestBacktickRun(body string) int {
	longest := 0
	for _, line := range strutil.Lines(body) {
		run := 0
		for run < len(line) && line[run] == '`' {
			run++
		}
		longest = max(longest, run)
	}

	return longest
}

func prompted(command string) string {
	lines := strutil.Lines(strings.TrimRight(command, "\n"))
	for i := range lines {
		if i == 0 {
			lines[i] = commandPrompt + lines[i]
			continue
		}
		lines[i] = continuationPrompt + lines[i]
	}

	return strings.Join(lines, "\n")
}

func statusNote(result Result) string {
	if result.IsStoppedByUser {
		return "Interrupted by the user after " + util.CompactDuration(result.StoppedAfter)
	}

	if result.StoppedAfter > 0 {
		return "Killed at its time limit of " + util.CompactDuration(result.StoppedAfter)
	}

	return "Exit code: " + strconv.Itoa(result.ExitCode)
}
