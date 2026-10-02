package painter

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/call"
	"crdx.org/oh/internal/app/dynamic"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/bash"
	"crdx.org/oh/pkg/toolbox/job"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

var callRowColumns = []int{120, 80, 60, 44, 30, 16}

const callRowSession = "tame-impala"

type callRowOutcome struct {
	status  agent.Status
	text    string
	took    time.Duration
	metrics *tool.ToolCallMetrics
}

type callRowCall struct {
	name      string
	arguments string
	outcome   *callRowOutcome
}

type callRowCase struct {
	title    string
	calls    []callRowCall
	isFrozen bool
}

func shellRowTool() tool.Tool {
	return bash.New(
		nil,
		func(context.Context) (sandbox.Policy, error) { return sandbox.Policy{}, nil },
		func(context.Context, string, string) error { return nil },
		sandbox.Direct(),
		true,
	)
}

func withOptionalIntent(subject tool.Tool) tool.Tool {
	schema := append(tool.Schema{}, subject.Schema()...)
	for index, parameter := range schema {
		if parameter.Name == "intent" {
			schema[index] = parameter.Optional()
		}
	}

	return tool.WithSnapshot(subject, tool.Snapshot{
		Definition: tool.Definition{Name: subject.Name(), Description: subject.Description(), Schema: schema},
		Revision:   subject.Revision(),
	})
}

func callRowTools(isFrozen bool) func(string) (tool.Tool, bool) {
	shellTool := shellRowTool()
	if isFrozen {
		shellTool = withOptionalIntent(shellTool)
	}
	jobTool := job.New(nil, nil, nil, nil)

	return func(name string) (tool.Tool, bool) {
		switch name {
		case "bash":
			return shellTool, true
		case "job":
			return jobTool, true
		}

		return nil, false
	}
}

func shellSucceeded(text string, lines int64) *callRowOutcome {
	return &callRowOutcome{
		status: agent.SuccessStatus,
		text:   text,
		took:   300 * time.Millisecond,
		metrics: &tool.ToolCallMetrics{
			Kind:       tool.MetricResources,
			Lines:      lines,
			Bytes:      int64(len(text)),
			PeakMemory: 28 << 20,
			CPUTime:    200 * time.Millisecond,
		},
	}
}

func shellSilent() *callRowOutcome {
	return &callRowOutcome{
		status:  agent.SuccessStatus,
		metrics: &tool.ToolCallMetrics{Kind: tool.MetricResources, PeakMemory: 2 << 20},
	}
}

func shellFailed(text string) *callRowOutcome {
	return &callRowOutcome{
		status:  agent.ErrorStatus,
		text:    text,
		took:    900 * time.Millisecond,
		metrics: &tool.ToolCallMetrics{Kind: tool.MetricResources, Lines: 2, PeakMemory: 30 << 20},
	}
}

func callRefused(text string) *callRowOutcome {
	return &callRowOutcome{status: agent.ErrorStatus, text: text}
}

func callStopped(text string) *callRowOutcome {
	return &callRowOutcome{status: agent.CancelledStatus, text: text, took: 4 * time.Second}
}

func jobAnswered(text string) *callRowOutcome {
	return &callRowOutcome{
		status: agent.SuccessStatus,
		text:   text,
		metrics: &tool.ToolCallMetrics{
			Kind:  tool.MetricOutput,
			Lines: int64(strings.Count(text, "\n") + 1),
			Bytes: int64(len(text)),
		},
	}
}

func startJob(name string, intent string) callRowCall {
	return callRowCall{
		name:      "job",
		arguments: fmt.Sprintf(`{"action":"start","name":%q,"command":"just %s","intent":%q}`, name, name, intent),
		outcome:   jobAnswered("started " + name),
	}
}

