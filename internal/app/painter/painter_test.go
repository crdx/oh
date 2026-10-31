package painter

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/experimental"
	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/markdown"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/startup"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/subagentrecord"
	"crdx.org/oh/internal/app/taskrecord"
	"crdx.org/oh/internal/app/width"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/money"
	"crdx.org/oh/internal/toolresult"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tasks"
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

func TestSubmittedBoxesReserveBothSideMarginsWhenTextWraps(t *testing.T) {
	for _, columns := range []int{8, 12, 24, 40, 100} {
		for name, text := range map[string]string{
			"prose":    strings.Repeat("word ", columns),
			"unbroken": strings.Repeat("x", columns*3),
			"list":     "- " + strings.Repeat("word ", columns),
		} {
			for kindName, kind := range map[string]submissionKind{
				"user":            userSubmission,
				"pending harness": pendingHarnessSubmission,
				"sent harness":    sentHarnessSubmission,
			} {
				t.Run(name+"/"+kindName+"/"+strconv.Itoa(columns), func(t *testing.T) {
					message := submittedMessage{kind: kind, text: text}
					content := submittedContentRows(message, columns, false, link.Roots{})
					rows := frameSubmitted(content, "", columns, message.background())
					if len(content) < 2 {
						t.Fatalf("want wrapped content, got %q", content)
					}
					for _, row := range rows[1 : len(rows)-1] {
						plain := style.Plain(row)
						if width.Of(plain) != columns || !strings.HasPrefix(plain, " ") || !strings.HasSuffix(plain, " ") {
							t.Errorf("content row %q has no left and right margin at %d columns", plain, columns)
						}
					}
				})
			}
		}
	}
}

func TestAUserMessageThatMarkdownHidesStaysVisible(t *testing.T) {
	for _, text := range []string{"1.", "42)", "-", "+", "*", "#", ">", "```", "[reference]: https://example.test"} {
		for _, columns := range []int{1, 8, 80} {
			for _, shouldRenderHyperlinks := range []bool{false, true} {
				t.Run(text+"/"+strconv.Itoa(columns)+"/"+strconv.FormatBool(shouldRenderHyperlinks), func(t *testing.T) {
					rows := submittedContentRows(submittedMessage{text: text}, columns, shouldRenderHyperlinks, link.Roots{})
					var visible strings.Builder
					for _, row := range rows {
						visible.WriteString(strings.ReplaceAll(strings.TrimSpace(style.Plain(row)), " ", ""))
					}
					if got, want := visible.String(), strings.ReplaceAll(text, " ", ""); got != want {
						t.Errorf("visible text = %q, want %q", got, want)
					}
				})
			}
		}
	}
}

func TestVisibleUserMarkdownKeepsItsRendering(t *testing.T) {
	for _, text := range []string{"1. first\n2. second", "# heading", "**bold**", "word\n\n1.", "---"} {
		for _, shouldRenderHyperlinks := range []bool{false, true} {
			rows := submittedContentRows(submittedMessage{text: text}, 80, shouldRenderHyperlinks, link.Roots{})
			want := markdown.Render(text, 78)
			for i := range want {
				want[i] = " " + want[i]
			}
			if !slices.Equal(rows, want) {
				t.Errorf("%q rendered as %q, want %q", text, rows, want)
			}
		}
	}
}

func TestOnlyUserMessagesFallBackToPlainText(t *testing.T) {
	for _, kind := range []submissionKind{pendingHarnessSubmission, sentHarnessSubmission} {
		rows := submittedContentRows(submittedMessage{kind: kind, text: "1."}, 80, false, link.Roots{})
		if len(rows) != 0 {
			t.Errorf("harness submission %d rendered as %q, want no rows", kind, rows)
		}
	}
}

func TestAWrappedMissingPathDoesNotLinkItsExistingPrefix(t *testing.T) {
	workspace := t.TempDir()
	parent := filepath.Join(workspace, "parent")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	missingPath := filepath.Join(parent, "missing.patch")
	rows := submittedContentRows(
		submittedMessage{text: missingPath},
		style.Width(parent)+1,
		true,
		link.Roots{Workspace: workspace},
	)

	if rendered := strings.Join(rows, "\n"); strings.Contains(rendered, "\x1b]8;;file://") {
		t.Errorf("wrapped missing path gained a link to its existing prefix in %q", rendered)
	}
}

