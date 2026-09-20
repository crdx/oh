package hostcommand_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/io/internal/app/hostcommand"
	"crdx.org/io/pkg/agent"
)

func TestACommandRunsInTheDirectoryItWasGiven(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "readme"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := hostcommand.Run(directory, "ls")
	if err != nil {
		t.Fatal(err)
	}
	if result.Command != "ls" || result.Output != "readme\n" || result.ExitCode != 0 {
		t.Errorf("got result %+v", result)
	}
}

func TestACommandReportsItsExitCodeAndItsStandardError(t *testing.T) {
	result, err := hostcommand.Run(t.TempDir(), "echo out; echo problem >&2; exit 3")
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 3 {
		t.Errorf("got exit code %d, want 3", result.ExitCode)
	}
	if result.Output != "out\nproblem\n" {
		t.Errorf("got output %q", result.Output)
	}
}

func TestACommandCannotReadTheKeyboard(t *testing.T) {
	result, err := hostcommand.Run(t.TempDir(), "cat")
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "" || result.ExitCode != 0 {
		t.Errorf("got result %+v", result)
	}
}

func TestANoticeQuotesTheCommandAndWhatItPrinted(t *testing.T) {
	notice, isSaid := hostcommand.Notice(hostcommand.RanEvent(hostcommand.Result{
		Command: "git status --short",
		Output:  " M README.md\n",
	}))
	if !isSaid {
		t.Fatal("expected the notice to be said")
	}

	want := strings.Join([]string{
		"The user ran the following command on the host:",
		"",
		"```bash",
		"$ git status --short",
		"```",
		"",
		"Output:",
		"",
		"```",
		" M README.md",
		"```",
		"",
		"Exit code: 0",
	}, "\n")
	if notice != want {
		t.Errorf("got notice %q, want %q", notice, want)
	}
}

func TestANoticeSaysSoWhenACommandPrintedNothing(t *testing.T) {
	notice, isSaid := hostcommand.Notice(hostcommand.RanEvent(hostcommand.Result{Command: "touch notes.txt"}))
	if !isSaid {
		t.Fatal("expected the notice to be said")
	}

	want := strings.Join([]string{
		"The user ran the following command on the host, producing no output:",
		"",
		"```bash",
		"$ touch notes.txt",
		"```",
		"",
		"Exit code: 0",
	}, "\n")
	if notice != want {
		t.Errorf("got notice %q, want %q", notice, want)
	}
}

func TestANoticeLengthensAFenceTheOutputWouldClose(t *testing.T) {
	notice, isSaid := hostcommand.Notice(hostcommand.RanEvent(hostcommand.Result{
		Command: "cat README.md",
		Output:  "```go\nfmt.Println()\n```\n",
	}))
	if !isSaid {
		t.Fatal("expected the notice to be said")
	}

	want := strings.Join([]string{
		"````",
		"```go",
		"fmt.Println()",
		"```",
		"````",
	}, "\n")
	if !strings.Contains(notice, want) {
		t.Errorf("got notice %q, want it to hold %q", notice, want)
	}
}

func TestANoticeGivesAMultiLineCommandAContinuationPrompt(t *testing.T) {
	notice, isSaid := hostcommand.Notice(hostcommand.RanEvent(hostcommand.Result{
		Command: "cat <<EOF\nhello\nEOF\n",
		Output:  "hello\n",
	}))
	if !isSaid {
		t.Fatal("expected the notice to be said")
	}

	want := strings.Join([]string{
		"```bash",
		"$ cat <<EOF",
		"> hello",
		"> EOF",
		"```",
	}, "\n")
	if !strings.Contains(notice, want) {
		t.Errorf("got notice %q, want it to hold %q", notice, want)
	}
}

func TestANoticeNamesHowACommandEnded(t *testing.T) {
	tests := map[string]struct {
		result hostcommand.Result
		want   string
	}{
		"succeeded": {
			result: hostcommand.Result{Command: "true"},
			want:   "Exit code: 0",
		},
		"non-zero exit": {
			result: hostcommand.Result{Command: "false", Output: "no\n", ExitCode: 1},
			want:   "Exit code: 1",
		},
		"stopped at its limit": {
			result: hostcommand.Result{Command: "sleep 600", StoppedAfter: 30 * time.Second},
			want:   "Stopped after its limit of 30s.",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			notice, isSaid := hostcommand.Notice(hostcommand.RanEvent(test.result))
			if !isSaid {
				t.Fatal("expected the notice to be said")
			}
			if !strings.HasSuffix(notice, test.want) {
				t.Errorf("got notice %q, want it to end with %q", notice, test.want)
			}
		})
	}
}

func TestAnEventWithoutACommandSaysNothing(t *testing.T) {
	for name, event := range map[string]agent.Event{
		"no command": hostcommand.RanEvent(hostcommand.Result{Output: "orphaned"}),
		"no state":   {Kind: hostcommand.Ran},
	} {
		t.Run(name, func(t *testing.T) {
			if notice, isSaid := hostcommand.Notice(event); isSaid {
				t.Errorf("got notice %q", notice)
			}
		})
	}
}

func TestAnEventCarriesTheCommandAsItsName(t *testing.T) {
	event := hostcommand.RanEvent(hostcommand.Result{Command: "ls", Output: "readme\n"})
	if event.Kind != hostcommand.Ran || event.Name != "ls" {
		t.Errorf("got event %+v", event)
	}
}
