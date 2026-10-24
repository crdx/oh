package toolresult

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/toolresult"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/bash"
	"crdx.org/oh/pkg/toolbox/edit"
	"crdx.org/oh/pkg/toolbox/fetch"
	"crdx.org/oh/pkg/toolbox/find"
	"crdx.org/oh/pkg/toolbox/grep"
	"crdx.org/oh/pkg/toolbox/lookup"
	"crdx.org/oh/pkg/toolbox/ls"
	"crdx.org/oh/pkg/toolbox/read"
	"crdx.org/oh/pkg/toolbox/task"
	"crdx.org/oh/pkg/toolbox/title"
	"crdx.org/oh/pkg/toolbox/write"
)

func TestGoldenToolResultsRenderForTheUser(t *testing.T) {
	cases := []struct {
		name     string
		exchange toolresult.Exchange
	}{
		{
			name: "read source range",
			exchange: resultExchange("read", read.Args{Path: "cmd/main.go", Offset: 8, Limit: 4}, agent.SuccessStatus,
				"package main\n\nfunc main() {\n\trun()\n}\n"),
		},
		{
			name: "read image",
			exchange: resultExchange("read", read.Args{Path: "shot.png"}, agent.SuccessStatus,
				"image/png image (48217 bytes)"),
		},
		{
			name: "read failure",
			exchange: resultExchange("read", read.Args{Path: "missing.go"}, agent.ErrorStatus,
				"missing.go: no such file or directory"),
		},
		{
			name: "call stopped before running",
			exchange: resultExchange("read", read.Args{Path: "main.go"}, agent.CancelledStatus,
				"the call stopped because the user pressed escape"),
		},
		{
			name: "read stopped",
			exchange: resultExchange("read", read.Args{Path: "main.go"}, agent.CancelledStatus,
				"the read stopped because the user pressed escape"),
		},
		{
			name: "write file",
			exchange: resultExchange("write", write.Args{Path: "config.yaml", Content: "name: dove\nenabled: true\n"}, agent.SuccessStatus,
				"wrote 25B to config.yaml"),
		},
		{
			name: "write failure",
			exchange: resultExchange("write", write.Args{Path: "config.yaml", Content: "enabled: false\n"}, agent.ErrorStatus,
				"the filesystem is read-only"),
		},
		{
			name: "write stopped",
			exchange: resultExchange("write", write.Args{Path: "config.yaml", Content: "enabled: false\n"}, agent.CancelledStatus,
				"the write stopped because the user pressed escape"),
		},
		{
			name: "write unread file",
			exchange: resultExchange("write", write.Args{Path: "config.yaml", Content: "enabled: false\n"}, agent.ErrorStatus,
				file.ErrNotRead.Error()),
		},
		{
			name: "write file changed since read",
			exchange: resultExchange("write", write.Args{Path: "config.yaml", Content: "enabled: false\n"}, agent.ErrorStatus,
				file.ErrChangedSinceRead.Error()),
		},
		{
			name: "edit with context",
			exchange: resultExchange("edit", edit.Args{
				Path:    "main.go",
				OldText: "one\ntwo\nthree\nfour\noldCall()\nsix\nseven\neight\nnine\n",
				NewText: "one\ntwo\nthree\nfour\nnewCall()\nsix\nseven\neight\nnine\n",
			}, agent.SuccessStatus, "edited main.go"),
		},
		{
			name: "edit failure",
			exchange: resultExchange("edit", edit.Args{Path: "main.go", OldText: "old", NewText: "new"}, agent.ErrorStatus,
				"old_text not found"),
		},
		{
			name: "edit stopped",
			exchange: resultExchange("edit", edit.Args{Path: "main.go", OldText: "old", NewText: "new"}, agent.CancelledStatus,
				"the edit stopped because the user pressed escape"),
		},
		{
			name: "shell success",
			exchange: resultExchange("bash", bash.Args{Command: "go test ./...\nprintf 'done\\n'"}, agent.SuccessStatus,
				"ok  crdx.org/oh\ndone\n"),
		},
		{
			name: "shell failure",
			exchange: resultExchange("bash", bash.Args{Command: "just check"}, agent.ErrorStatus,
				"lint1  ✗\nmain.go:12: undefined: run\n"),
		},
		{
			name: "shell approval timed out",
			exchange: resultExchange("bash", bash.Args{Command: "curl example.com", Network: "host"}, agent.ErrorStatus,
				"approval timed out after 1m; command did not run"),
		},
		{
			name: "shell approval unavailable",
			exchange: resultExchange("bash", bash.Args{Command: "curl example.com", Network: "host"}, agent.ErrorStatus,
				"approval unavailable; command did not run"),
		},
		{
			name:     "shell without output",
			exchange: resultExchange("bash", bash.Args{Command: "true"}, agent.SuccessStatus, ""),
		},
		{
			name: "shell cancelled",
			exchange: resultExchange("bash", bash.Args{Command: "sleep 30"}, agent.CancelledStatus,
				"the command stopped because the user pressed escape"),
		},
		{
			name: "list directory",
			exchange: resultExchange("ls", ls.Args{Path: "cmd"}, agent.SuccessStatus,
				"oh/\nsimple/\n"),
		},
		{
			name:     "empty directory",
			exchange: resultExchange("ls", ls.Args{Path: "empty"}, agent.SuccessStatus, "(empty)"),
		},
		{
			name: "listing stopped",
			exchange: resultExchange("ls", ls.Args{Path: "cmd"}, agent.CancelledStatus,
				"the listing stopped because the user pressed escape"),
		},
		{
			name: "find files",
			exchange: resultExchange("find", find.Args{Pattern: "**/*.go", Path: "cmd"}, agent.SuccessStatus,
				"cmd/oh/main.go\ncmd/oh/ctl/ctl.go\n"),
		},
		{
			name:     "find without matches",
			exchange: resultExchange("find", find.Args{Pattern: "**/*.cobol"}, agent.SuccessStatus, "(no matches)"),
		},
		{
			name: "search stopped",
			exchange: resultExchange("find", find.Args{Pattern: "**/*.go"}, agent.CancelledStatus,
				"the search stopped because the user pressed escape"),
		},
		{
			name: "grep source",
			exchange: resultExchange("grep", grep.Args{Pattern: "func (Run|Open)", Path: "cmd", Glob: "**/*.go"}, agent.SuccessStatus,
				"cmd/oh/main.go:12:func Run() error {\ncmd/oh/ctl/open.go:8:func Open() {\n"),
		},
		{
			name:     "grep without matches",
			exchange: resultExchange("grep", grep.Args{Pattern: "COBOL"}, agent.SuccessStatus, "(no matches)"),
		},
		{
			name: "grep failure",
			exchange: resultExchange("grep", grep.Args{Pattern: "[", Path: "cmd"}, agent.ErrorStatus,
				"regex parse error: unclosed character class"),
		},
		{
			name: "grep could not run",
			exchange: resultExchange("grep", grep.Args{Pattern: "hello"}, agent.ErrorStatus,
				"shim: Permission denied"),
		},
		{
			name: "lookup",
			exchange: resultExchange("lookup", lookup.Args{Query: "modern Go release"}, agent.SuccessStatus,
				"## Result\n\nGo has a new release. [Source](https://example.test/release)."),
		},
		{
			name: "lookup approval timed out",
			exchange: resultExchange("lookup", lookup.Args{Query: "modern Go release"}, agent.ErrorStatus,
				"approval timed out after 1m; lookup did not run"),
		},
		{
			name: "lookup approval unavailable",
			exchange: resultExchange("lookup", lookup.Args{Query: "modern Go release"}, agent.ErrorStatus,
				"approval unavailable; lookup did not run"),
		},
		{
			name: "lookup stopped",
			exchange: resultExchange("lookup", lookup.Args{Query: "modern Go release"}, agent.CancelledStatus,
				"the search stopped because the user pressed escape"),
		},
		{
			name: "lookup stopped with partial output",
			exchange: resultExchange("lookup", lookup.Args{Query: "modern Go release"}, agent.CancelledStatus,
				tool.MarkUntrusted(`a web search for "modern Go release"`, "Partial answer.")+"\n\nnote: the search stopped because the user pressed escape."),
		},
		{
			name: "fetch markdown",
			exchange: resultExchange("fetch", fetch.Args{URL: "https://example.test/article", Type: "markdown"}, agent.SuccessStatus,
				"[raw HTML saved to /state/sessions/tame-impala/drops/fetch-0123456789abcdef.html]\n\n"+
					"# Article\n\n- first\n- second\n"),
		},
		{
			name: "fetch html",
			exchange: resultExchange("fetch", fetch.Args{URL: "https://example.test/raw", Type: "raw"}, agent.SuccessStatus,
				"[raw HTML saved to /state/sessions/tame-impala/drops/fetch-fedcba9876543210.html]\n\n"+
					"<!DOCTYPE html>\n<title>Hello</title>\n"),
		},
		{
			name: "fetch clean HTML",
			exchange: resultExchange("fetch", fetch.Args{URL: "https://example.test/clean", Type: "clean_html"}, agent.SuccessStatus,
				"[raw HTML saved to /state/sessions/tame-impala/drops/fetch-clean.html]\n\n"+
					"<h1>Hello</h1>\n<p>Clean page.</p>\n"),
		},
		{
			name: "fetch text",
			exchange: resultExchange("fetch", fetch.Args{URL: "https://example.test/plain", Type: "text"}, agent.SuccessStatus,
				"[raw HTML saved to /state/sessions/tame-impala/drops/fetch-text.html]\n\n"+
					"Hello\n\nPlain page.\n"),
		},
		{
			name: "fetch failure",
			exchange: resultExchange("fetch", fetch.Args{URL: "https://example.test/missing", Type: "text"}, agent.ErrorStatus,
				"web fetch failed with status 404: page not found"),
		},
		{
			name: "fetch HTTP failure",
			exchange: resultExchange("fetch", fetch.Args{URL: "https://example.test/teapot", Type: "text"}, agent.ErrorStatus,
				"fetch returned HTTP 418: not today (raw HTML saved to /state/sessions/tame-impala/drops/fetch-error.html)"),
		},
		{
			name: "fetch empty page",
			exchange: resultExchange("fetch", fetch.Args{URL: "https://example.test/empty", Type: "text"}, agent.ErrorStatus,
				"fetch returned no content (raw HTML saved to /state/sessions/tame-impala/drops/fetch-empty.html)"),
		},
		{
			name: "fetch save failure",
			exchange: resultExchange("fetch", fetch.Args{URL: "https://example.test/article", Type: "markdown"}, agent.ErrorStatus,
				"save raw HTML: drops are unavailable"),
		},
		{
			name: "fetch refused",
			exchange: resultExchange("fetch", fetch.Args{URL: "https://example.test/article", Type: "markdown"}, agent.ErrorStatus,
				"fetch refused; fetch did not run"),
		},
		{
			name: "fetch stopped",
			exchange: resultExchange("fetch", fetch.Args{URL: "https://example.test/article", Type: "markdown"}, agent.CancelledStatus,
				"the fetch stopped because the user pressed escape"),
		},
		{
			name: "fetch reached its time limit",
			exchange: resultExchange("fetch", fetch.Args{URL: "https://example.test/article", Type: "markdown"}, agent.CancelledStatus,
				"the fetch reached its time limit"),
		},
		{
			name: "fetch approval timed out",
			exchange: resultExchange("fetch", fetch.Args{URL: "https://example.test/article", Type: "markdown"}, agent.ErrorStatus,
				"approval timed out after 1m; fetch did not run"),
		},
		{
			name: "fetch approval unavailable",
			exchange: resultExchange("fetch", fetch.Args{URL: "https://example.test/article", Type: "markdown"}, agent.ErrorStatus,
				"approval unavailable; fetch did not run"),
		},
		{
			name: "task with invalid name",
			exchange: resultExchange("task", task.Args{Action: "status", Name: "Way Too Long"}, agent.ErrorStatus,
				invalidTaskNameError(t, "Way Too Long")),
		},
		{
			name: "task request stopped",
			exchange: resultExchange("task", task.Args{Action: "list"}, agent.CancelledStatus,
				"the task request stopped because the user pressed escape"),
		},
		{
			name: "title",
			exchange: resultExchange("title", title.Args{Title: "render-useful-results"}, agent.SuccessStatus,
				"session titled \"render-useful-results\""),
		},
		{
			name: "title change stopped",
			exchange: resultExchange("title", title.Args{Title: "render-useful-results"}, agent.CancelledStatus,
				"the title change stopped because the user pressed escape"),
		},
		{
			name: "port request stopped",
			exchange: resultExchange("forward", map[string]any{"action": "add", "port": 8080}, agent.CancelledStatus,
				"the port request stopped because the user pressed escape"),
		},
		{
			name: "subagent request stopped",
			exchange: resultExchange("subagent", map[string]any{"action": "status"}, agent.CancelledStatus,
				"the subagent request stopped because the user pressed escape"),
		},
		{
			name: "parent message stopped",
			exchange: resultExchange("message_parent", map[string]any{"message": "halfway there"}, agent.CancelledStatus,
				"the message stopped because the user pressed escape"),
		},
		{
			name: "unknown tool",
			exchange: resultExchange("weather", map[string]any{"city": "London", "days": 3}, agent.SuccessStatus,
				"Rain, then sun."),
		},
	}

	var drawn strings.Builder
	for _, test := range cases {
		fmt.Fprintf(&drawn, "=== %s ===\n%s\n\n", test.name, strutil.VisibleEscapes(render(test.exchange, 60)))
	}
	assertGolden(t, "render.ansi", drawn.String())
}

