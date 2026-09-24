package transcript_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/interrupt"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/store/transcript"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
)

var transcriptParser = goldmark.New().Parser()

func transcriptWellFormedness(t *testing.T, source string) ast.Node {
	t.Helper()

	return transcriptParser.Parse(text.NewReader([]byte(source)))
}

func countHeadings(document ast.Node) int {
	count := 0
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && node.Kind() == ast.KindHeading {
			count++
		}

		return ast.WalkContinue, nil
	})

	return count
}

func textNodesContain(t *testing.T, source string, want string) bool {
	t.Helper()

	sourceBytes := []byte(source)
	var visibleText strings.Builder
	_ = ast.Walk(transcriptWellFormedness(t, source), func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if textNode, isText := node.(*ast.Text); entering && isText {
			visibleText.Write(textNode.Segment.Value(sourceBytes))
		}

		return ast.WalkContinue, nil
	})

	return strings.Contains(visibleText.String(), want)
}

func TestTranscriptOmitsReasoningEntirely(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), agent.Event{Kind: agent.ModelReasoningEvent, Text: "First. Second?\nThird!"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(5, 6), agent.Event{Kind: agent.ModelMessageEvent, Text: "answer"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	transcript := string(stored)
	if strings.Contains(transcript, "Reasoning") || strings.Contains(transcript, "First. Second?") || strings.Contains(transcript, "Third!") {
		t.Errorf("expected no trace of reasoning at all, got:\n%s", transcript)
	}
	if !strings.Contains(transcript, "## Assistant · +4s\n\nanswer\n") {
		t.Errorf("expected the reasoning to leave the next event untouched, got:\n%s", transcript)
	}
}

func TestTranscriptRoundsShortElapsedTimesToTenths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	startedAt := time.Unix(1, 0)
	recorder, err := transcript.Open(path, transcript.Meta{Name: "tame-impala", StartedAt: startedAt, Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(startedAt.Add(50*time.Millisecond), agent.Event{Kind: agent.ModelMessageEvent, Text: "answer"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stored), "## Assistant · +0.1s") {
		t.Errorf("short elapsed time was not rendered to tenths:\n%s", stored)
	}
}

func TestTranscriptLogsACallAndItsResultOnOneLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	request := agent.Event{Kind: agent.ToolCallRequestEvent, ID: "call-1", Name: "bash", Arguments: `{"command":"ls"}`}
	request.Subject = "ls"
	request.Emphasis = tool.Emphasis{Kind: tool.EmphasisSyntax, Value: "bash"}
	if err := recorder.Event(time.Unix(3, 4), request); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(5, 6), agent.Event{
		Kind:   agent.ToolCallResultEvent,
		ID:     "call-1",
		Name:   "bash",
		Status: agent.SuccessStatus,
		Took:   12 * time.Second,
		Metrics: &tool.ToolCallMetrics{
			Kind:        tool.MetricResources,
			Lines:       1,
			Bytes:       2048,
			TotalBytes:  4096,
			CPUTime:     2500 * time.Millisecond,
			PeakMemory:  4 << 20,
			IsTruncated: true,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(7, 8), agent.Event{Kind: agent.ModelMessageEvent, Text: "done"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	transcript := string(stored)
	want := "## Tool calls · +2s\n\n```\nbash: ls [ok in 12s, 1 line, 2K of 4K, 2s CPU, 4M peak, truncated, call-1]\n```"
	if !strings.Contains(transcript, want) {
		t.Errorf("expected one heading naming the call and its result folded onto one line, got:\n%s", transcript)
	}
	if strings.Contains(transcript, `{"command":"ls"}`) || strings.Contains(transcript, "## Tool call —") || strings.Contains(transcript, "## Tool result —") {
		t.Errorf("expected no per-call heading and no raw arguments, got:\n%s", transcript)
	}
}

func TestTranscriptDoesNotAttributeToolCallsToTheUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), agent.Event{Kind: agent.UserMessageEvent, Text: "check the web"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(5, 6), agent.Event{Kind: agent.ToolCallRequestEvent, ID: "call-1", Name: "lookup"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(7, 8), agent.Event{
		Kind: agent.ToolCallResultEvent, ID: "call-1", Name: "lookup", Status: agent.SuccessStatus,
	}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(9, 10), agent.Event{Kind: agent.ModelMessageEvent, Text: "done"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	transcript := string(stored)
	want := "## User · +2s\n\ncheck the web\n\n## Tool calls · +4s\n\n```\nlookup [ok, call-1]\n```\n\n## Assistant · +8s"
	if !strings.Contains(transcript, want) {
		t.Errorf("expected the call log held under its own heading between the user and the answer, got:\n%s", transcript)
	}
}

func TestTranscriptLogsSeveralCallsInOneFencedBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"call-1", "call-2"} {
		request := agent.Event{Kind: agent.ToolCallRequestEvent, ID: id, Name: "read"}
		request.Subject = id + ".go"
		if err := recorder.Event(time.Unix(3, 4), request); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"call-1", "call-2"} {
		if err := recorder.Event(time.Unix(5, 6), agent.Event{
			Kind:   agent.ToolCallResultEvent,
			ID:     id,
			Name:   "read",
			Status: agent.SuccessStatus,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	transcript := string(stored)
	want := "```\nread: call-1.go [ok, call-1]\nread: call-2.go [ok, call-2]\n```"
	if !strings.Contains(transcript, want) {
		t.Errorf("expected both calls folded into one block, in order, got:\n%s", transcript)
	}
	if strings.Count(transcript, "```") != 2 {
		t.Errorf("expected exactly one fenced block for both calls, got:\n%s", transcript)
	}
}

func TestTranscriptFallsBackToTheArgumentsOfAnUnrenderedCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), agent.Event{
		Kind:      agent.ToolCallRequestEvent,
		ID:        "call-1",
		Name:      "summon",
		Arguments: `{"name":"one"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	transcript := string(stored)
	if !strings.Contains(transcript, "```\nsummon [call-1]\n```") {
		t.Errorf("expected an unanswered call to be logged with what is known, got:\n%s", transcript)
	}
	if strings.Contains(transcript, `{"name":"one"}`) {
		t.Errorf("expected the raw arguments to give way to the call log, got:\n%s", transcript)
	}
}

func TestTranscriptCollapsesAndTruncatesALongSubject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	request := agent.Event{Kind: agent.ToolCallRequestEvent, ID: "call-1", Name: "bash"}
	request.Subject = "for line in\n  one\n  two\ndo\n  " + strings.Repeat("echo hi; ", 20) + "\ndone"
	if err := recorder.Event(time.Unix(3, 4), request); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	transcript := string(stored)
	start := strings.Index(transcript, "```\n") + len("```\n")
	end := strings.Index(transcript[start:], "\n```")
	block := transcript[start : start+end]
	if strings.Contains(block, "\n") {
		t.Errorf("expected the subject to collapse onto one line, got:\n%s", block)
	}
	if !strings.Contains(block, "…") {
		t.Errorf("expected a long subject to be truncated, got:\n%s", block)
	}
}

func TestTranscriptLogsACallWithNoSubject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), agent.Event{Kind: agent.ToolCallRequestEvent, ID: "call-1", Name: "ls"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(5, 6), agent.Event{
		Kind: agent.ToolCallResultEvent, ID: "call-1", Name: "ls", Status: agent.SuccessStatus,
	}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stored), "```\nls [ok, call-1]\n```") {
		t.Errorf("expected no colon when there is no subject to name, got:\n%s", stored)
	}
}

func TestTranscriptLogsAResultWithoutAMatchingCallInTheSameRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), agent.Event{
		Kind: agent.ToolCallResultEvent, ID: "call-1", Name: "read", Status: agent.SuccessStatus,
	}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stored), "```\nread [ok, call-1]\n```") {
		t.Errorf("expected a stray result to still be logged by name, got:\n%s", stored)
	}
}

