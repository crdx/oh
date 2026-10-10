package harness

import (
	"fmt"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/ansi"
	"crdx.org/oh/internal/app/dynamic"
	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/pkg/agent"
)

const arrivingProseLines = 12

func arrivingProseRig(t *testing.T, mode output.StreamingMode) *replayRig {
	t.Helper()

	rig := newRig(t, func(written *strings.Builder, workspaceDir string) *output.Screen {
		return output.NewTerminalOfSize(written, replayColumns, arrivingProseLines).
			LinkPathsUnder(link.Roots{Workspace: workspaceDir})
	})
	rig.chat.display.streamingMode = mode
	rig.chat.currentTurn = Turn{Stream: testRunningTurnStream(), painter: rig.chat.newPainter(true)}
	rig.chat.recordEvent(agent.Event{Kind: agent.UserMessageEvent, Text: "explain it"})

	return rig
}

func streamArriving(rig *replayRig, kind agent.Kind, text string) {
	for piece := range deltaSized(text) {
		streamDelta(rig.chat, agent.Delta{Kind: kind, Text: piece})
	}
}

func closeArrivingProse(rig *replayRig) string {
	rig.chat.releaseHeldNotices()
	rig.chat.currentTurn.painter.Close(dynamic.Done)
	rig.chat.screen.End()

	return rig.drawn()
}

func replayOfArrivingProse(t *testing.T, rig *replayRig, columns int) string {
	t.Helper()

	return replayAtWidth(t, entriesRecordedBy(rig), columns)
}

func tallProse() string {
	paragraphs := make([]string, 0, 2*arrivingProseLines)
	for index := range cap(paragraphs) {
		paragraphs = append(paragraphs, fmt.Sprintf("Paragraph %d of something taller than the terminal.", index+1))
	}

	return strings.Join(paragraphs, "\n\n")
}

func TestGoldenANoticeArrivingMidProseWaitsForItToLand(t *testing.T) {
	passes := map[string]func() string{}

	for proseName, kind := range map[string]agent.Kind{
		"an answer": agent.ModelMessageEvent,
		"a thought": agent.ModelReasoningEvent,
	} {
		for noticeName := range noticesDrawnBesideAnOpenBlock(nil) {
			name := fmt.Sprintf("%s in the middle of %s", noticeName, proseName)
			arriving, landed := drawNoticesMidProse(t, name, arrivingNotices{
				mode:    output.StreamingModeLine,
				kind:    kind,
				notices: []string{noticeName},
			})

			passes[name+", arriving"] = func() string { return shownInLines(t, arriving, arrivingProseLines) }
			passes[name+", landed"] = func() string { return shown(t, landed, replayColumns) }
		}
	}

	compareWithGolden(t, "notice-mid-prose", ".screen", passes)
}

func TestGoldenAnAnswerThatEndsDrawingNothingLeavesNoRow(t *testing.T) {
	rig := arrivingProseRig(t, output.StreamingModeASAP)
	rig.chat.recordEvent(agent.Event{Kind: agent.ModelReasoningEvent, Text: "Reading the notes first."})
	streamDelta(rig.chat, agent.Delta{Kind: agent.ModelMessageEvent, Text: "0"})
	streamDelta(rig.chat, agent.Delta{Kind: agent.ModelMessageEvent, Text: ")"})
	rig.chat.recordEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: "0)"})
	rig.chat.recordEvent(agent.Event{
		Kind:      agent.ToolCallRequestEvent,
		ID:        "1",
		Name:      "read",
		Arguments: `{"path":"notes.md"}`,
	})
	rig.chat.recordEvent(agent.Event{Kind: agent.ToolCallResultEvent, ID: "1", Name: "read", Text: "notes"})
	landed := closeArrivingProse(rig)

	requireSameVisibleScreen(t, "an answer that ends drawing nothing differs from its replay", replayOfArrivingProse(t, rig, replayColumns), landed)

	compareWithGolden(t, "answer-drawing-nothing", ".screen", map[string]func() string{
		"streamed": func() string { return shown(t, landed, replayColumns) },
	})
}

const (
	guessingColumns = 40
	guessingLines   = 4
)