func TestGoldenTaskRunsRenderForTheUser(t *testing.T) {
	cases := []struct {
		name     string
		snapshot tasks.Snapshot
	}{
		{
			name: "shell task done",
			snapshot: taskSnapshot(bash.Args{Command: "go build ./..."}, tasks.Done, "",
				"building\nbuilt oh\n"),
		},
		{
			name: "shell task failed",
			snapshot: taskSnapshot(bash.Args{Command: "just check"}, tasks.Failed, "exit status 1",
				"lint1  ✗\nmain.go:12: undefined: run\n"),
		},
		{
			name:     "shell task stopped",
			snapshot: taskSnapshot(bash.Args{Command: "sleep 30"}, tasks.Stopped, "", ""),
		},
		{
			name: "search task",
			snapshot: taskSnapshot(grep.Args{Pattern: "func Run"}, tasks.Done, "",
				"cmd/oh/main.go:12:func Run() error {\n"),
		},
		{
			name:     "task without invocation",
			snapshot: tasks.Snapshot{ID: "restored", Run: 2, State: tasks.TimedOut, Failure: "time limit reached"},
		},
	}

	var drawn strings.Builder
	for _, test := range cases {
		fmt.Fprintf(&drawn, "=== %s ===\n%s\n\n", test.name, strutil.VisibleEscapes(renderTask(test.snapshot, 60)))
	}
	assertGolden(t, "task.ansi", drawn.String())
}

