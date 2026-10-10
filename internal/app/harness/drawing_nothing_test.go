package harness

import (
	"strings"
	"testing"

	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/pkg/agent"
)

type thoughtDrawingNothing struct {
	rendering output.ReasoningRendering
	pieces    []string
}

func TestGoldenAThoughtThatEndsDrawingNothingLeavesNoRow(t *testing.T) {
	passes := map[string]func() string{}

	for name, scene := range map[string]thoughtDrawingNothing{
		"drawn as markdown": {rendering: output.ReasoningMarkdown, pieces: []string{"0", ")"}},
	} {
		rig := arrivingProseRig(t, output.StreamingModeASAP)
		rig.chat.display.reasoningRendering = scene.rendering
		rig.chat.currentTurn.painter = rig.chat.newPainter(true)
		for _, piece := range scene.pieces {
			streamDelta(rig.chat, agent.Delta{Kind: agent.ModelReasoningEvent, Text: piece})
		}
		rig.chat.recordEvent(agent.Event{Kind: agent.ModelReasoningEvent, Text: strings.Join(scene.pieces, "")})
		rig.chat.recordEvent(agent.Event{
			Kind:      agent.ToolCallRequestEvent,
			ID:        "1",
			Name:      "read",
			Arguments: `{"path":"notes.md"}`,
		})
		rig.chat.recordEvent(agent.Event{Kind: agent.ToolCallResultEvent, ID: "1", Name: "read", Text: "notes"})
		landed := closeArrivingProse(rig)

		replayRig := newReplayRig(t, replayColumns)
		replayRig.chat.display.reasoningRendering = scene.rendering
		requireSameVisibleScreen(
			t,
			"a thought "+name+" that ends drawing nothing differs from its replay",
			replayInto(replayRig, entriesRecordedBy(rig)),
			landed,
		)

		passes[name] = func() string { return shown(t, landed, replayColumns) }
	}

	compareWithGolden(t, "thought-drawing-nothing", ".screen", passes)
}