func callRowCases() []callRowCase {
	return []callRowCase{
		{title: "shell succeeded with output", calls: []callRowCall{{"bash", `{"command":"git status --short","intent":"Checking the working tree state"}`, shellSucceeded("M internal/app/call/call.go\nM pkg/toolbox/job/job.go", 2)}}},
		{title: "shell succeeded with no output", calls: []callRowCall{{"bash", `{"command":"touch build.lock","intent":"Creating the build lock file"}`, shellSilent()}}},
		{title: "shell failed with an exit code", calls: []callRowCall{{"bash", `{"command":"go build ./...","intent":"Building every package once more"}`, shellFailed("exit(1): ./cmd/oh/main.go:12:2: undefined: spinner")}}},
		{title: "shell stopped by the user", calls: []callRowCall{{"bash", `{"command":"sleep 10","intent":"Waiting for the server to settle"}`, callStopped("the command was stopped because the user pressed escape")}}},
		{title: "shell stopped by its time limit", calls: []callRowCall{{"bash", `{"command":"just test","intent":"Running the whole test suite"}`, callStopped("the command was stopped after 2m because it ran out of time")}}},
		{title: "shell with a multi-line command", calls: []callRowCall{{"bash", `{"command":"for NAME in one two three; do\n  echo \"$NAME\"\ndone","intent":"Printing each name in turn"}`, shellSucceeded("one\ntwo\nthree", 3)}}},
		{title: "shell with a heredoc", calls: []callRowCall{{"bash", `{"command":"cat > notes.md <<'EOF'\n# Notes\n\nKeep the intent first.\nEOF","intent":"Writing the design notes file"}`, shellSilent()}}},
		{title: "shell with a long command", calls: []callRowCall{{"bash", `{"command":"git diff --word-diff=plain -- internal/app/harness/testdata/output/host-commands.ansi internal/app/harness/testdata/output/frame-edges.screen","intent":"Inspecting the host command golden changes"}`, shellSucceeded("diff --git a/internal/app/harness/testdata/output/host-commands.ansi", 103)}}},
		{title: "shell with a long intent", calls: []callRowCall{{"bash", `{"command":"ls","intent":"Listing everything in the workspace root before deciding which of the many directories to search next"}`, shellSucceeded("README.md", 1)}}},
		{title: "shell with a lowercase intent", calls: []callRowCall{{"bash", `{"command":"just check","intent":"running every lint and test"}`, shellSucceeded("ok all eight steps", 1)}}},
		{title: "shell with whitespace and control characters in its intent", calls: []callRowCall{{"bash", `{"command":"true","intent":"  Checking\tthe\nshell \u001b[31mworks  "}`, shellSilent()}}},
		{title: "shell with wide characters", calls: []callRowCall{{"bash", `{"command":"echo 日本語のテキスト","intent":"Printing 日本語 to the terminal"}`, shellSucceeded("日本語のテキスト", 1)}}},
		{title: "shell on the host network, approved", calls: []callRowCall{{"bash", `{"command":"curl https://example.com/status","intent":"Checking what the status endpoint reports","network":"host"}`, shellSucceeded(`{"status":"ok"}`, 1)}}},
		{title: "shell on the host network, refused", calls: []callRowCall{{"bash", `{"command":"curl example.com","intent":"Checking what example.com serves","network":"host"}`, callRefused("host-network access refused; command did not run")}}},
		{title: "shell withheld", calls: []callRowCall{{"bash", `{"command":"echo forbidden","intent":"Printing a word to test access"}`, callRefused("shell access unavailable; ctrl+x x grants it")}}},
		{title: "shell with invalid Bash", calls: []callRowCall{{"bash", `{"command":"for do done esac fi \"unclosed","intent":"Trying out some broken syntax"}`, callRefused("syntax error")}}},
		{title: "shell without an intent in a new session", calls: []callRowCall{{"bash", `{"command":"ls -la"}`, callRefused("intent is required")}}},
		{title: "shell without an intent in an old session", isFrozen: true, calls: []callRowCall{{"bash", `{"command":"ls -la"}`, shellSucceeded("total 8", 1)}}},
		{title: "shell with an unknown parameter", calls: []callRowCall{{"bash", `{"command":"ls","intent":"Listing the files","colour":"blue"}`, callRefused("unknown parameter: colour")}}},
		{title: "shell with arguments that are not JSON", calls: []callRowCall{{"bash", `{"command":`, callRefused("could not parse the arguments")}}},
		{title: "several shell calls in one round", calls: []callRowCall{
			{"bash", `{"command":"git status --short","intent":"Checking the working tree state"}`, shellSucceeded("M a.go", 1)},
			{"bash", `{"command":"go vet ./...","intent":"Vetting every package"}`, shellFailed("exit(1): vet: a.go:3: unreachable code")},
			{"bash", `{"command":"sleep 5","intent":"Pausing for the build cache"}`, callStopped("the command was stopped because the user pressed escape")},
		}},
		{title: "job start", calls: []callRowCall{{"job", `{"action":"start","name":"docs","command":"python3 -m http.server 8080","intent":"Serving the documentation locally"}`, jobAnswered("started docs")}}},
		{title: "job start with a forwarded port", calls: []callRowCall{{"job", `{"action":"start","name":"docs","port":8080,"command":"just docs","intent":"Serving the docs on the forwarded port"}`, jobAnswered("started docs; forwarded http://127.82.233.168:8080")}}},
		{title: "job start with a multi-line command", calls: []callRowCall{{"job", `{"action":"start","name":"watch","command":"while true; do\n  just build\n  sleep 5\ndone","intent":"Rebuilding every five seconds"}`, jobAnswered("started watch")}}},
		{title: "job restart", calls: []callRowCall{{"job", `{"action":"start","name":"docs","intent":"Restarting the documentation server"}`, jobAnswered("restarted docs")}}},
		{title: "job restart with a port", calls: []callRowCall{{"job", `{"action":"start","name":"docs","port":8080,"intent":"Restarting the docs on its port"}`, jobAnswered("restarted docs")}}},
		{title: "job start refused", calls: []callRowCall{{"job", `{"action":"start","name":"docs","command":"just docs","intent":"Serving the documentation locally"}`, callRefused("a job of that name is already running")}}},
		{title: "job start without an intent", calls: []callRowCall{{"job", `{"action":"start","name":"docs","command":"just docs"}`, callRefused("intent is required to start a job")}}},
		{title: "job start with an invalid name", calls: []callRowCall{{"job", `{"action":"start","name":"way-too-long-a-name","command":"true","intent":"Trying a name that is too long"}`, callRefused("job name must match [a-z0-9-]{1,10}")}}},
		{title: "job calls naming nobody's job", calls: []callRowCall{
			{"job", `{"action":"status","name":"docs"}`, jobAnswered("docs is running")},
			{"job", `{"action":"output","name":"docs"}`, jobAnswered("Serving HTTP on 0.0.0.0 port 8080")},
		}},
		{title: "job calls reminded of their job", calls: []callRowCall{
			startJob("docs", "Serving the documentation locally"),
			{"job", `{"action":"status","name":"docs"}`, jobAnswered("docs is running")},
			{"job", `{"action":"output","name":"docs"}`, jobAnswered("Serving HTTP on 0.0.0.0 port 8080\n127.0.0.1 - GET / 200")},
			{"job", `{"action":"stop","name":"docs"}`, jobAnswered("stopped docs")},
			{"job", `{"action":"discard","name":"docs"}`, jobAnswered("discarded docs")},
		}},
		{title: "job waits reminded of every job", calls: []callRowCall{
			startJob("docs", "Serving the documentation locally"),
			startJob("build", "Building every package"),
			{"job", `{"action":"wait","names":["docs","build"]}`, jobAnswered("build finished with exit 0")},
			{"job", `{"action":"wait","names":["docs","build"],"wait_for":"all","wait_seconds":20}`, jobAnswered("docs still running\nbuild finished")},
			{"job", `{"action":"wait","names":["docs","other"]}`, jobAnswered("docs still running")},
		}},
		{title: "job reminded of its newest start", calls: []callRowCall{
			startJob("docs", "Serving the documentation locally"),
			{"job", `{"action":"status","name":"docs"}`, jobAnswered("docs is running")},
			startJob("docs", "Serving the docs on a new port"),
			{"job", `{"action":"status","name":"docs"}`, jobAnswered("docs is running")},
		}},
		{title: "job calls naming no job", calls: []callRowCall{
			startJob("docs", "Serving the documentation locally"),
			{"job", `{"action":"list"}`, jobAnswered("docs running\nbuild finished")},
			{"job", `{"action":"prune"}`, jobAnswered("pruned build")},
		}},
		{title: "job wait stopped by the user", calls: []callRowCall{
			startJob("check", "Running the full check"),
			{"job", `{"action":"wait","name":"check","wait_seconds":270}`, callStopped("stopped because the user pressed escape")},
		}},
		{title: "job call with an unknown action", calls: []callRowCall{{"job", `{"action":"explode","name":"docs"}`, callRefused("action must be one of start, status, output, wait, stop, discard, list, prune")}}},
		{title: "shell and job calls mixed in one round", calls: []callRowCall{
			startJob("docs", "Serving the documentation locally"),
			{"bash", `{"command":"curl -s localhost:8080 | head -1","intent":"Checking the docs answer requests"}`, shellSucceeded("<!DOCTYPE html>", 1)},
			{"job", `{"action":"stop","name":"docs"}`, jobAnswered("stopped docs")},
		}},
	}
}