func taskSnapshot(arguments any, state tasks.State, failure string, output string) tasks.Snapshot {
	encodedArguments, err := json.Marshal(arguments)
	if err != nil {
		panic(err)
	}
	toolName := "bash"
	if _, isSearch := arguments.(grep.Args); isSearch {
		toolName = "grep"
	}
	return tasks.Snapshot{
		Invocation: &tasks.Invocation{Arguments: string(encodedArguments)},
		ID:         "task-1",
		ToolName:   toolName,
		Run:        1,
		State:      state,
		Failure:    failure,
		Output:     output,
	}
}

func invalidTaskNameError(t *testing.T, name string) string {
	t.Helper()

	err := tasks.ValidateName(name)
	if err == nil {
		t.Fatalf("expected %q to be an invalid task name", name)
	}
	return err.Error()
}

func resultExchange(name string, arguments any, status agent.Status, text string) toolresult.Exchange {
	encodedArguments, err := json.Marshal(arguments)
	if err != nil {
		panic(err)
	}
	return toolresult.Exchange{
		Request: agent.Event{Kind: agent.ToolCallRequestEvent, ID: "call-1", Name: name, Arguments: string(encodedArguments)},
		Result:  agent.Event{Kind: agent.ToolCallResultEvent, ID: "call-1", Name: name, Status: status, Text: text},
	}
}

func assertGolden(t *testing.T, name string, drawn string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)

	if *updateGoldens {
		if err := os.MkdirAll("testdata", 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(drawn), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if drawn != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}
