package bash

import (
	"errors"
	"strings"
	"syscall"
	"testing"
	"time"

	"crdx.org/io/internal/sandbox"

	"crdx.org/io/pkg/tool"
)

func TestAnUnfinishedCommandKeepsItsOutputAndSaysWhyItEnded(t *testing.T) {
	stopped := errors.New("the command was stopped after 12s because the user pressed escape")

	tests := map[string]struct {
		output string
		err    error
		want   string
	}{
		"output and a stop": {
			output: "compiling\n",
			err:    stopped,
			want:   "compiling\nnote: the command was stopped after 12s because the user pressed escape.",
		},
		"trailing blank lines": {
			output: "compiling\n\n\n",
			err:    stopped,
			want:   "compiling\nnote: the command was stopped after 12s because the user pressed escape.",
		},
		"a timeout": {
			output: "compiling\n",
			err:    errors.New("the command did not finish within 2m0s"),
			want:   "compiling\nnote: the command did not finish within 2m0s.",
		},
		"nothing written": {
			output: "",
			err:    stopped,
			want:   "",
		},
		"nothing but whitespace": {
			output: " \n\t\n",
			err:    stopped,
			want:   "",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := unfinished(test.output, test.err); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestWhatACommandProducedIsMeasured(t *testing.T) {
	metrics := tool.ToolCallMetrics{Kind: tool.MetricResources}

	if got := measured("one\ntwo\n", &metrics); got != "one\ntwo\n" {
		t.Errorf("got %q, want the text back unchanged", got)
	}
	if metrics.Lines != 2 || metrics.Bytes != 8 || metrics.TotalBytes != 8 {
		t.Errorf("got %+v, want 2 lines and 8 bytes", metrics)
	}
	if metrics.Kind != tool.MetricResources {
		t.Errorf("got kind %q, want what a command reports", metrics.Kind)
	}
}

func TestNothingProducedIsMeasuredAsNothing(t *testing.T) {
	metrics := tool.ToolCallMetrics{Kind: tool.MetricResources}

	measured("", &metrics)

	if metrics.Lines != 0 || metrics.Bytes != 0 {
		t.Errorf("got %+v, want an empty report to measure as empty", metrics)
	}
}

func killTestPolicy() sandbox.Policy {
	return sandbox.Policy{
		Timeout:     5 * time.Minute,
		MaxCPUTime:  time.Hour,
		MaxFileSize: 1024 << 20,
		Write:       []string{"/workspace"},
	}
}

func TestACommandKilledForItsProcessorTimeIsToldTheLimitAndWhatItUsed(t *testing.T) {
	result := sandbox.Result{
		ExitCode: 137,
		CPUTime:  90 * time.Minute,
		Output:   "error: recipe `lint1` was terminated on line 104 by signal 9",
	}

	got := report(result, killTestPolicy())

	for _, want := range []string{
		"killed by SIGKILL",
		"each process 1h of processor time",
		"counted across every thread it runs",
		"after 5m of wall clock",
		"used 1h30m of processor time between them",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("got %q, want it to mention %q", got, want)
		}
	}
}

func TestACommandKilledForWritingTooMuchIsToldTheFileLimit(t *testing.T) {
	result := sandbox.Result{ExitCode: 153, Output: "dd: writing 'big': File size limit exceeded"}

	got := report(result, killTestPolicy())

	if !strings.Contains(got, "killed by SIGXFSZ") || !strings.Contains(got, "no more than 1G") {
		t.Errorf("got %q, want the signal and the file size limit named", got)
	}
	if strings.Contains(got, "using too much") {
		t.Errorf("got %q, want the vague note replaced by the exact one", got)
	}
}

func TestAKillIsReportedEvenWhereTheOutputLooksLikeADenial(t *testing.T) {
	result := sandbox.Result{ExitCode: 137, Output: "cp: cannot open 'x': Permission denied"}

	if got := report(result, killTestPolicy()); !strings.Contains(got, "killed by SIGKILL") {
		t.Errorf("got %q, want a kill reported whatever else the output says", got)
	}
}

func TestAnUnkilledFailureIsAccusedOfNothing(t *testing.T) {
	for name, code := range map[string]int{
		"an ordinary failure":       1,
		"a status of the boundary":  128,
		"a status above any signal": 255,
	} {
		got := report(sandbox.Result{ExitCode: code, Output: "make: *** [all] Error 1"}, killTestPolicy())
		if strings.Contains(got, "killed by") {
			t.Errorf("%s: got %q, want no kill claimed", name, got)
		}
	}
}

func TestASignalTheCommandItselfDiedOfIsReportedWithoutHedging(t *testing.T) {
	result := sandbox.Result{
		ExitCode: -1,
		Signal:   syscall.SIGKILL,
		CPUTime:  90 * time.Minute,
	}

	got := report(result, killTestPolicy())

	if !strings.Contains(got, "note: the command was killed by SIGKILL.") {
		t.Errorf("got %q, want an observed kill stated outright", got)
	}
	if strings.Contains(got, "the shell reports") {
		t.Errorf("got %q, want no hedge when the kill was seen rather than inferred", got)
	}
	if !strings.Contains(got, "each process 1h of processor time") {
		t.Errorf("got %q, want the processor limit named", got)
	}
}

func TestASignalOnlyTheShellSawIsReportedAsSuch(t *testing.T) {
	result := sandbox.Result{ExitCode: 137, Output: "Killed"}

	got := report(result, killTestPolicy())

	if !strings.Contains(got, "note: the shell reports that a process was killed by SIGKILL.") {
		t.Errorf("got %q, want an inferred kill attributed to the shell", got)
	}
}

func TestAnObservedSignalIsPreferredToTheExitStatus(t *testing.T) {
	result := sandbox.Result{ExitCode: 139, Signal: syscall.SIGXFSZ}

	got := report(result, killTestPolicy())

	if !strings.Contains(got, "killed by SIGXFSZ") || strings.Contains(got, "SIGSEGV") {
		t.Errorf("got %q, want what was seen rather than what the status implies", got)
	}
}

func TestAKillUnderNoProcessorLimitNamesTheSignalAlone(t *testing.T) {
	result := sandbox.Result{ExitCode: 137, Output: "Killed"}

	got := report(result, sandbox.Policy{Timeout: time.Minute})

	if !strings.Contains(got, "killed by SIGKILL") {
		t.Errorf("got %q, want the signal named", got)
	}
	if strings.Contains(got, "processor time") {
		t.Errorf("got %q, want no limit named when the policy sets no limit", got)
	}
}