func TestTranscriptWritesMessagesAsMarkdownRatherThanFencingThem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	content := "**bold**, a [link](https://example.com), and:\n\n```go\nfunc main() {}\n```"
	if err := recorder.Event(time.Unix(3, 4), agent.Event{Kind: agent.UserMessageEvent, Text: "please explain"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(5, 6), agent.Event{Kind: agent.ModelMessageEvent, Text: content}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	transcript := string(stored)
	if !strings.Contains(transcript, "## User · +2s\n\nplease explain\n") {
		t.Errorf("expected the user's message written as plain markdown, got:\n%s", transcript)
	}
	if !strings.Contains(transcript, "## Assistant · +4s\n\n"+content+"\n") {
		t.Errorf("expected the answer written as its own markdown rather than fenced, got:\n%s", transcript)
	}
}

func TestTranscriptLetsUserTextCollideWithMarkdownSyntax(t *testing.T) {
	for name, test := range map[string]struct {
		text         string
		wantHeadings int
	}{
		"a leading hash renders as a heading, not a comment banner": {
			text:         "# ——— system journal errors ———\nnothing else follows",
			wantHeadings: 3,
		},
		"a rule under a line turns it into a heading of its own": {
			text:         "a rule\n---\nunderneath",
			wantHeadings: 3,
		},
		"an indented continuation line stays part of the same paragraph": {
			text:         "the instruction was:\n    do the thing\n    then stop",
			wantHeadings: 2,
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "transcript.md")
			recorder, err := transcript.Open(path, transcript.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 2), Model: "model"})
			if err != nil {
				t.Fatal(err)
			}
			if err := recorder.Event(time.Unix(3, 4), agent.Event{Kind: agent.UserMessageEvent, Text: test.text}); err != nil {
				t.Fatal(err)
			}
			if err := recorder.Close(); err != nil {
				t.Fatal(err)
			}

			stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
			if err != nil {
				t.Fatal(err)
			}
			transcript := string(stored)
			if !strings.Contains(transcript, "## User · +2s\n\n"+test.text+"\n") {
				t.Errorf("expected the user's text written exactly as given, got:\n%s", transcript)
			}
			document := transcriptWellFormedness(t, transcript)
			if headingCount := countHeadings(document); headingCount != test.wantHeadings {
				t.Errorf("expected %d headings once parsed as markdown, got %d in:\n%s", test.wantHeadings, headingCount, transcript)
			}
		})
	}
}

