package sandbox

import (
	"fmt"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"crdx.org/oh/internal/util"
)

const signalled = 128

var fileSizeOverruns = []string{
	"File size limit exceeded",
}

var processorOverruns = []string{
	"Cpu time limit exceeded",
}

var openFileOverruns = []string{
	"Too many open files",
}

var processOverruns = []string{
	"fork: retry",
	"fork: Resource temporarily unavailable",
	"Cannot fork",
	"pthread_create failed",
	"failed to create new OS thread",
}

func KillNotice(result Result, policy Policy) string {
	signal, isObserved := endingSignal(result)

	name := unix.SignalName(signal)
	if name == "" {
		return ""
	}

	openingNote := fmt.Sprintf("note: the shell reports that a process was killed by %s.", name)
	if isObserved {
		openingNote = fmt.Sprintf("note: the command was killed by %s.", name)
	}

	lines := []string{openingNote}

	if signal == syscall.SIGKILL || signal == syscall.SIGXCPU {
		lines = append(lines, processorLimit(result, policy)...)
	}
	if signal == syscall.SIGXFSZ {
		lines = append(lines, fileSizeLimit(policy)...)
	}

	return strings.Join(lines, "\n")
}

func OverrunNotice(result Result, policy Policy) string {
	var lines []string

	switch {
	case matchesAny(result.Output, fileSizeOverruns):
		lines = fileSizeLimit(policy)
	case matchesAny(result.Output, processorOverruns):
		lines = processorLimit(result, policy)
	case matchesAny(result.Output, openFileOverruns):
		lines = openFileLimit(policy)
	case matchesAny(result.Output, processOverruns):
		lines = processLimit(policy)
	default:
		return ""
	}

	openingNote := "note: the sandbox stopped this command for using too much."

	return strings.Join(append([]string{openingNote}, lines...), "\n")
}

func endingSignal(result Result) (syscall.Signal, bool) {
	if result.Signal != 0 {
		return result.Signal, true
	}

	if result.ExitCode > signalled {
		return syscall.Signal(result.ExitCode - signalled), false
	}

	return 0, false
}

func processorLimit(result Result, policy Policy) []string {
	if policy.MaxCPUTime <= 0 {
		return nil
	}

	return []string{
		fmt.Sprintf(
			"the sandbox gives each process %s of processor time, counted across every thread it runs,"+
				" and stops the whole command after %s of wall clock.",
			util.CompactDuration(policy.MaxCPUTime), util.CompactDuration(policy.Timeout),
		),
		fmt.Sprintf(
			"every process this command started used %s of processor time between them.",
			util.CompactDuration(result.CPUTime),
		),
	}
}

func fileSizeLimit(policy Policy) []string {
	if policy.MaxFileSize <= 0 {
		return nil
	}

	return []string{fmt.Sprintf(
		"the sandbox lets a command write no more than %s to a single file.",
		util.FormatBytes(policy.MaxFileSize, 3),
	)}
}

func openFileLimit(policy Policy) []string {
	if policy.MaxOpenFiles <= 0 {
		return nil
	}

	return []string{fmt.Sprintf(
		"the sandbox lets each process hold no more than %d files open at once.",
		policy.MaxOpenFiles,
	)}
}

func processLimit(policy Policy) []string {
	if policy.MaxProcesses <= 0 {
		return nil
	}

	return []string{fmt.Sprintf(
		"the sandbox lets no more than %d tasks run at once across everything the command started,"+
			" counting each thread as one of them.",
		policy.MaxProcesses,
	)}
}

func matchesAny(output string, wordings []string) bool {
	for _, wording := range wordings {
		if strings.Contains(output, wording) {
			return true
		}
	}

	return false
}
