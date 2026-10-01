package hostcommand_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/hostcommand"
	"crdx.org/oh/pkg/agent"
)

func TestACommandRunsInTheDirectoryItWasGiven(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "readme"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := hostcommand.Run(t.Context(), directory, "ls", &hostcommand.Output{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Command != "ls" || result.Output != "readme\n" || result.ExitCode != 0 {
		t.Errorf("got result %+v", result)
	}
}

func TestACommandReportsItsExitCodeAndItsStandardError(t *testing.T) {
	result, err := hostcommand.Run(t.Context(), t.TempDir(), "echo out; echo problem >&2; exit 3", &hostcommand.Output{})
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
	result, err := hostcommand.Run(t.Context(), t.TempDir(), "cat", &hostcommand.Output{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "" || result.ExitCode != 0 {
		t.Errorf("got result %+v", result)
	}
}

func TestACommandLeadsASessionOfItsOwnBesideATerminalOfItsOwn(t *testing.T) {
	result, err := hostcommand.Run(t.Context(), t.TempDir(), `read -ra fields < /proc/$$/stat; echo "${fields[0]} ${fields[5]} ${fields[6]}"`, &hostcommand.Output{})
	if err != nil {
		t.Fatal(err)
	}

	own, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		t.Fatal(err)
	}
	ownTerminal := strings.Fields(string(own[bytes.LastIndexByte(own, ')')+2:]))[4]

	fields := strings.Fields(result.Output)
	if len(fields) != 3 || fields[0] != fields[1] {
		t.Errorf("got process, session and terminal %q, want the shell to lead its own session", result.Output)
	}
	if len(fields) == 3 && (fields[2] == "0" || fields[2] == ownTerminal) {
		t.Errorf("got terminal %s beside the harness's own %s, want a terminal of its own", fields[2], ownTerminal)
	}
}

func TestStoppingACommandKillsEverythingItStarted(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "child")
	stopContext, stop := context.WithCancel(t.Context())

	go func() {
		for {
			if _, err := os.Stat(childPath); err == nil {
				stop()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	startedAt := time.Now()
	result, err := hostcommand.Run(stopContext, directory, "sleep 60 & echo $! > child.tmp; mv child.tmp child; wait", &hostcommand.Output{})
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(startedAt); took > 500*time.Millisecond {
		t.Errorf("took %s to stop, want the whole group killed at once", took)
	}
	if !result.IsStoppedByUser || result.StoppedAfter >= hostcommand.TimeLimit {
		t.Errorf("got result %+v, want it stopped by the user", result)
	}

	directoryRoot, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = directoryRoot.Close() })
	child, err := directoryRoot.ReadFile("child")
	if err != nil {
		t.Fatal(err)
	}
	childID, err := strconv.Atoi(strings.TrimSpace(string(child)))
	if err != nil {
		t.Fatal(err)
	}

	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if isDead(childID) {
			return
		}
	}
	t.Errorf("the background child %d outlived the stop", childID)
}

func isDead(processID int) bool {
	processes, err := os.OpenRoot("/proc")
	if err != nil {
		return false
	}
	defer func() { _ = processes.Close() }()

	status, err := processes.ReadFile(filepath.Join(strconv.Itoa(processID), "stat"))
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	fields := strings.Fields(string(status))

	return err == nil && len(fields) > 2 && fields[2] == "Z"
}

func TestAnInterruptedCommandTellsTheModelWhatItPrintedBeforeItWasStopped(t *testing.T) {
	directory := t.TempDir()
	stopContext, stop := context.WithCancel(t.Context())

	go func() {
		for {
			if _, err := os.Stat(filepath.Join(directory, "begun")); err == nil {
				stop()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	result, err := hostcommand.Run(stopContext, directory, "echo first; echo second >&2; touch begun; sleep 60; echo never", &hostcommand.Output{})
	if err != nil {
		t.Fatal(err)
	}
	result.StoppedAfter = 4 * time.Second

	notice, isSaid := hostcommand.Notice(hostcommand.RanEvent(result))
	if !isSaid {
		t.Fatal("expected the notice to be said")
	}

	want := strings.Join([]string{
		"The user started the following command on the host, then interrupted it before it finished:",
		"```bash\n$ echo first; echo second >&2; touch begun; sleep 60; echo never\n```",
		"Output up to the point it was killed, which may be incomplete:",
		"```\nfirst\nsecond\n```",
		"Interrupted by the user after 4s, so it has no exit code. Anything it had not yet done was not done.",
	}, "\n\n")
	if notice != want {
		t.Errorf("got notice\n%s\nwant\n%s", notice, want)
	}
}

func TestANoticeOpensBySayingWhetherTheCommandFinished(t *testing.T) {
	tests := map[string]struct {
		result hostcommand.Result
		want   string
	}{
		"finished": {
			result: hostcommand.Result{Command: "ls", Output: "readme\n"},
			want:   "The user ran the following command on the host:",
		},
		"finished silently": {
			result: hostcommand.Result{Command: "true"},
			want:   "The user ran the following command on the host, producing no output:",
		},
		"interrupted": {
			result: hostcommand.Result{Command: "git push", Output: "Counting\n", IsStoppedByUser: true},
			want:   "The user started the following command on the host, then interrupted it before it finished:",
		},
		"interrupted silently": {
			result: hostcommand.Result{Command: "git push", IsStoppedByUser: true},
			want:   "The user started the following command on the host, then interrupted it before it finished or printed anything:",
		},
		"killed at its limit": {
			result: hostcommand.Result{Command: "make", Output: "cc\n", StoppedAfter: hostcommand.TimeLimit},
			want:   "The user started the following command on the host, which was killed at its time limit before it finished:",
		},
		"killed silently at its limit": {
			result: hostcommand.Result{Command: "sleep 600", StoppedAfter: hostcommand.TimeLimit},
			want:   "The user started the following command on the host, which was killed at its time limit before it finished or printed anything:",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			notice, isSaid := hostcommand.Notice(hostcommand.RanEvent(test.result))
			if !isSaid {
				t.Fatal("expected the notice to be said")
			}
			if !strings.HasPrefix(notice, test.want+"\n\n") {
				t.Errorf("got notice %q, want it to open with %q", notice, test.want)
			}
		})
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
			want:   "Killed at its time limit of 30s, so it has no exit code. Anything it had not yet done was not done.",
		},
		"stopped by the user": {
			result: hostcommand.Result{Command: "git push", StoppedAfter: 4 * time.Second, IsStoppedByUser: true},
			want:   "Interrupted by the user after 4s, so it has no exit code. Anything it had not yet done was not done.",
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

func TestOnlyACommandTheUserStoppedIsStoppedByTheUser(t *testing.T) {
	for name, test := range map[string]struct {
		event agent.Event
		want  bool
	}{
		"stopped by the user": {
			event: hostcommand.RanEvent(hostcommand.Result{Command: "git push", IsStoppedByUser: true}),
			want:  true,
		},
		"stopped at its limit": {
			event: hostcommand.RanEvent(hostcommand.Result{Command: "sleep 600", StoppedAfter: hostcommand.TimeLimit}),
		},
		"no state": {event: agent.Event{Kind: hostcommand.Ran}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := hostcommand.IsStoppedByUser(test.event); got != test.want {
				t.Errorf("got %t, want %t", got, test.want)
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

func TestWhatACommandPrintsCanBeReadWhileItRuns(t *testing.T) {
	directory := t.TempDir()
	stopContext, stop := context.WithCancel(t.Context())
	output := &hostcommand.Output{}
	seen := make(chan string, 1)

	go func() {
		for {
			if line := output.LatestLine(); line != "" {
				seen <- line
				stop()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	if _, err := hostcommand.Run(stopContext, directory, "echo 'Place your finger on the reader' >&2; sleep 60", output); err != nil {
		t.Fatal(err)
	}
	if line := <-seen; line != "Place your finger on the reader" {
		t.Errorf("got latest line %q", line)
	}
}

func TestTheLatestLineIsTheLastOneThatSaysSomething(t *testing.T) {
	tests := map[string]struct {
		written string
		want    string
	}{
		"nothing":               {written: "", want: ""},
		"only blanks":           {written: "\n \n\t\n", want: ""},
		"unfinished line":       {written: "first\nsecond", want: "second"},
		"trailing blank lines":  {written: "first\nsecond\n\n  \n", want: "second"},
		"overwritten progress":  {written: "fetching 10%\rfetching 90%\r", want: "fetching 90%"},
		"carriage return pairs": {written: "first\r\nsecond\r\n", want: "second"},
		"escape sequences":      {written: "\x1b[1;32mready\x1b[0m\n", want: "ready"},
		"padded":                {written: "   indented   \n", want: "indented"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			output := &hostcommand.Output{}
			if _, err := output.Write([]byte(test.written)); err != nil {
				t.Fatal(err)
			}
			if line := output.LatestLine(); line != test.want {
				t.Errorf("got %q, want %q", line, test.want)
			}
		})
	}
}

func TestSettledOutputKeepsOnlyWhatEachLineWasLastOverwrittenWith(t *testing.T) {
	tests := map[string]struct {
		written string
		want    string
	}{
		"plain lines":           {written: "first\nsecond\n", want: "first\nsecond\n"},
		"overwritten progress":  {written: "Receiving 10%\rReceiving 50%\rReceiving 90%\rdone\n", want: "done\n"},
		"progress left showing": {written: "cloning\nReceiving 10%\rReceiving 90%\r", want: "cloning\nReceiving 90%"},
		"carriage return pairs": {written: "first\r\nsecond\r\n", want: "first\nsecond\n"},
		"unfinished line":       {written: "first\nsecond", want: "first\nsecond"},
		"nothing":               {written: "", want: ""},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			output := &hostcommand.Output{}
			if _, err := output.Write([]byte(test.written)); err != nil {
				t.Fatal(err)
			}
			if settled := output.Settled(); settled != test.want {
				t.Errorf("got %q, want %q", settled, test.want)
			}
		})
	}
}

func TestACommandReportsOnlyTheLastStateOfItsProgress(t *testing.T) {
	result, err := hostcommand.Run(t.Context(), t.TempDir(), `printf 'Receiving 10%%\rReceiving 90%%\rdone\n'`, &hostcommand.Output{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "done\n" {
		t.Errorf("got output %q", result.Output)
	}
}
