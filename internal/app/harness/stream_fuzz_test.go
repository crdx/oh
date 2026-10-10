package harness

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"crdx.org/oh/internal/app/ansi"
	"crdx.org/oh/internal/app/dynamic"
	"crdx.org/oh/internal/app/jobrecord"
	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/pkg/agent"
)

type proseJournal struct {
	name    string
	entries []replayEntry
	prose   []int
}

func proseJournals(f *testing.F) []proseJournal {
	f.Helper()

	var found []proseJournal

	for _, journal := range everyJournal(f) {
		entries := readJournal(f, journal.path)

		var prose []int
		for index, entry := range entries {
			if isStreamedKind(entry.Event.Kind) {
				prose = append(prose, index)
			}
		}

		if len(prose) > 0 {
			found = append(found, proseJournal{name: journal.name, entries: entries, prose: prose})
		}
	}

	if len(found) == 0 {
		f.Fatal("expected a journal holding prose")
	}

	return found
}

func isStreamedKind(kind agent.Kind) bool {
	return kind == agent.ModelMessageEvent || kind == agent.ModelReasoningEvent
}

var fuzzedStreamingModes = []output.StreamingMode{
	output.StreamingModeASAP,
	output.StreamingModeLine,
	output.StreamingModePaced,
}

var fuzzedReasoningRenderings = []output.ReasoningRendering{
	output.ReasoningPlain,
	output.ReasoningMarkdown,
}

const (
	maximumFuzzedProseBytes = 1024
	maximumFuzzedColumns    = 120
	minimumFuzzedLines      = 3
	maximumFuzzedLines      = 30

	streamStepRunes  = 0x0f
	streamStepAction = 4
	jobEndedAction   = 0x0e

	linkDefinitionMarker = "]:"
)

type streamSteps struct {
	steps []byte
	at    int
}

func (self *streamSteps) next() (int, byte) {
	if len(self.steps) == 0 {
		return deltaRunes, 0
	}

	step := self.steps[self.at%len(self.steps)]
	self.at++

	return 1 + int(step&streamStepRunes), step >> streamStepAction
}

type fuzzedStream struct {
	drawn          string
	recordedEvents []agent.Event
	endedJobs      int
}

func streamWithSteps(
	t *testing.T,
	entries []replayEntry,
	columns int,
	lines int,
	mode output.StreamingMode,
	rendering output.ReasoningRendering,
	steps *streamSteps,
) fuzzedStream {
	t.Helper()

	rig := newRig(t, func(written *strings.Builder, workspaceDir string) *output.Screen {
		return output.NewTerminalOfSize(written, columns, lines).LinkPathsUnder(link.Roots{Workspace: workspaceDir})
	})
	rig.chat.display.streamingMode = mode
	rig.chat.display.reasoningRendering = rendering
	rig.chat.currentTurn = Turn{Stream: testRunningTurnStream(), painter: rig.chat.newPainter(true)}
	rig.chat.screen.ReportProgress(true)

	endedJobs := 0
	act := func(action byte) {
		if action == jobEndedAction {
			rig.chat.jobEnded(endedJobConclusion())
			endedJobs++
		}
	}

	for _, entry := range entries {
		event := *entry.Event
		if isStreamedKind(event.Kind) {
			runes := []rune(event.Text)
			for at := 0; at < len(runes); {
				size, action := steps.next()
				piece := string(runes[at:min(at+size, len(runes))])
				at += size
				streamDelta(rig.chat, agent.Delta{Kind: event.Kind, Text: piece})
				act(action)
			}
		}

		rig.chat.recordEvent(event)

		_, action := steps.next()
		act(action)
	}

	rig.chat.releaseHeldNotices()
	rig.chat.currentTurn.painter.Close(dynamic.Done)
	if rig.chat.currentTurn.painter.Stale() {
		rig.chat.redraw()
	}
	rig.chat.screen.End()
	rig.chat.screen.ReportProgress(false)

	return fuzzedStream{
		drawn:          rig.drawn(),
		recordedEvents: rig.chat.recordedEvents,
		endedJobs:      endedJobs,
	}
}

func endedJobsIn(entries []replayEntry) int {
	count := 0
	for _, entry := range entries {
		if entry.Event.Kind == jobrecord.Ended {
			count++
		}
	}

	return count
}