func callRowEvents(item callRowCase, getTool func(string) (tool.Tool, bool), isStored bool) []agent.Event {
	var events []agent.Event
	for index, made := range item.calls {
		id := fmt.Sprintf("call-%d", index+1)
		request := agent.Event{Kind: agent.ToolCallRequestEvent, ID: id, Name: made.name, Arguments: made.arguments}
		if isStored {
			request.FallbackRendering = call.Describe(request, getTool, nil)
		}
		events = append(events, request)
	}
	for index, made := range item.calls {
		if made.outcome == nil {
			continue
		}
		events = append(events, agent.Event{
			Kind:    agent.ToolCallResultEvent,
			ID:      fmt.Sprintf("call-%d", index+1),
			Name:    made.name,
			Status:  made.outcome.status,
			Text:    made.outcome.text,
			Took:    made.outcome.took,
			Metrics: made.outcome.metrics,
		})
	}

	return events
}

func drawCallRows(item callRowCase, columns int, isStored bool) string {
	getTool := callRowTools(item.isFrozen)
	events := callRowEvents(item, getTool, isStored)

	var screenOutput bytes.Buffer
	screen := output.NewTerminalOfSize(&screenOutput, columns, 0).AppendOnly().WithoutMessageMarks()
	drawingTool := getTool
	if isStored {
		drawingTool = nil
	}
	picasso := New(screen, false, drawingTool, nil, output.StreamingModeLine)
	picasso.LinkToolResults(callRowSession)
	for _, event := range events {
		picasso.DrawEvent(event)
	}
	picasso.Close(dynamic.Cancelled)
	screen.End()

	return strings.Trim(strings.ReplaceAll(screenOutput.String(), "\r\n", "\n"), "\n")
}

