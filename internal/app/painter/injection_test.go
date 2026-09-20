package painter

import (
	"bytes"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/pkg/agent"
)

const injection = "before \x1b]52;c;cHduZWQ=\x07 and \x1b[2J after"

func drawnBytes(t *testing.T, draw func(*Picasso)) string {
	t.Helper()

	var screenOutput bytes.Buffer
	paint := New(output.NewTerminalOfSize(&screenOutput, 80, 24), true, nil, nil, output.StreamingModeLine)
	draw(paint)
	paint.Close(0)

	return screenOutput.String()
}

func assertInert(t *testing.T, name string, drawn string) {
	t.Helper()

	for _, sequence := range []string{"\x1b]52;", "\x1b[2J"} {
		if strings.Contains(drawn, sequence) {
			t.Errorf("%s let %q reach the terminal: %q", name, sequence, drawn)
		}
	}

	for _, word := range []string{"before", "after"} {
		if !strings.Contains(drawn, word) {
			t.Errorf("%s dropped %q along with the sequences: %q", name, word, drawn)
		}
	}
}

func TestAnAnswerCannotInstructTheTerminal(t *testing.T) {
	assertInert(t, "a whole answer", drawnBytes(t, func(paint *Picasso) {
		paint.DrawEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: injection})
	}))
}

func TestAStreamedAnswerCannotInstructTheTerminal(t *testing.T) {
	assertInert(t, "a streamed answer", drawnBytes(t, func(paint *Picasso) {
		for _, fragment := range strings.SplitAfter(injection, " ") {
			paint.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: fragment})
		}
	}))
}

func TestReasoningCannotInstructTheTerminal(t *testing.T) {
	assertInert(t, "reasoning", drawnBytes(t, func(paint *Picasso) {
		paint.DrawEvent(agent.Event{Kind: agent.ModelReasoningEvent, Text: injection})
	}))
}

func TestASubmittedMessageCannotInstructTheTerminal(t *testing.T) {
	assertInert(t, "a submitted message", drawnBytes(t, func(paint *Picasso) {
		paint.DrawEvent(agent.Event{Kind: agent.UserMessageEvent, Text: injection})
	}))
}

func TestAProvisionalDeltaRecordsWhatArrivedRatherThanWhatWasDrawn(t *testing.T) {
	paint := New(output.New(&bytes.Buffer{}), true, nil, nil, output.StreamingModeLine)
	paint.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: injection})

	if got := paint.ProvisionalDelta().Text; got != injection {
		t.Errorf("recorded %q, want what arrived %q", got, injection)
	}
}
