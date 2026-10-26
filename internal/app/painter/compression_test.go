package painter

import (
	"bytes"
	"testing"

	"crdx.org/oh/internal/app/metrics"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/pkg/agent"
)

func TestCompressionDiagnosticsDoNotDrawOrSettleAStreamingAnswer(t *testing.T) {
	for name, mode := range map[string]output.StreamingMode{
		"line":  output.StreamingModeLine,
		"asap":  output.StreamingModeASAP,
		"paced": output.StreamingModePaced,
	} {
		t.Run(name, func(t *testing.T) {
			var drawing bytes.Buffer
			paint := New(output.NewTerminalOfSize(&drawing, 80, 24), false, nil, nil, mode)
			paint.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "first "})
			before := drawing.String()
			previousKind := paint.previousKind
			events := paint.drawnEvents
			paint.DrawEvent(agent.Event{Kind: metrics.RequestCompressionRejected})
			if drawing.String() != before || paint.previousKind != previousKind || paint.drawnEvents != events {
				t.Error("an analysis-only fact changed the live conversation")
			}
			paint.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "second"})
			if paint.answer.Text() != "first second" {
				t.Errorf("the fact settled or changed the answer: %q", paint.answer.Text())
			}
		})
	}
}
