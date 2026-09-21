package painter

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/markdown"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/startup"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/width"
	"crdx.org/oh/pkg/agent"
)

func TestStartupDrawingUsesTheScreensTextSizingSupport(t *testing.T) {
	for name, isSupported := range map[string]bool{"supported": true, "unsupported": false} {
		t.Run(name, func(t *testing.T) {
			var screenOutput bytes.Buffer
			screen := output.NewTerminalOfSize(&screenOutput, 80, 24)
			screen.SetTextSizingSupported(isSupported)
			paint := New(screen, false, nil, nil, output.StreamingModeLine)
			paint.DrawEvent(startup.NewEvent(time.Millisecond, startup.Info{Session: "tame-impala"}))

			got := strings.Contains(screenOutput.String(), "\x1b]66;")
			if got != isSupported {
				t.Errorf("sized startup presence = %t, want %t", got, isSupported)
			}
		})
	}
}

func TestPathGrantEventsAreDrawnFromTheirStructuredState(t *testing.T) {
	var screenOutput bytes.Buffer
	paint := New(output.NewTerminalOfSize(&screenOutput, 80, 24), false, nil, nil, output.StreamingModeLine)
	event, err := pathgrant.ChangeEvent("/reference", []pathgrant.Grant{{
		Path:   "/reference",
		Access: pathgrant.ReadAccess,
	}})
	if err != nil {
		t.Fatal(err)
	}

	paint.DrawEvent(event)
	if drawn := style.Plain(screenOutput.String()); !strings.Contains(drawn, "Granted temporary read-only access to /reference.") {
		t.Errorf("got drawing %q", drawn)
	}
}

func TestSubmittedMessagePresentationFollowsItsKind(t *testing.T) {
	for name, testCase := range map[string]struct {
		kind       submissionKind
		marker     string
		background style.Style
	}{
		"user":            {kind: userSubmission, background: style.User},
		"pending harness": {kind: pendingHarnessSubmission, marker: unsentMark + " ", background: style.Harness},
		"sent harness":    {kind: sentHarnessSubmission, marker: harnessMark + " ", background: style.Harness},
	} {
		t.Run(name, func(t *testing.T) {
			message := submittedMessage{kind: testCase.kind}
			if got := message.marker(); got != testCase.marker {
				t.Errorf("marker = %q, want %q", got, testCase.marker)
			}
			if got, want := message.background()("text"), testCase.background("text"); got != want {
				t.Errorf("background = %q, want %q", got, want)
			}
		})
	}
}

func TestOnlyTerminalConversationMessagesContainHyperlinks(t *testing.T) {
	for eventName, kind := range map[string]agent.Kind{
		"assistant": agent.ModelMessageEvent,
		"user":      agent.UserMessageEvent,
	} {
		for streamName, isTerminal := range map[string]bool{"terminal": true, "stream": false} {
			t.Run(eventName+"/"+streamName, func(t *testing.T) {
				var screenOutput bytes.Buffer
				screen := output.New(&screenOutput)
				if isTerminal {
					screen = output.NewTerminalOfSize(&screenOutput, 80, 24)
				}
				paint := New(screen, false, nil, nil, output.StreamingModeLine)
				paint.DrawEvent(agent.Event{Kind: kind, Text: "[docs](https://example.test)"})

				hasHyperlink := strings.Contains(screenOutput.String(), "\x1b]8;;https://example.test\x1b\\")
				if hasHyperlink != isTerminal {
					t.Errorf("hyperlink presence = %t in %q", hasHyperlink, screenOutput.String())
				}
			})
		}
	}
}

