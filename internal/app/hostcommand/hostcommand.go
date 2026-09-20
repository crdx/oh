package hostcommand

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"crdx.org/oh/internal/util"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/agent"
)

const Ran agent.Kind = "host_command_ran"

const (
	timeLimit = 30 * time.Second
	waitDelay = time.Second

	shortestFence      = 3
	shellLanguage      = "bash"
	commandPrompt      = "$ "
	continuationPrompt = "> "
)

type Result struct {
	Command      string        `json:"command"`
	Output       string        `json:"output,omitempty"`
	ExitCode     int           `json:"exit_code,omitempty"`
	StoppedAfter time.Duration `json:"stopped_after,omitempty"`
}

func Run(directory string, command string) (Result, error) {
	runContext, cancel := context.WithTimeout(context.Background(), timeLimit)
	defer cancel()

	//nolint:gosec // the person at the keyboard typed this command themselves
	shell := exec.CommandContext(runContext, "bash", "-c", command)
	shell.Dir = directory
	shell.Stdin = nil
	shell.WaitDelay = waitDelay

	output, err := shell.CombinedOutput()
	result := Result{Command: command, Output: string(output)}

	if errors.Is(runContext.Err(), context.DeadlineExceeded) {
		result.StoppedAfter = timeLimit

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

func RanEvent(result Result) agent.Event {
	encodedResult, err := json.Marshal(result)
	if err != nil {
		return agent.Event{}
	}

	return agent.Event{Kind: Ran, Name: result.Command, State: encodedResult}
}

func Notice(event agent.Event) (string, bool) {
	var result Result
	if err := json.Unmarshal(event.State, &result); err != nil || result.Command == "" {
		return "", false
	}

	output := strings.TrimRight(result.Output, "\n")
	lede := "The user ran the following command on the host:"
	if strings.TrimSpace(output) == "" {
		lede = "The user ran the following command on the host, producing no output:"
	}

	paragraphs := []string{lede, fenced(shellLanguage, prompted(result.Command))}

	if strings.TrimSpace(output) != "" {
		paragraphs = append(paragraphs, "Output:", fenced("", output))
	}

	return strings.Join(append(paragraphs, statusNote(result)), "\n\n"), true
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
	if result.StoppedAfter > 0 {
		return "Stopped after its limit of " + util.CompactDuration(result.StoppedAfter) + "."
	}

	return "Exit code: " + strconv.Itoa(result.ExitCode)
}