func TestAWrappedMissingAnswerPathDoesNotLinkItsExistingPrefix(t *testing.T) {
	workspaceDirectory := t.TempDir()
	parent := filepath.Join(workspaceDirectory, "parent")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	missingPath := filepath.Join(parent, "missing.patch")

	var screenOutput bytes.Buffer
	screen := output.NewTerminalOfSize(&screenOutput, style.Width(parent), 24).
		LinkPathsUnder(link.Roots{Workspace: workspaceDirectory})
	paint := New(screen, false, nil, work.At(workspaceDirectory), output.StreamingModeLine)
	paint.DrawEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: missingPath})

	if rendered := screenOutput.String(); strings.Contains(rendered, "\x1b]8;;file://") {
		t.Errorf("wrapped missing path gained a link to its existing prefix in %q", rendered)
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

func TestPlainReasoningLinksNothing(t *testing.T) {
	scratchDirectory := t.TempDir()
	if err := os.WriteFile(filepath.Join(scratchDirectory, "notes.txt"), nil, 0o600); err != nil {
		t.Fatalf("prepare file: %v", err)
	}

	var screenOutput bytes.Buffer
	screen := output.NewTerminalOfSize(&screenOutput, 80, 24).LinkPathsUnder(link.Roots{Scratch: scratchDirectory})
	paint := New(screen, false, nil, nil, output.StreamingModeLine)
	paint.RenderReasoningAs(output.ReasoningPlain)
	paint.DrawEvent(agent.Event{
		Kind: agent.ModelReasoningEvent,
		Text: "checking /tmp/notes.txt against <https://example.test/notes> and https://example.test/more",
	})

	if drawn := screenOutput.String(); strings.Contains(drawn, "\x1b]8;;") {
		t.Errorf("plain reasoning gained a link in %q", drawn)
	}
}

func TestMarkdownReasoningPathsAreLinkedToTheModelFacingFile(t *testing.T) {
	scratchDirectory := t.TempDir()
	path := filepath.Join(scratchDirectory, "notes.txt")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("prepare file: %v", err)
	}

	var screenOutput bytes.Buffer
	screen := output.NewTerminalOfSize(&screenOutput, 80, 24).LinkPathsUnder(link.Roots{Scratch: scratchDirectory})
	paint := New(screen, false, nil, nil, output.StreamingModeLine)
	paint.RenderReasoningAs(output.ReasoningMarkdown)
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
		"footer": strings.Join(RenderQueue([]string{"check notes.txt"}, nil, true, 80, true, roots).Rows, "\n"),
		"notice": strings.Join(NewPendingMessages(notices("check notes.txt"), true, roots).Rows(80), "\n"),
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
	rows := NewPendingMessages(notices("one", "two"), false, link.Roots{}).Rows(40)

	if len(rows) != 4 {
		t.Fatalf("got %d rows, want the hint, both messages, and a pad: %q", len(rows), rows)
	}
	if !strings.Contains(rows[1], "one") || !strings.Contains(rows[2], "two") {
		t.Errorf("got rows %q, want the messages beside each other", rows)
	}
	if !strings.Contains(style.Plain(rows[3]), sendHint) {
		t.Errorf("got rows %q, want the send hint last", rows)
	}
}

func TestStandingNoticesSayHowToSendThemNow(t *testing.T) {
	pending := NewPendingMessages(notices("one"), false, link.Roots{})

	standing := pending.Rows(40)
	if !strings.Contains(style.Plain(standing[len(standing)-1]), sendHint) {
		t.Errorf("got rows %q, want the hint below the standing notice", standing)
	}
	if strings.Contains(style.Plain(standing[0]), sendHint) {
		t.Errorf("got rows %q, want the hint only below the standing notice", standing)
	}

	pending.MarkSent()
	for _, row := range pending.Rows(40) {
		if strings.Contains(style.Plain(row), sendHint) {
			t.Errorf("got rows %q, want no hint once the notices have gone", pending.Rows(40))
		}
	}
}