func TestToolCallPathsAreLinkedToTheModelFacingFile(t *testing.T) {
	scratchDirectory := t.TempDir()
	path := filepath.Join(scratchDirectory, "candle-shots", "dl-sorted.png")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("prepare directory: %v", err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("prepare file: %v", err)
	}

	var screenOutput bytes.Buffer
	screen := output.NewTerminalOfSize(&screenOutput, 80, 24).LinkPathsUnder(link.Roots{Scratch: scratchDirectory})
	paint := New(screen, false, nil, nil, output.StreamingModeLine)
	paint.DrawEvent(agent.Event{
		Kind: agent.ToolCallRequestEvent,
		ID:   "call-1",
		Name: "read",
		FallbackRendering: agent.FallbackRendering{
			Subject:  "/tmp/candle-shots/dl-sorted.png",
			ReadOnly: true,
		},
	})

	wantTarget := "file://" + filepath.ToSlash(path)
	if !strings.Contains(screenOutput.String(), "\x1b]8;;"+wantTarget+"\x1b\\") {
		t.Errorf("got drawing %q, want link to %q", screenOutput.String(), wantTarget)
	}
}

func TestModelReasoningPathsAreLinkedToTheModelFacingFile(t *testing.T) {
	scratchDirectory := t.TempDir()
	path := filepath.Join(scratchDirectory, "notes.txt")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("prepare file: %v", err)
	}

	var screenOutput bytes.Buffer
	screen := output.NewTerminalOfSize(&screenOutput, 80, 24).LinkPathsUnder(link.Roots{Scratch: scratchDirectory})
	paint := New(screen, false, nil, nil, output.StreamingModeLine)
	paint.DrawEvent(agent.Event{Kind: agent.ModelReasoningEvent, Text: "checking /tmp/notes.txt"})

	wantTarget := "file://" + filepath.ToSlash(path)
	if !strings.Contains(screenOutput.String(), "\x1b]8;;"+wantTarget+"\x1b\\") {
		t.Errorf("got drawing %q, want link to %q", screenOutput.String(), wantTarget)
	}
}

func TestQueuedMessagePathsAreLinkedAsHostPaths(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "notes.txt")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("prepare file: %v", err)
	}

	roots := link.Roots{Workspace: workspace}
	renderings := map[string]string{
		"footer": strings.Join(RenderQueuedMessages([]string{"check notes.txt"}, true, 80, true, roots), "\n"),
		"notice": strings.Join(NewPendingMessages([]string{"check notes.txt"}, true, roots).Rows(80), "\n"),
	}

	wantTarget := "file://" + filepath.ToSlash(path)
	for name, rendering := range renderings {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(rendering, "\x1b]8;;"+wantTarget+"\x1b\\") {
				t.Errorf("got drawing %q, want link to %q", rendering, wantTarget)
			}
		})
	}
}

func TestPendingMessagesShareOneBlock(t *testing.T) {
	rows := NewPendingMessages([]string{"one", "two"}, false, link.Roots{}).Rows(40)

	if len(rows) != 4 {
		t.Fatalf("got %d rows, want the hint, both messages, and a pad: %q", len(rows), rows)
	}
	if !strings.Contains(rows[1], "one") || !strings.Contains(rows[2], "two") {
		t.Errorf("got rows %q, want the messages beside each other", rows)
	}
}

func TestStandingNoticesSayHowToSendThemNow(t *testing.T) {
	pending := NewPendingMessages([]string{"one"}, false, link.Roots{})

	standing := pending.Rows(40)
	if !strings.Contains(style.Plain(standing[0]), sendHint) {
		t.Errorf("got rows %q, want the hint above the standing notice", standing)
	}

	pending.MarkSent()
	for _, row := range pending.Rows(40) {
		if strings.Contains(style.Plain(row), sendHint) {
			t.Errorf("got rows %q, want no hint once the notices have gone", pending.Rows(40))
		}
	}
}

func TestQueuedMessagesSayHowToSendThemNow(t *testing.T) {
	rows := RenderQueuedMessages([]string{"one"}, true, 40, false, link.Roots{})

	if !strings.Contains(style.Plain(rows[0]), sendHint) {
		t.Errorf("got rows %q, want the hint above the queued message", rows)
	}
}

func TestAQueueAwaitingAStoppingTurnSaysSoRatherThanOfferingToSendItNow(t *testing.T) {
	rows := RenderQueuedMessages([]string{"one"}, false, 40, false, link.Roots{})

	if !strings.Contains(style.Plain(rows[0]), stoppingHint) {
		t.Errorf("got rows %q, want the stopping hint above the queued message", rows)
	}
	if strings.Contains(style.Plain(rows[0]), sendHint) {
		t.Errorf("got rows %q, want no offer to send what cannot be sent yet", rows)
	}
	if !strings.Contains(style.Plain(rows[1]), unsentMark+" one") {
		t.Errorf("got rows %q, want the message still standing unsent", rows)
	}
}