const guessingLine = "Checking the flag `--yolo first, since it changes what the sandbox allows and every later " +
	"step depends on whether the shell can reach the host network or not, so it has to be settled first. " +
	"It also decides which paths are readable, which ports can be forwarded, and whether a job may outlive its call."

type guessingThought struct {
	before string
	line   string
	rest   string
	mode   output.StreamingMode
}

func TestGoldenAThoughtStillGuessingKeepsItsRowsOutOfScrollback(t *testing.T) {
	plainLine := strings.ReplaceAll(guessingLine, "`", "")
	passes := map[string]func() string{}

	for name, scene := range map[string]guessingThought{
		"an unclosed backtick": {line: guessingLine, rest: "\n\nDone.", mode: output.StreamingModeLine},
		"an unclosed backtick drawn as it arrives": {
			line: guessingLine, rest: "\n\nDone.", mode: output.StreamingModeASAP,
		},
		"an unclosed backtick drawn in paced steps": {
			line: guessingLine, rest: "\n\nDone.", mode: output.StreamingModePaced,
		},
		"an unclosed star": {
			line: strings.Replace(plainLine, "the flag", "the *flag", 1), rest: "\n\nDone.", mode: output.StreamingModeLine,
		},
		"an unfinished autolink": {
			line: "See <https://example.com/" + strings.Repeat("a/rather/long/address/", 8) + "index.html> for it.",
			rest: "\n\nDone.",
			mode: output.StreamingModeLine,
		},
		"a table header": {
			line: "| " + strings.Repeat("a rather long column heading | ", 6),
			rest: "\n|" + strings.Repeat(" --- |", 6) + "\n| 1 | 2 | 3 | 4 | 5 | 6 |\n\nDone.",
			mode: output.StreamingModeLine,
		},
		"a closing fence": {
			before: strings.Repeat("`", 200) + "\ncode\n",
			line:   strings.Repeat("`", 190),
			rest:   strings.Repeat("`", 10) + "\nDone.",
			mode:   output.StreamingModeLine,
		},
		"nothing open": {line: plainLine, rest: "\n\nDone.", mode: output.StreamingModeLine},
	} {
		arriving, landed := drawGuessingThought(t, name, scene)

		passes[name+", arriving"] = func() string {
			return strings.Join(playScreenOfSize(t, arriving, guessingColumns, guessingLines).text(), "\n")
		}
		passes[name+", landed"] = func() string {
			return strings.Join(playScreenOfSize(t, landed, guessingColumns, guessingLines).text(), "\n")
		}
	}

	compareWithGolden(t, "thought-still-guessing", ".screen", passes)
}

func drawGuessingThought(t *testing.T, name string, scene guessingThought) (string, string) {
	t.Helper()

	thought := scene.before + scene.line + scene.rest

	rig := newRig(t, func(written *strings.Builder, workspaceDir string) *output.Screen {
		return output.NewTerminalOfSize(written, guessingColumns, guessingLines).
			LinkPathsUnder(link.Roots{Workspace: workspaceDir})
	})
	rig.chat.display.reasoningRendering = output.ReasoningPlain
	rig.chat.display.streamingMode = scene.mode
	rig.chat.currentTurn = Turn{Stream: testRunningTurnStream(), painter: rig.chat.newPainter(true)}
	rig.chat.recordEvent(agent.Event{Kind: agent.UserMessageEvent, Text: "check it"})

	streamArriving(rig, agent.ModelReasoningEvent, scene.before+scene.line)
	arriving := rig.drawn()
	streamArriving(rig, agent.ModelReasoningEvent, scene.rest)
	rig.chat.recordEvent(agent.Event{Kind: agent.ModelReasoningEvent, Text: thought})
	landed := closeArrivingProse(rig)

	if strings.Contains(landed, ansi.EraseScrollback) {
		t.Errorf("a thought holding %q cleared the scrollback", name)
	}
	requireSameVisibleScreenInColumns(
		t,
		"a thought holding "+name+" differs from its replay",
		guessingColumns,
		replayOfArrivingProse(t, rig, guessingColumns),
		landed,
	)

	return arriving, landed
}
