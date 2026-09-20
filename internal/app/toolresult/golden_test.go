package toolresult

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/internal/file"
	"crdx.org/io/internal/jobs"
	internaltoolresult "crdx.org/io/internal/toolresult"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/toolbox/bash"
	"crdx.org/io/pkg/toolbox/edit"
	"crdx.org/io/pkg/toolbox/fetch"
	"crdx.org/io/pkg/toolbox/find"
	"crdx.org/io/pkg/toolbox/grep"
	"crdx.org/io/pkg/toolbox/job"
	"crdx.org/io/pkg/toolbox/lookup"
	"crdx.org/io/pkg/toolbox/ls"
	"crdx.org/io/pkg/toolbox/notify"
	"crdx.org/io/pkg/toolbox/read"
	"crdx.org/io/pkg/toolbox/title"
	"crdx.org/io/pkg/toolbox/write"
)

func TestGoldenToolResultsRenderForTheUser(t *testing.T) {
	cases := []struct {
		name     string
		exchange internaltoolresult.Exchange
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
			name: "shell success",
			exchange: resultExchange("bash", bash.Args{Command: "go test ./...\nprintf 'done\\n'"}, agent.SuccessStatus,
				"ok  crdx.org/io\ndone\n"),
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
			name: "find files",
			exchange: resultExchange("find", find.Args{Pattern: "**/*.go", Path: "cmd"}, agent.SuccessStatus,
				"cmd/oh/main.go\ncmd/oh/ctl/ctl.go\n"),
		},
		{
			name:     "find without matches",
			exchange: resultExchange("find", find.Args{Pattern: "**/*.cobol"}, agent.SuccessStatus, "(no matches)"),
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
				"fetch refused; fetch did not run; choose another approach"),
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
			name: "job with invalid name",
			exchange: resultExchange("job", job.Args{Action: "start", Name: "way-too-long", Command: "true"}, agent.ErrorStatus,
				invalidJobNameError(t, "way-too-long")),
		},
		{
			name: "job wait on a job that finished",
			exchange: resultExchange("job", job.Args{Action: "wait", Name: "check"}, agent.SuccessStatus,
				"check: complete after 37s\nok  crdx.org/io\nlint1  \u2713\n"),
		},
		{
			name: "job wait on several jobs until any finishes",
			exchange: resultExchange("job", job.Args{Action: "wait", Names: []string{"build", "lint"}}, agent.SuccessStatus,
				"lint: complete after 22s\nall checks passed\n"),
		},
		{
			name: "job wait for any that reached its limit",
			exchange: resultExchange("job", job.Args{Action: "wait", Names: []string{"build", "lint"}}, agent.SuccessStatus,
				"build: running for 5m05s\n"+
					"lint: running for 5m01s\n"+
					"note: the wait gave up after 5m00s before any watched job ended.\n"),
		},
		{
			name: "job wait on several jobs until all finish",
			exchange: resultExchange("job", job.Args{
				Action:  "wait",
				Names:   []string{"build", "lint"},
				WaitFor: "all",
			}, agent.SuccessStatus,
				"build: complete after 37s\nok  crdx.org/io\n\n"+
					"lint: complete after 22s\nall checks passed\n"),
		},
		{
			name: "job wait for all that reached its limit",
			exchange: resultExchange("job", job.Args{
				Action:  "wait",
				Names:   []string{"build", "lint"},
				WaitFor: "all",
			}, agent.SuccessStatus,
				"build: complete after 37s\n"+
					"lint: running for 5m05s\n"+
					"note: the wait gave up after 5m00s before all watched jobs ended.\n"),
		},
		{
			name: "job wait that gave up on a job still running",
			exchange: resultExchange("job", job.Args{Action: "wait", Name: "docs"}, agent.SuccessStatus,
				"docs: running for 5m05s\n"+
					"note: the wait gave up after 5m00s, and the job is still running.\n"+
					"Serving HTTP on localhost port 8080 ...\n"),
		},
		{
			name: "title",
			exchange: resultExchange("title", title.Args{Title: "render-useful-results"}, agent.SuccessStatus,
				"session titled \"render-useful-results\""),
		},
		{
			name: "notification",
			exchange: resultExchange("notify", notify.Args{Title: "Checks passed", Message: "Everything is green", Icon: "success"}, agent.SuccessStatus,
				"notification sent"),
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

func invalidJobNameError(t *testing.T, name string) string {
	t.Helper()

	err := jobs.ValidateName(name)
	if err == nil {
		t.Fatalf("expected %q to be an invalid job name", name)
	}
	return err.Error()
}

func resultExchange(name string, arguments any, status agent.Status, text string) internaltoolresult.Exchange {
	encodedArguments, err := json.Marshal(arguments)
	if err != nil {
		panic(err)
	}
	return internaltoolresult.Exchange{
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