func TestTheSendHintIsDroppedWhenItDoesNotFit(t *testing.T) {
	renderings := map[string][]string{
		"notices": NewPendingMessages([]string{"one"}, false, link.Roots{}).Rows(len(sendHint)),
		"queue":   RenderQueuedMessages([]string{"one"}, true, len(sendHint), false, link.Roots{}),
	}

	for name, rows := range renderings {
		t.Run(name, func(t *testing.T) {
			if len(rows) != 3 {
				t.Errorf("got %d rows, want a pad, the message, and a pad: %q", len(rows), rows)
			}
			if strings.Contains(style.Plain(rows[0]), sendHint) {
				t.Errorf("got rows %q, want no hint where it does not fit", rows)
			}
		})
	}
}

func TestConsecutiveHarnessNoticesShareOneBlock(t *testing.T) {
	var screenOutput strings.Builder
	screen := output.NewTerminalOfSize(&screenOutput, 80, 24)
	paint := New(screen, false, nil, nil, output.StreamingModeLine)

	paint.DrawEvent(caps.ModeToggleEvent(caps.Write, caps.Read))
	paint.DrawEvent(caps.ModeToggleEvent(caps.Git, caps.Read|caps.Git))
	screen.End()

	drawn := screenOutput.String()
	first := strings.Index(drawn, "read-only")
	second := strings.Index(drawn, "read-write")
	if first < 0 || second < 0 {
		t.Fatalf("got drawing %q, want both notices", drawn)
	}
	if between := drawn[first:second]; strings.Count(between, "\n") > 1 {
		t.Errorf("got %q between the notices, want them in one block", between)
	}
}

func TestNoUnfinishedLineEverWithdrawsARowThatWasDrawn(t *testing.T) {
	answers := map[string]string{
		"a bullet marker":       "Here is the list.\n\n- one\n- two\n- three\n\nAnd after it.\n",
		"a nested marker":       "Here is the list.\n\n- one\n  - deeper\n  - deeper still\n- two\n\nAnd after it.\n",
		"an ordered marker":     "Here is the list.\n\n1. one\n2. two\n3. three\n\nAnd after it.\n",
		"a task marker":         "Here is the list.\n\n- [ ] one\n- [x] two\n\nAnd after it.\n",
		"a quoted marker":       "Here is the quote.\n\n> - one\n> - two\n\nAnd after it.\n",
		"a heading marker":      "Here is the heading.\n\n## One\n\nAnd after it.\n",
		"an indented code line": "Here is the code.\n\n    one = 1\n    two = 2\n\nAnd after it.\n",
		"a table row":           "Here is the table.\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\nAnd after it.\n",
	}

	for name, answer := range answers {
		t.Run(name, func(t *testing.T) {
			for _, deltaRunes := range []int{1, 2, 3, 4, 5, 7} {
				t.Run(strconv.Itoa(deltaRunes), func(t *testing.T) {
					streamWithoutWithdrawing(t, answer, deltaRunes)
				})
			}
		})
	}
}

func streamWithoutWithdrawing(t *testing.T, answer string, deltaRunes int) {
	t.Helper()

	const columns = 40

	var screenOutput bytes.Buffer

	paint := New(output.NewTerminalOfSize(&screenOutput, columns, 24), false, nil, nil, output.StreamingModeLine)

	runes := []rune(answer)
	drawnRowCount := 0

	var arrived strings.Builder

	for at := 0; at < len(runes); at += deltaRunes {
		delta := string(runes[at:min(at+deltaRunes, len(runes))])
		arrived.WriteString(delta)

		screenOutput.Reset()
		paint.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: delta})

		rendered := len(markdown.Render(arrived.String(), columns))

		if paint.answer.drawnRowCount < drawnRowCount && rendered >= drawnRowCount {
			t.Errorf(
				"delta %q took the drawn rows from %d down to %d while %d were rendered",
				delta, drawnRowCount, paint.answer.drawnRowCount, rendered,
			)
		}
		drawnRowCount = paint.answer.drawnRowCount

		if drawsNothingButErases(screenOutput.String()) {
			t.Errorf("delta %q sent a frame that erased without drawing: %q", delta, screenOutput.String())
		}
	}
}

