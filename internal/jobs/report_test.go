package jobs

import (
	"strings"
	"testing"
)

func TestAnEmptyJobReportMarksTheStatusLine(t *testing.T) {
	for name, output := range map[string]string{
		"empty":      "",
		"whitespace": "  \n\t",
	} {
		t.Run(name, func(t *testing.T) {
			const status = "build: complete after 3s"
			want := status + " with no output"
			if got := Report(status, output, 0); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestANoOutputMarkerPrecedesADroppedOutputNotice(t *testing.T) {
	got := Report("build: complete", "", 1024)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("got lines %q, want marked status and dropped-output notice", lines)
	}
	if lines[0] != "build: complete with no output" {
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

func TestANoOutputMarkerStaysInsideStatusPunctuation(t *testing.T) {
	want := "Job `build` exited: complete with no output."
	if got := Report("Job `build` exited: complete.", "", 0); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
