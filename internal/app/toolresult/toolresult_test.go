package toolresult

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/taskrecord"
	"crdx.org/oh/internal/toolresult"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
	"crdx.org/oh/pkg/tasks"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestAnInternalRequestIsParsed(t *testing.T) {
	for name, test := range map[string]struct {
		arguments []string
		want      Request
	}{
		"direct": {arguments: []string{"--view", "oh://tool-result?call=one"}, want: Request{URL: "oh://tool-result?call=one"}},
		"pager":  {arguments: []string{"--view", "oh://tool-result?call=one", "--pager"}, want: Request{URL: "oh://tool-result?call=one", ShouldPage: true}},
	} {
		t.Run(name, func(t *testing.T) {
			request, isRequested, err := ParseRequest(test.arguments)
			if err != nil || !isRequested || request != test.want {
				t.Errorf("got %+v, requested %v, error %v", request, isRequested, err)
			}
		})
	}
}

func TestAnInternalRequestMustHaveItsExactShape(t *testing.T) {
	for _, arguments := range [][]string{
		{"--view"},
		{"--view", "--pager"},
		{"--view", "oh://tool-result?call=one", "--other"},
		{"--view", "oh://tool-result?call=one", "--pager", "extra"},
	} {
		if _, isRequested, err := ParseRequest(arguments); !isRequested || err == nil {
			t.Errorf("arguments %q produced requested %v and error %v", arguments, isRequested, err)
		}
	}
}

func TestAnOrdinaryInvocationIsNotAnInternalRequest(t *testing.T) {
	if _, isRequested, err := ParseRequest([]string{"--help"}); isRequested || err != nil {
		t.Errorf("got requested %v and error %v", isRequested, err)
	}
}

func TestShowWritesSafeToolOutput(t *testing.T) {
	directory, address := toolResultFixture(t)

	var output strings.Builder
	err := show(address, false, directory, &output, nil)
	if err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}
	want := "read\n\n{\n    \"path\": \"notes.txt\"\n}\n\none\ntwo\n"
	if style.Plain(output.String()) != want {
		t.Errorf("got output %q", output.String())
	}
	if strings.Contains(output.String(), "example.test") {
		t.Errorf("unsafe hyperlink survived in %q", output.String())
	}
}

func TestShowWritesTheWholeOutputOfATaskRun(t *testing.T) {
	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	completion := taskrecord.Completion{Snapshot: tasks.Snapshot{
		Invocation: &tasks.Invocation{Arguments: `{"command":"seq 1 9"}`},
		ID:         "count",
		ToolName:   "bash",
		Run:        1,
		State:      tasks.Done,
		EndedAt:    time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC),
		Output:     "1\n2\n3\n4\n5\n6\n7\n8\n9\n",
	}, IsFinal: true}
	conclusion := tasks.Conclusion{Snapshot: completion.Snapshot, IsFinal: true}
	if _, err := writer.Event(taskrecord.FinishedEvent(conclusion)); err != nil {
		t.Fatal(err)
	}
	name := writer.Name()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	var output strings.Builder
	if err := show(toolresult.TaskURL(completion.Reference(name)), false, directory, &output, nil); err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}
	if want := "bash\n\n{\n    \"command\": \"seq 1 9\"\n}\n\n1\n2\n3\n4\n5\n6\n7\n8\n9\n"; style.Plain(output.String()) != want {
		t.Errorf("got output %q, want %q", style.Plain(output.String()), want)
	}
}

func TestPagerReceivesSafeToolOutput(t *testing.T) {
	directory, address := toolResultFixture(t)
	pagedText := ""
	openPager := func(text string) error {
		pagedText = text
		return nil
	}

	var output strings.Builder
	err := show(address, true, directory, &output, openPager)
	if err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}
	if style.Plain(pagedText) != "read\n\n{\n    \"path\": \"notes.txt\"\n}\n\none\ntwo\n" {
		t.Errorf("paged text is %q", pagedText)
	}
	if output.Len() != 0 {
		t.Errorf("standard output contains %q", output.String())
	}
}