func drawsNothingButErases(frame string) bool {
	payload := style.Plain(frame)
	for _, wrapper := range []string{"\x1b[?2026h", "\x1b[?2026l", "\x1b[?25l", "\x1b[?25h", "\x1b[?7l"} {
		payload = strings.ReplaceAll(payload, wrapper, "")
	}

	if payload == "" || !strings.Contains(frame, "\x1b[K") && !strings.Contains(frame, "\x1b[J") {
		return false
	}

	return strings.TrimLeft(cursorMotion.ReplaceAllString(payload, ""), "\r") == ""
}

var cursorMotion = regexp.MustCompile(`\x1b\[[0-9]*[ABCDJK]`)

func TestToolResultLinksAreOptIn(t *testing.T) {
	for name, test := range map[string]struct {
		sessionName string
		resultText  string
		shouldLink  bool
	}{
		"linked":             {sessionName: "tame-impala", resultText: "output", shouldLink: true},
		"empty result":       {sessionName: "tame-impala", shouldLink: true},
		"plain":              {resultText: "output"},
		"plain empty result": {},
	} {
		t.Run(name, func(t *testing.T) {
			var screenOutput bytes.Buffer
			paint := New(output.NewTerminalOfSize(&screenOutput, 80, 24), false, nil, nil, output.StreamingModeLine)
			paint.LinkToolResults(test.sessionName)
			paint.DrawEvent(agent.Event{Kind: agent.ToolCallRequestEvent, ID: "call-1", Name: "read"})
			paint.DrawEvent(agent.Event{Kind: agent.ToolCallResultEvent, ID: "call-1", Status: agent.SuccessStatus, Text: test.resultText})
			hasLink := strings.Contains(screenOutput.String(), "\x1b]8;;oh://tool-result?")
			if hasLink != test.shouldLink {
				t.Errorf("link presence = %t in %q", hasLink, screenOutput.String())
			}
		})
	}
}

func TestNewQuestionClosesOldToolBlockAndResetsRows(t *testing.T) {
	paint := New(output.New(&bytes.Buffer{}), false, nil, nil, output.StreamingModeLine)
	paint.DrawEvent(agent.Event{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "one.go"}})
	paint.DrawEvent(agent.Event{Kind: agent.UserMessageEvent, Text: "never mind"})
	if paint.toolBlock != nil {
		t.Fatal("old tool block remained open")
	}

	paint.DrawEvent(agent.Event{Kind: agent.ToolCallRequestEvent, ID: "2", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "two.go"}})
	if row := paint.rows["2"]; row != 0 {
		t.Errorf("new block started at row %d", row)
	}
}

func TestHarnessAsideKeepsToolBlockOpen(t *testing.T) {
	paint := New(output.New(&bytes.Buffer{}), true, nil, nil, output.StreamingModeLine)
	paint.DrawEvent(agent.Event{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "read"})
	paint.DrawEvent(agent.Event{Kind: agent.SilentTurnEvent})
	if paint.toolBlock == nil {
		t.Fatal("aside closed the tool block")
	}
}

func TestARetryIsDrawnFromWhatItWasRatherThanFromWhatItSaid(t *testing.T) {
	var screenOutput bytes.Buffer

	paint := New(output.New(&screenOutput), false, nil, nil, output.StreamingModeLine)
	paint.DrawEvent(agent.Event{
		Kind:    agent.RetryingEvent,
		Text:    "The stream ended before the response did\nand a second line nobody needs",
		Attempt: 2,
		Took:    500 * time.Millisecond,
	})

	drawn := style.Plain(screenOutput.String())

	for _, want := range []string{
		"[#2] Request failed",
		"retrying in 0.5s",
		"The stream ended before the response did",
	} {
		if !strings.Contains(drawn, want) {
			t.Errorf("expected %q to be drawn, got %q", want, drawn)
		}
	}

	if strings.Contains(drawn, "nobody needs") {
		t.Errorf("expected only the first line of what stopped it, got %q", drawn)
	}
}