func TestQueuedMessagesSayHowToSendThemNow(t *testing.T) {
	queue := RenderQueue([]string{"one"}, nil, true, 40, false, link.Roots{})

	if queue.Hint != sendHint {
		t.Errorf("got hint %q, want %q", queue.Hint, sendHint)
	}
}

func TestAQueueAwaitingAStoppingTurnSaysSoRatherThanOfferingToSendItNow(t *testing.T) {
	queue := RenderQueue([]string{"one"}, nil, false, 40, false, link.Roots{})

	if queue.Hint != stoppingHint {
		t.Errorf("got hint %q, want %q", queue.Hint, stoppingHint)
	}
	if len(queue.Rows) != 1 || style.Plain(queue.Rows[0]) != unsentMark+" one" {
		t.Errorf("got rows %q, want the message still standing unsent", queue.Rows)
	}
}

func TestAnEmptyQueueDrawsNothing(t *testing.T) {
	if queue := RenderQueue(nil, nil, true, 40, false, link.Roots{}); len(queue.Rows) != 0 || queue.Hint != "" {
		t.Errorf("got %+v, want nothing", queue)
	}
}

func TestQueuedNoticesStandDimBeforeTheMessages(t *testing.T) {
	queue := RenderQueue([]string{"mine"}, []string{"harness"}, true, 40, false, link.Roots{})

	if len(queue.Rows) != 2 {
		t.Fatalf("got rows %q, want the notice and the message", queue.Rows)
	}
	if queue.Rows[0] != style.Subtle.Over(unsentMark+" harness") {
		t.Errorf("got notice row %q, want it dim", queue.Rows[0])
	}
	if queue.Rows[1] != unsentMark+" mine" {
		t.Errorf("got message row %q, want it plain", queue.Rows[1])
	}
}

func TestAQueuedRowIsElidedToItsColumns(t *testing.T) {
	queue := RenderQueue([]string{"a message far too long for its row"}, nil, true, 12, false, link.Roots{})

	if got := style.Width(queue.Rows[0]); got > 12 {
		t.Errorf("got a row %d columns wide, want at most 12: %q", got, queue.Rows[0])
	}
}