func journalOf(t *testing.T, events []agent.Event) string {
	t.Helper()

	var lines strings.Builder

	for _, event := range events {
		line, err := json.Marshal(replayEntry{Event: &event})
		if err != nil {
			t.Fatal(err)
		}
		lines.Write(line)
		lines.WriteByte('\n')
	}

	return lines.String()
}

func FuzzAStreamedConversationDrawsWhatItsReplayDraws(fuzzer *testing.F) {
	journals := proseJournals(fuzzer)

	for journal := range journals {
		fuzzer.Add(uint8(journal), uint8(0), "", []byte{}, uint8(replayColumns-1), uint8(shortLines), uint8(0))
	}
	for index, source := range []string{
		"one paragraph\n\na second\n\nand a third",
		"- one\n\n- two\n\nafter",
		"```go\none\n\ntwo\n```\n\nafter",
		"see [note][n]\n\n[n]: https://example.com",
		"```mermaid\ngraph TD\nA --> B\n```\n\nafter",
		"| a | b |\n|---|---|\n| 1 | 2 |\n\nafter",
		"\tindented code\n\nparagraph\n\n\tmore code",
		"héllo 🐞\n\nsecond\n\nthird",
		"> quoted\n> still quoted\n\nnot quoted",
	} {
		fuzzer.Add(
			uint8(index),
			uint8(index),
			source,
			[]byte{0x03, 0x01, 0x00, 0xe7, 0x0f},
			uint8(index*13),
			uint8(index*5),
			uint8(index),
		)
	}

	fuzzer.Fuzz(func(
		t *testing.T,
		journalChoice uint8,
		proseChoice uint8,
		prose string,
		steps []byte,
		columnsChoice uint8,
		linesChoice uint8,
		modeChoice uint8,
	) {
		if len(prose) > maximumFuzzedProseBytes || !utf8.ValidString(prose) {
			t.Skip()
		}

		journal := journals[int(journalChoice)%len(journals)]
		entries := make([]replayEntry, len(journal.entries))
		for index, entry := range journal.entries {
			event := *entry.Event
			entries[index] = replayEntry{Event: &event}
		}
		if prose != "" {
			entries[journal.prose[int(proseChoice)%len(journal.prose)]].Event.Text = prose
		}

		columns := 1 + int(columnsChoice)%maximumFuzzedColumns
		lines := minimumFuzzedLines + int(linesChoice)%(maximumFuzzedLines-minimumFuzzedLines+1)
		mode := fuzzedStreamingModes[int(modeChoice)%len(fuzzedStreamingModes)]
		rendering := fuzzedReasoningRenderings[int(modeChoice)/len(fuzzedStreamingModes)%len(fuzzedReasoningRenderings)]

		streamed := streamWithSteps(t, entries, columns, lines, mode, rendering, &streamSteps{steps: steps})

		replayRig := newReplayRig(t, columns)
		replayRig.chat.display.reasoningRendering = rendering
		recorded := make([]replayEntry, len(streamed.recordedEvents))
		for index := range streamed.recordedEvents {
			recorded[index] = replayEntry{Event: &streamed.recordedEvents[index]}
		}
		replayed := replayInto(replayRig, recorded)

		defer func() {
			if t.Failed() {
				t.Logf(
					"journal %s at %d columns, %d lines, streamed %v with %v thoughts, drawn:\n%s",
					journal.name, columns, lines, mode, rendering,
					journalOf(t, streamed.recordedEvents),
				)
			}
		}()

		if !strings.Contains(prose, linkDefinitionMarker) && strings.Contains(streamed.drawn, ansi.EraseScrollback) {
			t.Error("streaming cleared the scrollback, which scrolls whoever is reading it to the bottom")
		}
		if recordedJobs := endedJobsIn(recorded) - endedJobsIn(entries); recordedJobs != streamed.endedJobs {
			t.Errorf("%d jobs ended while streaming but %d were recorded", streamed.endedJobs, recordedJobs)
		}
		played := playScreenOfSize(t, streamed.drawn, columns, lines)
		if played.wasDrawnAboveTheScreen {
			t.Errorf("streaming moved the cursor above the top of a %d-line screen", lines)
		}
		if played.wasScreenErasedWhole {
			t.Errorf("streaming erased a %d-line screen from its top, which a terminal may push into scrollback", lines)
		}
		requireSameVisibleScreenInColumns(
			t,
			"a streamed conversation differs from its replay",
			columns,
			replayed,
			streamed.drawn,
		)
	})
}
