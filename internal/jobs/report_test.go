package jobs

import (
	"strings"
	"testing"
	"time"
)

var reportedStart = time.Date(2026, time.August, 23, 14, 32, 9, 0, time.UTC)

func endedSnapshot() Snapshot {
	return Snapshot{
		Name:      "build",
		State:     StateComplete,
		StartedAt: reportedStart,
		EndedAt:   reportedStart.Add(3 * time.Second),
	}
}

func TestAnEmptyJobReportMarksTheStatusLine(t *testing.T) {
	for name, output := range map[string]string{
		"empty":      "",
		"whitespace": "  \n\t",
	} {
		t.Run(name, func(t *testing.T) {
			const want = "build: complete after 3s with no output"
			if got := Report(endedSnapshot().DescribeWith(output), output, 0); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestANoOutputMarkerFollowsTheDurationRatherThanTheFailure(t *testing.T) {
	snapshot := endedSnapshot()
	snapshot.State = StateFailed
	snapshot.ExitCode = 2
	snapshot.Run = 2
	snapshot.Failure = "the job could not be respawned: no namespaces left"

	const want = "failed after 3s with no output, exit(2), run 2, the job could not be respawned: no namespaces left"
	if got := snapshot.OutcomeWith(""); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAnOutcomeWithOutputIsTheOrdinaryOutcome(t *testing.T) {
	snapshot := endedSnapshot()
	if got, want := snapshot.OutcomeWith("compiled\n"), snapshot.Outcome(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestANoOutputMarkerPrecedesADroppedOutputNotice(t *testing.T) {
	got := Report(endedSnapshot().DescribeWith(""), "", 1024)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("got lines %q, want marked status and dropped-output notice", lines)
	}
	if lines[0] != "build: complete after 3s with no output" {
		t.Errorf("got status line %q, want the concise marker inline", lines[0])
	}
	if !strings.HasPrefix(lines[1], "note: the oldest ") {
		t.Errorf("got final line %q, want the dropped-output notice", lines[1])
	}
}

func TestJobOutputRemainsBelowTheStatusLine(t *testing.T) {
	const status = "build: complete after 3s"
	const output = "compiled successfully\n"
	want := status + "\ncompiled successfully"
	if got := Report(status, output, 0); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAReportLeavesTheStatusLineAsItWasGiven(t *testing.T) {
	const want = "Job `build` exited: complete."
	if got := Report(want, "", 0); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
