package harness

import (
	"fmt"
	"strings"
	"testing"

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

func replayOfArrivingProse(t *testing.T, rig *replayRig) string {
	t.Helper()

	recorded := make([]replayEntry, len(rig.chat.recordedEvents))
	for index := range rig.chat.recordedEvents {
		recorded[index] = replayEntry{Event: &rig.chat.recordedEvents[index]}
	}

	return replayAtWidth(t, recorded, replayColumns)
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
	prose := tallProse()
	middle := len(prose) / 2

	for proseName, kind := range map[string]agent.Kind{
		"an answer": agent.ModelMessageEvent,
		"a thought": agent.ModelReasoningEvent,
	} {
		for noticeName := range noticesDrawnBesideAnOpenBlock(nil) {
			name := fmt.Sprintf("%s in the middle of %s", noticeName, proseName)

			rig := arrivingProseRig(t, output.StreamingModeLine)
			streamArriving(rig, kind, prose[:middle])
			noticesDrawnBesideAnOpenBlock(rig.chat)[noticeName]()
			arriving := rig.drawn()
			streamArriving(rig, kind, prose[middle:])
			rig.chat.recordEvent(agent.Event{Kind: kind, Text: prose})
			landed := closeArrivingProse(rig)

			requireNothingDrawnAboveTheScreen(t, name, landed, arrivingProseLines)
			requireSameVisibleScreen(t, name+" differs from its replay", replayOfArrivingProse(t, rig), landed)

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

	requireSameVisibleScreen(t, "an answer that ends drawing nothing differs from its replay", replayOfArrivingProse(t, rig), landed)

	compareWithGolden(t, "answer-drawing-nothing", ".screen", map[string]func() string{
		"streamed": func() string { return shown(t, landed, replayColumns) },
	})
}