func toolResultFixture(t *testing.T) (string, string) {
	t.Helper()

	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{
		Kind:      agent.ToolCallRequestEvent,
		ID:        "call-1",
		Name:      "read",
		Arguments: `{"path":"notes.txt"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{
		Kind:   agent.ToolCallResultEvent,
		ID:     "call-1",
		Name:   "read",
		Status: agent.SuccessStatus,
		Text:   "one\n\x1b]8;;https://example.test\x1b\\two\x1b]8;;\x1b\\\n",
	}); err != nil {
		t.Fatal(err)
	}
	name := writer.Name()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return directory, toolresult.URL(name, "call-1")
}

func TestAResultIsHandedToThePagerWhole(t *testing.T) {
	directory := t.TempDir()
	pagedPath := filepath.Join(directory, "paged")
	if err := os.WriteFile(filepath.Join(directory, "less"), []byte("#!/bin/sh\ncat > \"$PAGED\"\n"), 0o700); err != nil { //nolint:gosec // a pager this test runs
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PAGED", pagedPath)

	if err := page("first line\nsecond line\n"); err != nil {
		t.Fatal(err)
	}

	paged, err := os.ReadFile(pagedPath) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if string(paged) != "first line\nsecond line\n" {
		t.Errorf("the pager was handed %q", paged)
	}
}

func TestAPagerThatCannotRunIsReported(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	if err := page("text"); err == nil || !strings.Contains(err.Error(), "pager failed") {
		t.Errorf("got %v, want the pager's failure reported", err)
	}
}

func TestShowWritesTheCurrentOutputOfARunningTask(t *testing.T) {
	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Close() }()
	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "start"}); err != nil {
		t.Fatal(err)
	}

	snapshot := tasks.Snapshot{
		Invocation: &tasks.Invocation{Arguments: `{"command":"make build"}`},
		ID:         "build",
		ToolName:   "bash",
		Run:        1,
		State:      tasks.Running,
		Output:     "compiling package two\n",
	}
	if err := taskrecord.WriteCurrent(session.Dir(directory, writer.Name()), []tasks.Snapshot{snapshot}); err != nil {
		t.Fatal(err)
	}
	address := toolresult.TaskURL(taskrecord.Completion{Snapshot: snapshot}.Reference(writer.Name()))
	var output strings.Builder
	if err := show(address, false, directory, &output, nil); err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}
	want := "bash\n\n{\n    \"command\": \"make build\"\n}\n\ncompiling package two\n"
	if got := style.Plain(output.String()); got != want {
		t.Errorf("got output %q, want %q", got, want)
	}

	snapshot.Output += "compiling package three\n"
	if err := taskrecord.WriteCurrent(session.Dir(directory, writer.Name()), []tasks.Snapshot{snapshot}); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := show(address, false, directory, &output, nil); err != nil {
		t.Fatalf("unexpected updated run error: %v", err)
	}
	want += "compiling package three\n"
	if got := style.Plain(output.String()); got != want {
		t.Errorf("updated output is %q, want %q", got, want)
	}
}

func TestArgumentsShowMarkupEscapesAsTheCharactersTheyStandFor(t *testing.T) {
	for escaped, want := range map[string]string{
		`"a \u0026\u0026 b"`: `"a && b"`,
		`"\u003cdone\u003e"`: `"<done>"`,
		`"\\u0026"`:          `"\\u0026"`,
		`"\\\u0026"`:         `"\\&"`,
		`"\u00e9 \n \u2028"`: `"\u00e9 \n \u2028"`,
		`"ends with \u002`:   `"ends with \u002`,
	} {
		if got := unescapeMarkup(escaped); got != want {
			t.Errorf("%s: got %s, want %s", escaped, got, want)
		}
	}
}