func TestARetryShowsTheCallThatProvokedIt(t *testing.T) {
	var screenOutput bytes.Buffer

	paint := New(output.New(&screenOutput), false, nil, nil, output.StreamingModeLine)
	paint.DrawEvent(agent.Event{
		Kind:      agent.RetryingEvent,
		Text:      "The read tool call did not contain a JSON object",
		Name:      "read",
		Arguments: `{"path": "one.go",, "limit": 20}`,
		Attempt:   1,
	})

	drawn := style.Plain(screenOutput.String())

	if !strings.Contains(drawn, "[#1] Request failed; retrying") {
		t.Errorf("expected the first retry to show its attempt number, got %q", drawn)
	}
	if !strings.Contains(drawn, `{"path": "one.go",, "limit": 20}`) {
		t.Errorf("expected the call that provoked the retry to be drawn, got %q", drawn)
	}
}

func TestALongFaultedCallIsCutRatherThanDrawnWhole(t *testing.T) {
	var screenOutput bytes.Buffer

	arguments := `{"text": "` + strings.Repeat("x", 4*retryArgumentsCells) + `"}`

	paint := New(output.New(&screenOutput), false, nil, nil, output.StreamingModeLine)
	paint.DrawEvent(agent.Event{
		Kind:      agent.RetryingEvent,
		Text:      "The write tool call did not contain a JSON object",
		Name:      "write",
		Arguments: arguments,
		Attempt:   1,
	})

	drawn := style.Plain(screenOutput.String())

	if strings.Contains(drawn, arguments) {
		t.Error("expected a long call to be cut rather than drawn whole")
	}
	if !strings.Contains(drawn, width.Ellipsis) {
		t.Errorf("expected the cut to be marked, got %q", drawn)
	}
	if !strings.Contains(drawn, `{"text": "xxx`) {
		t.Errorf("expected what was kept to start the call, got %q", drawn)
	}
}

func TestRenderContextExceededAdvisesForkingWithTheSameModel(t *testing.T) {
	event := agent.Event{
		Kind: agent.FailureEvent,
		Failure: &agent.Failure{
			Kind:       agent.HTTPStatusFailure,
			HTTPStatus: 400,
			Code:       "context_length_exceeded",
			Message:    "the prompt is bigger than the window",
		},
	}

	notice, isSaid := RenderContextExceeded(event, "qwen4:70b")
	if !isSaid {
		t.Fatal("expected the notice to be said")
	}
	if !strings.Contains(notice, "/fork qwen4:70b") {
		t.Errorf("expected the model to be named for the fork, got %q", notice)
	}
}

func TestRenderContextExceededNamesNoForkWithoutAModel(t *testing.T) {
	event := agent.Event{
		Kind: agent.FailureEvent,
		Failure: &agent.Failure{
			Kind:       agent.HTTPStatusFailure,
			HTTPStatus: 400,
			Code:       "context_length_exceeded",
		},
	}

	notice, isSaid := RenderContextExceeded(event, "")
	if !isSaid {
		t.Fatal("expected the notice to be said")
	}
	if strings.Contains(notice, forkCommand) {
		t.Errorf("expected no fork to be advised without a model, got %q", notice)
	}
}

func TestRenderContextExceededStaysSilentForOtherFailures(t *testing.T) {
	for name, event := range map[string]agent.Event{
		"another refusal": {
			Kind:    agent.FailureEvent,
			Failure: &agent.Failure{Kind: agent.HTTPStatusFailure, HTTPStatus: 404},
		},
		"no failure at all": {Kind: agent.FailureEvent, Text: "the context length is fine"},
	} {
		t.Run(name, func(t *testing.T) {
			if notice, isSaid := RenderContextExceeded(event, "qwen4:70b"); isSaid {
				t.Errorf("expected silence, got %q", notice)
			}
		})
	}
}