func TestTranscriptFencesAWholeMessageThatWouldOtherwiseSwallowWhatFollows(t *testing.T) {
	for name, text := range map[string]string{
		"an unclosed backtick fence":         "here's a snippet:\n```\nweird\nand it just keeps going",
		"an unclosed tilde fence":            "~~~~\nfenced with tildes, and a stray ``` inside it",
		"an unclosed script block":           "before\n\n<script>\nvar x = 1;",
		"an unclosed html comment":           "before\n\n<!-- oops",
		"an unclosed pre block":              "before\n\n<pre>\nstuff",
		"an unclosed cdata section":          "before\n\n<![CDATA[\nstuff",
		"an unclosed processing instruction": "before\n\n<?php\necho 1;",
		"an unclosed declaration":            "before\n\n<!DOCTYPE html\nweird",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "transcript.md")
			recorder, err := transcript.Open(path, transcript.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 2), Model: "model"})
			if err != nil {
				t.Fatal(err)
			}
			if err := recorder.Event(time.Unix(3, 4), agent.Event{Kind: agent.ModelMessageEvent, Text: text}); err != nil {
				t.Fatal(err)
			}
			if err := recorder.Event(time.Unix(5, 6), agent.Event{Kind: agent.UserMessageEvent, Text: "canary reply"}); err != nil {
				t.Fatal(err)
			}
			if err := recorder.Close(); err != nil {
				t.Fatal(err)
			}

			stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
			if err != nil {
				t.Fatal(err)
			}
			transcript := string(stored)
			if !textNodesContain(t, transcript, "canary reply") {
				t.Errorf("expected the canary to survive as real text rather than being swallowed, got:\n%s", transcript)
			}
		})
	}
}

func TestTranscriptFencesAWholeUserMessageThatWouldOtherwiseSwallowWhatFollows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), agent.Event{
		Kind: agent.UserMessageEvent,
		Text: "does this look right:\n```go\nfunc broken() {",
	}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(5, 6), agent.Event{Kind: agent.ModelMessageEvent, Text: "canary reply"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	transcript := string(stored)
	if !textNodesContain(t, transcript, "canary reply") {
		t.Errorf("expected the canary to survive as real text rather than being swallowed, got:\n%s", transcript)
	}
}

func TestTranscriptLeavesASelfContainedMessageAlone(t *testing.T) {
	for name, content := range map[string]string{
		"a balanced fence":        "before:\n\n```go\nfunc main() {}\n```\n\nafter",
		"a self-closing tag":      "before\n\n<div>\nstuff",
		"a rule":                  "a rule\n---\nunderneath",
		"a heading":               "# hello\nworld",
		"a table":                 "| a | b |\n|---|---|\n| 1 | 2 |",
		"a nested list":           "- one\n  - two\n- three",
		"a blockquote":            "> line one\n> line two",
		"an unbalanced code span": "some `code that never closes",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "transcript.md")
			recorder, err := transcript.Open(path, transcript.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 2), Model: "model"})
			if err != nil {
				t.Fatal(err)
			}
			if err := recorder.Event(time.Unix(3, 4), agent.Event{Kind: agent.ModelMessageEvent, Text: content}); err != nil {
				t.Fatal(err)
			}
			if err := recorder.Close(); err != nil {
				t.Fatal(err)
			}

			stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(stored), "## Assistant · +2s\n\n"+content+"\n") {
				t.Errorf("expected self-contained markdown left exactly as written rather than fenced, got:\n%s", stored)
			}
		})
	}
}