func drawRunningCallRow(made callRowCall, columns int) string {
	label := call.LabelFor(
		agent.Event{Kind: agent.ToolCallRequestEvent, ID: "call-1", Name: made.name, Arguments: made.arguments},
		callRowTools(false),
		nil,
	)
	block := dynamic.NewBlock(func() {})
	block.Add(label, label.TimeLimit)

	return strings.Join(block.Rows(columns), "\n")
}

func runningCallRows() []callRowCase {
	return []callRowCase{
		{title: "shell running", calls: []callRowCall{{"bash", `{"command":"just test","intent":"Running the whole test suite"}`, nil}}},
		{title: "shell running on the host network", calls: []callRowCall{{"bash", `{"command":"curl https://example.com/status","intent":"Checking what the status endpoint reports","network":"host"}`, nil}}},
		{title: "shell running a long command", calls: []callRowCall{{"bash", `{"command":"go test -run 'TestGolden' -count=1 ./internal/app/harness/ ./internal/app/painter/ ./internal/app/call/","intent":"Regenerating the harness golden files"}`, nil}}},
		{title: "job starting", calls: []callRowCall{{"job", `{"action":"start","name":"golden","command":"cd /tmp/oh && just golden 2>&1 | tail -30","intent":"Regenerating every golden in scratch"}`, nil}}},
		{title: "job waiting with a limit", calls: []callRowCall{{"job", `{"action":"wait","name":"golden","wait_seconds":20}`, nil}}},
		{title: "job waiting on several", calls: []callRowCall{{"job", `{"action":"wait","names":["docs","build"],"wait_for":"all"}`, nil}}},
	}
}

func callRowDocument(draw func(item callRowCase, columns int) string, cases []callRowCase) string {
	var sections []string
	for _, item := range cases {
		for _, columns := range callRowColumns {
			sections = append(sections, fmt.Sprintf("── %s · %d columns\n%s\n", item.title, columns, draw(item, columns)))
		}
	}

	return strings.Join(sections, "\n")
}

func compareCallRowGolden(t *testing.T, name string, drawn string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
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
		t.Errorf("rows differ from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}

func TestGoldenEveryShellAndJobRowIsDrawnAsBefore(t *testing.T) {
	settled := callRowDocument(func(item callRowCase, columns int) string {
		return drawCallRows(item, columns, false)
	}, callRowCases())
	running := callRowDocument(func(item callRowCase, columns int) string {
		return drawRunningCallRow(item.calls[0], columns)
	}, runningCallRows())

	compareCallRowGolden(t, "call-rows.ansi", settled)
	compareCallRowGolden(t, "call-rows.txt", style.Plain(settled))
	compareCallRowGolden(t, "running-call-rows.ansi", running)
	compareCallRowGolden(t, "running-call-rows.txt", style.Plain(running))
}

func TestGoldenAStoredCallIsDrawnAsItWasLive(t *testing.T) {
	for _, item := range callRowCases() {
		for _, columns := range callRowColumns {
			live := drawCallRows(item, columns, false)
			stored := drawCallRows(item, columns, true)
			if live != stored {
				t.Errorf("%s at %d columns:\n--- live ---\n%s\n--- stored ---\n%s", item.title, columns, style.Plain(live), style.Plain(stored))
			}
		}
	}
}