func TestTheSendHintIsDroppedWhenItDoesNotFit(t *testing.T) {
	renderings := map[string][]string{
		"notices": NewPendingMessages(notices("one"), false, link.Roots{}).Rows(len(sendHint)),
	}

	for name, rows := range renderings {
		t.Run(name, func(t *testing.T) {
			if len(rows) != 3 {
				t.Errorf("got %d rows, want a pad, the message, and a pad: %q", len(rows), rows)
			}
			if slices.ContainsFunc(rows, func(row string) bool {
				return strings.Contains(style.Plain(row), sendHint)
			}) {
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

func TestASubagentCompletionNoticeLinksItsNameButNotItsAlias(t *testing.T) {
	var screenOutput bytes.Buffer
	paint := New(output.NewTerminalOfSize(&screenOutput, 80, 24), false, nil, nil, output.StreamingModeLine)
	paint.LinkToolResults("tame-impala")
	paint.DrawEvent(subagentrecord.StartedEvent(
		"tame-adder",
		"child-session-id",
		"Reading the README",
		subagentrecord.Origin{Alias: "goose-two"},
	))
	paint.DrawEvent(subagentrecord.DeliveryEvent(
		[]string{"tame-adder"},
		"Subagent tame-adder (goose-two) finished:\n\ntame-adder done: complete",
		[]subagentrecord.Report{{Name: "tame-adder", State: subagentrecord.Done, Answer: "complete"}},
	))

	address := toolresult.SubagentURL(toolresult.SubagentReference{
		SessionName:  "tame-impala",
		SubagentName: "tame-adder",
	})
	linkedName := link.RenderURL("tame-adder", address)
	if want := "Subagent " + linkedName + " (goose-two) has finished:"; !strings.Contains(screenOutput.String(), want) {
		t.Errorf("completion does not contain %q in %q", want, screenOutput.String())
	}
	if strings.Contains(screenOutput.String(), link.RenderURL("tame-adder (goose-two)", address)) {
		t.Errorf("the alias is linked in %q", screenOutput.String())
	}
}

func TestATaskNoticeLinksItsNameAndShowsTheEndsOfItsOutput(t *testing.T) {
	ended := taskrecord.EndedEvent(tasks.Conclusion{Snapshot: tasks.Snapshot{
		ID:      "count",
		Run:     1,
		State:   tasks.Done,
		EndedAt: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC),
		Output:  "first\nsecond\nthird\nfourth\nfifth\nsixth\nseventh\neighth\n",
	}, IsFinal: true})
	toggles := experimental.New(nil)
	for name, test := range map[string]struct {
		sessionName string
		lines       map[string]any
		shown       []string
		hidden      []string
		shouldLink  bool
	}{
		"unset": {
			sessionName: "tame-impala",
			shown:       []string{"first", "fourth", "fifth", "eighth"},
			hidden:      []string{"more lines"},
			shouldLink:  true,
		},
		"none": {
			sessionName: "tame-impala",
			lines:       map[string]any{string(experimental.TaskOutputLines): int64(0)},
			shown:       []string{"Task count finished"},
			hidden:      []string{"first", "eighth", "more lines"},
			shouldLink:  true,
		},
		"three lines each end": {
			sessionName: "tame-impala",
			lines:       map[string]any{string(experimental.TaskOutputLines): int64(3)},
			shown:       []string{"first", "third", "⋮ 2 more lines", "sixth", "eighth"},
			hidden:      []string{"fourth", "fifth"},
			shouldLink:  true,
		},
		"one line each end": {
			sessionName: "tame-impala",
			lines:       map[string]any{string(experimental.TaskOutputLines): int64(1)},
			shown:       []string{"first", "⋮ 6 more lines", "eighth"},
			hidden:      []string{"second", "seventh"},
			shouldLink:  true,
		},
		"unlinked": {
			lines:  map[string]any{string(experimental.TaskOutputLines): int64(3)},
			shown:  []string{"first", "⋮ 2 more lines", "eighth"},
			hidden: []string{"fourth"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			toggles.Replace(test.lines)
			var screenOutput bytes.Buffer
			paint := New(output.NewTerminalOfSize(&screenOutput, 80, 24), false, nil, nil, output.StreamingModeLine)
			paint.ReadTogglesFrom(toggles)
			paint.LinkToolResults(test.sessionName)
			paint.DrawEvent(ended)

			drawn := style.Plain(screenOutput.String())
			if !strings.Contains(drawn, taskMark) || strings.Contains(drawn, harnessMark) {
				t.Errorf("task notice has the wrong mark in %q", drawn)
			}
			for _, line := range test.shown {
				if !strings.Contains(drawn, line) {
					t.Errorf("%q is missing from %q", line, drawn)
				}
			}
			for _, line := range test.hidden {
				if strings.Contains(drawn, line) {
					t.Errorf("%q is drawn in %q", line, drawn)
				}
			}
			address := "\x1b]8;;oh://task-result?ended=1792843200000000000&run=1&session=tame-impala&task=count\x1b\\"
			if hasLink := strings.Contains(screenOutput.String(), address+style.Code("count")); hasLink != test.shouldLink {
				t.Errorf("link presence = %t in %q", hasLink, screenOutput.String())
			}
		})
	}
}

func TestOnlyTaskCompletionsUseTheTaskMark(t *testing.T) {
	toggles := experimental.New(nil)
	for name, test := range map[string]struct {
		event agent.Event
		mark  string
	}{
		"completion": {
			event: taskrecord.EndedEvent(tasks.Conclusion{Snapshot: tasks.Snapshot{ID: "build", Run: 1, State: tasks.Done}}),
			mark:  taskMark,
		},
		"access withdrawal": {event: caps.TaskStopEvent("build", caps.Write)},
		"session ended":     {event: taskrecord.EndedWithSessionEvent([]string{"build"})},
	} {
		notices, isSaid := DrawnNotices(test.event, NoticeDrawing{Introductions: NewIntroductions(), Toggles: toggles})
		if !isSaid || len(notices) != 1 || notices[0].Mark != test.mark {
			t.Errorf("%s produced %+v (%t), want mark %q", name, notices, isSaid, test.mark)
		}
	}
}

func TestATaskNoticeEndsItsStatusWithTheIntent(t *testing.T) {
	ended := taskrecord.EndedEvent(tasks.Conclusion{Snapshot: tasks.Snapshot{
		Invocation: &tasks.Invocation{Arguments: `{"command":"go build ./...","intent":"Building every package once more"}`},
		ID:         "build",
		ToolName:   "bash",
		Run:        1,
		State:      tasks.Failed,
		Failure:    "exit status 1",
		Output:     "main.go:12: undefined: run\n",
	}, IsFinal: true})
	notices, isSaid := DrawnNotices(ended, NoticeDrawing{
		Introductions: NewIntroductions(),
		Toggles:       experimental.New(map[string]any{string(experimental.TaskOutputLines): int64(3)}),
	})
	if !isSaid || len(notices) != 1 || notices[0].Mark != taskMark || notices[0].Intent != "Building every package once more" {
		t.Fatalf("got %+v (%t)", notices, isSaid)
	}
	if strings.Contains(notices[0].Text, "Building") {
		t.Errorf("the intent is part of the notice text %q", notices[0].Text)
	}
	message := submittedMessage{text: notices[0].Text, kind: sentHarnessSubmission, mark: notices[0].Mark, intent: notices[0].Intent}

	wide := submittedContentRows(message, 80, false, link.Roots{})
	if want := "Task build failed: exited 1 (Building every package once more)"; !strings.Contains(style.Plain(wide[0]), want) {
		t.Errorf("first row is %q, want it to hold %q", style.Plain(wide[0]), want)
	}
	if !strings.Contains(wide[0], style.Reasoning("(Building every package once more)")) {
		t.Errorf("the intent is not drawn like a call row's intent in %q", wide[0])
	}
	if !slices.ContainsFunc(wide, func(row string) bool { return strings.Contains(style.Plain(row), "main.go:12: undefined: run") }) {
		t.Errorf("the output block was lost from %q", wide)
	}

	narrow := submittedContentRows(message, 30, false, link.Roots{})
	for _, row := range narrow {
		if style.Width(row) > 30 {
			t.Errorf("row %q is wider than 30 columns", style.Plain(row))
		}
	}
	if joined := style.Plain(strings.Join(narrow, " ")); !strings.Contains(strings.Join(strings.Fields(joined), " "), "exited 1 (Building every package once more)") {
		t.Errorf("the wrapped status lost its intent: %q", joined)
	}
}

func TestASuccessfulTaskNoticePutsItsDurationBeforeItsIntent(t *testing.T) {
	startedAt := time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)
	ended := taskrecord.EndedEvent(tasks.Conclusion{Snapshot: tasks.Snapshot{
		Invocation: &tasks.Invocation{Arguments: `{"intent":"Correcting the golden fixture regeneration"}`},
		ID:         "golden-1",
		ToolName:   "offload",
		Run:        1,
		State:      tasks.Done,
		StartedAt:  startedAt,
		EndedAt:    startedAt.Add(8*time.Minute + 10*time.Second),
	}, IsFinal: true})
	notices, _ := DrawnNotices(ended, NoticeDrawing{Introductions: NewIntroductions()})
	message := submittedMessage{text: notices[0].Text, kind: sentHarnessSubmission, mark: notices[0].Mark, intent: notices[0].Intent}
	rows := submittedContentRows(message, 100, false, link.Roots{})
	if got := strings.TrimSpace(style.Plain(rows[0])); got != "⚙️ Task golden-1 finished after 8m 10s (Correcting the golden fixture regeneration)" {
		t.Errorf("got %q", got)
	}
}

func TestATaskNoticeWithoutAnIntentEndsAtItsStatus(t *testing.T) {
	ended := taskrecord.EndedEvent(tasks.Conclusion{Snapshot: tasks.Snapshot{
		Invocation: &tasks.Invocation{Arguments: `{"pattern":"func Run"}`},
		ID:         "search",
		ToolName:   "grep",
		Run:        1,
		State:      tasks.Done,
	}, IsFinal: true})
	notices, _ := DrawnNotices(ended, NoticeDrawing{Introductions: NewIntroductions()})
	rows := submittedContentRows(submittedMessage{text: notices[0].Text, kind: sentHarnessSubmission, mark: notices[0].Mark, intent: notices[0].Intent}, 80, false, link.Roots{})
	if got := strings.TrimSpace(style.Plain(rows[0])); got != "⚙️ Task search finished" {
		t.Errorf("got %q", got)
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
	if !strings.Contains(notice, "/fork -m qwen4:70b") {
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

func TestMarkdownReasoningLinksAPathBeforeItWraps(t *testing.T) {
	workspace := t.TempDir()
	relativePath := "somewhere/a-very-long-patch-file-name.patch"
	path := filepath.Join(workspace, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	var renderer markdown.IncrementalRenderer
	var plain plainThought
	var reflow paragraphReflow
	var styles rowMemory
	rows := renderReasoningWith(
		&renderer,
		&plain,
		&reflow,
		&styles,
		"Inspect "+relativePath+" before answering.",
		20,
		output.ReasoningMarkdown,
		true,
		true,
		link.Roots{Workspace: workspace},
	)
	drawn := strings.Join(rows, "\n")
	completeOpening := "\x1b]8;;" + link.PathURL(path) + "\x1b\\"
	completeLinks := strings.Count(drawn, completeOpening)
	if completeLinks < 2 {
		t.Errorf("wrapped path has %d complete links in %q", completeLinks, drawn)
	}
	if fileLinks := strings.Count(drawn, "\x1b]8;;file://"); fileLinks != completeLinks {
		t.Errorf("wrapped path has %d file links but %d complete targets in %q", fileLinks, completeLinks, drawn)
	}
}

func TestWrappedMissingReasoningPathDoesNotLinkItsExistingPrefix(t *testing.T) {
	workspace := t.TempDir()
	parent := filepath.Join(workspace, "parent")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	missingPath := filepath.Join(parent, "missing.patch")

	for name, rendering := range map[string]output.ReasoningRendering{
		"plain":    output.ReasoningPlain,
		"markdown": output.ReasoningMarkdown,
	} {
		t.Run(name, func(t *testing.T) {
			var renderer markdown.IncrementalRenderer
			var plain plainThought
			var reflow paragraphReflow
			var styles rowMemory
			rows := renderReasoningWith(
				&renderer,
				&plain,
				&reflow,
				&styles,
				missingPath,
				style.Width(parent),
				rendering,
				true,
				true,
				link.Roots{Workspace: workspace},
			)
			if drawn := strings.Join(rows, "\n"); strings.Contains(drawn, "\x1b]8;;file://") {
				t.Errorf("wrapped missing path gained a link to its existing prefix in %q", drawn)
			}
		})
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

func TestLinkedReasoningDrawnADeltaAtATimeMatchesAFreshDraw(t *testing.T) {
	workspace := t.TempDir()
	relativePath := "somewhere/a-very-long-patch-file-name.patch"
	path := filepath.Join(workspace, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	thought := "Inspect " + relativePath + " before answering."
	pathRoots := link.Roots{Workspace: workspace}

	for name, rendering := range map[string]output.ReasoningRendering{
		"plain":    output.ReasoningPlain,
		"markdown": output.ReasoningMarkdown,
	} {
		t.Run(name, func(t *testing.T) {
			var renderer markdown.IncrementalRenderer
			var plain plainThought
			var reflow paragraphReflow
			var styles rowMemory

			for at := 1; at <= len(thought); at++ {
				got := renderReasoningWith(
					&renderer,
					&plain,
					&reflow,
					&styles,
					thought[:at],
					20,
					rendering,
					false,
					true,
					pathRoots,
				)

				var freshRenderer markdown.IncrementalRenderer
				var freshPlain plainThought
				var freshReflow paragraphReflow
				var freshStyles rowMemory
				want := renderReasoningWith(
					&freshRenderer,
					&freshPlain,
					&freshReflow,
					&freshStyles,
					thought[:at],
					20,
					rendering,
					false,
					true,
					pathRoots,
				)
				if strings.Join(got, "\n") != strings.Join(want, "\n") {
					t.Fatalf("byte %d drew\n%q\nwant\n%q", at, got, want)
				}
			}
		})
	}
}

func TestReasoningDrawnADeltaAtATimeIsTheReasoningDrawnAtOnce(t *testing.T) {
	const thought = "First  thought,\twith spacing.\n\n> quoted **bold** line\n\n" +
		"| a | b |\n| --- | --- |\n| one | two |\n\n```go\nfunc   kept() {}\n```\n\n" +
		"- listed `code`\n- another\n\n# Heading\n\nA closing paragraph that runs long enough to wrap across rows."

	for name, rendering := range map[string]output.ReasoningRendering{
		"plain":    output.ReasoningPlain,
		"markdown": output.ReasoningMarkdown,
	} {
		t.Run(name, func(t *testing.T) {
			var renderer markdown.IncrementalRenderer
			var plain plainThought
			var reflow paragraphReflow
			var styles rowMemory

			for at := 1; at <= len(thought); at++ {
				got := renderReasoningWith(
					&renderer,
					&plain,
					&reflow,
					&styles,
					thought[:at],
					30,
					rendering,
					false,
					false,
					link.Roots{},
				)

				var freshRenderer markdown.IncrementalRenderer
				var freshPlain plainThought
				var freshReflow paragraphReflow
				var freshStyles rowMemory
				want := renderReasoningWith(
					&freshRenderer,
					&freshPlain,
					&freshReflow,
					&freshStyles,
					thought[:at],
					30,
					rendering,
					false,
					false,
					link.Roots{},
				)
				if strings.Join(got, "\n") != strings.Join(want, "\n") {
					t.Fatalf("byte %d drew\n%q\nwant\n%q", at, got, want)
				}
			}
		})
	}
}

func TestACacheRebuildIsCostedInTheChosenCurrency(t *testing.T) {
	var screenOutput bytes.Buffer
	paint := New(output.NewTerminalOfSize(&screenOutput, 80, 24), false, nil, nil, output.StreamingModeLine)
	paint.ReadTogglesFrom(experimental.New(map[string]any{string(experimental.CacheMissNotices): true}))
	paint.PriceCacheRebuildsAt(Tariff{
		Prices:   &agent.TokenPrices{Input: 5, Output: 25, CacheRead: 0.5, CacheWrite: 6.25},
		Currency: money.In("GBP", 0.5),
	})

	paint.DrawEvent(agent.Event{
		Kind:  agent.CacheRebuildEvent,
		Name:  string(agent.CacheRebuilt),
		Took:  45 * time.Second,
		Usage: &agent.Usage{Cache: &agent.CacheUsage{WriteTokens: 40_000}},
	})

	if want := "Cache rebuilt: 40Kt sent <1m later for £0.12."; !strings.Contains(screenOutput.String(), want) {
		t.Errorf("got %q, want it to contain %q", screenOutput.String(), want)
	}
}

func TestCacheMissNoticesFollowLiveExperimentalToggle(t *testing.T) {
	var screenOutput bytes.Buffer
	paint := New(output.NewTerminalOfSize(&screenOutput, 80, 24), false, nil, nil, output.StreamingModeLine)
	toggles := experimental.New(nil)
	paint.ReadTogglesFrom(toggles)
	event := agent.Event{
		Kind:  agent.CacheRebuildEvent,
		Name:  string(agent.CacheExpired),
		Usage: &agent.Usage{Cache: &agent.CacheUsage{WriteTokens: 40_000}},
	}

	paint.DrawEvent(event)
	if strings.Contains(screenOutput.String(), "Cache expired:") {
		t.Fatal("cache miss notice was drawn with the toggle off")
	}

	toggles.Replace(map[string]any{string(experimental.CacheMissNotices): true})
	paint.DrawEvent(event)
	if !strings.Contains(screenOutput.String(), "Cache expired: 40Kt sent") {
		t.Fatal("cache miss notice was not drawn after enabling the toggle")
	}

	before := screenOutput.Len()
	toggles.Replace(map[string]any{string(experimental.CacheMissNotices): false})
	paint.DrawEvent(event)
	if screenOutput.Len() != before {
		t.Error("cache miss notice was drawn after disabling the toggle")
	}
}

func notices(texts ...string) []Notice {
	drawn := make([]Notice, len(texts))
	for index, text := range texts {
		drawn[index] = Notice{Text: text}
	}
	return drawn
}