func FuzzTranscriptMessageNeverSwallowsWhatFollows(f *testing.F) {
	for _, seed := range []string{
		"plain text",
		"here's a snippet:\n```\nweird\nand it just keeps going",
		"~~~~\nfenced with tildes, and a stray ``` inside it",
		"before\n\n<script>\nvar x = 1;",
		"before\n\n<!-- oops",
		"before\n\n<pre>\nstuff",
		"before\n\n<![CDATA[\nstuff",
		"before\n\n<?php\necho 1;",
		"before\n\n<!DOCTYPE html\nweird",
		"before:\n\n```go\nfunc main() {}\n```\n\nafter",
		"before\n\n<div>\nstuff",
		"a rule\n---\nunderneath",
		"# heading\ntext",
		"| a | b |\n|---|---|\n| 1 | 2 |",
		"- one\n  - two\n- three",
		"> line one\n> line two",
		"some `code that never closes",
		"````\nnested ```\nstill open",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, text string) {
		path := filepath.Join(t.TempDir(), "transcript.md")
		recorder, err := transcript.Open(path, transcript.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 2), Model: "model"})
		if err != nil {
			t.Fatal(err)
		}
		if err := recorder.Event(time.Unix(3, 4), agent.Event{Kind: agent.UserMessageEvent, Text: text}); err != nil {
			t.Fatal(err)
		}
		if err := recorder.Event(time.Unix(5, 6), agent.Event{Kind: agent.ModelMessageEvent, Text: "canary reply"}); err != nil {
			t.Fatal(err)
		}
		if err := recorder.Close(); err != nil {
			t.Fatal(err)
		}

		stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
		if err != nil {
			t.Fatal(err)
		}
		if !textNodesContain(t, string(stored), "canary reply") {
			t.Errorf("expected the canary to survive as real text rather than being swallowed, got:\n%s", stored)
		}
	})
}

func TestTranscriptUsesAFenceLongerThanItsContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 2), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	content := "before\n````\nafter"
	if err := recorder.Event(time.Unix(3, 4), agent.Event{Kind: agent.FailureEvent, Text: content}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	transcript := string(stored)
	if !strings.Contains(transcript, "`````\n"+content+"\n`````") {
		t.Errorf("expected a five-backtick fence, got:\n%s", transcript)
	}
	if !strings.Contains(transcript, "# Conversation") || !strings.Contains(transcript, "## Failure") {
		t.Errorf("expected the metadata and event headings, got:\n%s", transcript)
	}
}

func TestTranscriptRetainsTurnFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 2)})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), agent.Event{
		Kind: agent.FailureEvent,
		Text: "read: connection reset by peer",
	}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	transcript := string(stored)
	for _, want := range []string{"## Failure", "read: connection reset by peer"} {
		if !strings.Contains(transcript, want) {
			t.Errorf("expected %q in the transcript, got:\n%s", want, transcript)
		}
	}
}

func transcriptOfOneEvent(t *testing.T, event agent.Event) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 2)})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), event); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	return string(stored)
}

func TestTranscriptNamesWhyATurnWasInterrupted(t *testing.T) {
	for name, test := range map[string]struct {
		cause interrupt.Cause
		want  string
	}{
		"with a cause": {
			cause: interrupt.Escape,
			want:  "Turn stopped because the user pressed escape.",
		},
		"without a cause": {
			cause: "",
			want:  "Turn stopped.",
		},
	} {
		t.Run(name, func(t *testing.T) {
			written := transcriptOfOneEvent(t, interrupt.Event(test.cause))

			for _, want := range []string{"## Interrupted", test.want} {
				if !strings.Contains(written, want) {
					t.Errorf("expected %q in the transcript, got:\n%s", want, written)
				}
			}
		})
	}
}

func TestTranscriptOmitsDurableState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 2)})
	if err != nil {
		t.Fatal(err)
	}
	state := json.RawMessage(`{"path":"a.txt","sha256":"abc"}`)
	if err := recorder.Event(time.Unix(3, 4), agent.Event{
		Kind:  agent.StateChangeEvent,
		ID:    "call",
		Name:  "file_read",
		State: state,
	}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	transcript := string(stored)
	for _, unwanted := range []string{"## State", "`call`", "`file_read`", string(state)} {
		if strings.Contains(transcript, unwanted) {
			t.Errorf("expected %q to be left out of the transcript, got:\n%s", unwanted, transcript)
		}
	}
}

