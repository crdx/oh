package harness

import (
	"testing"

	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/pkg/agent"
)

type arrivingNotices struct {
	mode    output.StreamingMode
	kind    agent.Kind
	notices []string
}

func entriesRecordedBy(rig *replayRig) []replayEntry {
	recorded := make([]replayEntry, len(rig.chat.recordedEvents))
	for index := range rig.chat.recordedEvents {
		recorded[index] = replayEntry{Event: &rig.chat.recordedEvents[index]}
	}

	return recorded
}

func drawNoticesMidProse(t *testing.T, name string, scene arrivingNotices) (string, string) {
	t.Helper()

	prose := tallProse()
	middle := len(prose) / 2

	rig := arrivingProseRig(t, scene.mode)
	streamArriving(rig, scene.kind, prose[:middle])
	for _, notice := range scene.notices {
		noticesDrawnBesideAnOpenBlock(rig.chat)[notice]()
	}
	arriving := rig.drawn()
	streamArriving(rig, scene.kind, prose[middle:])
	rig.chat.recordEvent(agent.Event{Kind: scene.kind, Text: prose})
	landed := closeArrivingProse(rig)

	requireNothingDrawnAboveTheScreen(t, name, landed, arrivingProseLines)
	requireSameVisibleScreen(
		t,
		name+" differs from its replay",
		replayAtWidth(t, entriesRecordedBy(rig), replayColumns),
		landed,
	)

	return arriving, landed
}

func TestGoldenNoticesArrivingMidProseKeepTheirOrderInEveryMode(t *testing.T) {
	passes := map[string]func() string{}

	for name, scene := range map[string]arrivingNotices{
		"an ended job and a host command in the middle of an answer": {
			mode:    output.StreamingModeLine,
			kind:    agent.ModelMessageEvent,
			notices: []string{"an ended job", "a host command"},
		},
		"an ended job in the middle of an answer drawn as it arrives": {
			mode:    output.StreamingModeASAP,
			kind:    agent.ModelMessageEvent,
			notices: []string{"an ended job"},
		},
		"an ended job in the middle of a thought drawn in paced steps": {
			mode:    output.StreamingModePaced,
			kind:    agent.ModelReasoningEvent,
			notices: []string{"an ended job"},
		},
	} {
		arriving, landed := drawNoticesMidProse(t, name, scene)
		passes[name+", arriving"] = func() string { return shownInLines(t, arriving, arrivingProseLines) }
		passes[name+", landed"] = func() string { return shown(t, landed, replayColumns) }
	}

	compareWithGolden(t, "notice-mid-prose-modes", ".screen", passes)
}