func TestTranscriptRecordsWhatASilentTurnSaid(t *testing.T) {
	written := transcriptOfOneEvent(t, agent.Event{Kind: agent.SilentTurnEvent})

	for _, want := range []string{"## Silent turn", agent.SilentTurnNotice} {
		if !strings.Contains(written, want) {
			t.Errorf("expected %q in the transcript, got:\n%s", want, written)
		}
	}
}

func TestTranscriptRendersPathGrantEventsFromStructuredState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 2)})
	if err != nil {
		t.Fatal(err)
	}
	event, err := pathgrant.ChangeEvent("/reference", []pathgrant.Grant{{
		Path:   "/reference",
		Access: pathgrant.ReadAccess | pathgrant.WriteAccess,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), event); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	transcript := string(stored)
	for _, want := range []string{
		"## Path grant · 1 path · changed /reference · +2s",
		"Granted temporary read and write access to /reference.",
	} {
		if !strings.Contains(transcript, want) {
			t.Errorf("expected %q in the transcript, got:\n%s", want, transcript)
		}
	}
}

func TestTranscriptRecordsBothPortDirections(t *testing.T) {
	hostToSandbox, err := portgrant.HostToSandboxChangeEvent("127.9.9.9", 8080, []portgrant.Route{{Port: 8080}})
	if err != nil {
		t.Fatal(err)
	}
	sandboxToHost, err := portgrant.SandboxToHostChangeEvent(3000, []uint16{3000})
	if err != nil {
		t.Fatal(err)
	}

	for name, test := range map[string]struct {
		event agent.Event
		want  []string
	}{
		"host to sandbox": {event: hostToSandbox, want: []string{
			"## Host → sandbox · 1 sandbox port · changed port 8080",
			"Sandbox port 8080 exposed at http://127.9.9.9:8080.",
		}},
		"sandbox to host": {event: sandboxToHost, want: []string{
			"## Sandbox → host · 1 host port · changed host port 3000",
			"Host loopback port 3000 exposed to sandbox.",
		}},
	} {
		t.Run(name, func(t *testing.T) {
			written := transcriptOfOneEvent(t, test.event)
			for _, want := range test.want {
				if !strings.Contains(written, want) {
					t.Errorf("expected %q in the transcript, got:\n%s", want, written)
				}
			}
		})
	}
}

func TestTranscriptHoldsTheHeaderApartFromWhatFollowsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.md")
	recorder, err := transcript.Open(path, transcript.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 2), Workspace: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(3, 4), agent.Event{
		Kind: agent.SilentTurnEvent,
	}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(5, 6), caps.ModeEvent(caps.Read|caps.Shell)); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Event(time.Unix(7, 8), agent.Event{Kind: agent.UserMessageEvent, Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	transcript := string(stored)
	for _, want := range []string{
		"- **Workspace:** `/workspace`\n- **Tool detail:** `jq 'select(.event.id == \"<id>\")' session.jsonl`, for the `[id]` of any call\n\n## Silent turn · +2s\n",
		"## Mode · rx · +4s\n\n## User · +6s\n",
	} {
		if !strings.Contains(transcript, want) {
			t.Errorf("expected %q in the transcript, got:\n%s", want, transcript)
		}
	}
}

func TestTranscriptEndsWithOneNewlineHoweverMuchWasWritten(t *testing.T) {
	for name, events := range map[string][]agent.Event{
		"nothing beyond the header": nil,
		"a message": {
			{Kind: agent.ModelMessageEvent, Text: "answer"},
		},
		"a message the model padded": {
			{Kind: agent.ModelMessageEvent, Text: "answer\n\n\n"},
		},
		"a fenced message": {
			{Kind: agent.ModelMessageEvent, Text: "# heading\n\n```\ncode\n```\n"},
		},
		"a tool call": {
			{Kind: agent.ToolCallRequestEvent, ID: "call-1", Name: "read", Text: `{"path":"a.txt"}`},
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "transcript.md")
			recorder, err := transcript.Open(path, transcript.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 2)})
			if err != nil {
				t.Fatal(err)
			}
			for at, event := range events {
				if err := recorder.Event(time.Unix(int64(3+at), 0), event); err != nil {
					t.Fatal(err)
				}
			}
			if err := recorder.Close(); err != nil {
				t.Fatal(err)
			}

			stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
			if err != nil {
				t.Fatal(err)
			}

			written := string(stored)
			trailing := len(written) - len(strings.TrimRight(written, "\n"))
			if trailing != 1 {
				t.Errorf("ended with %d newlines, want exactly one:\n%q", trailing, written)
			}
		})
	}
}
