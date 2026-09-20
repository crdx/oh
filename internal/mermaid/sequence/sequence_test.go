package sequence

import (
	"strings"
	"testing"

	"crdx.org/oh/internal/mermaid/diagram"
)

func TestEverySupportedSequenceConstructParsesAndRenders(t *testing.T) {
	source := "sequenceDiagram\nparticipant A as Alice\nactor B as Bob\nautonumber\nA->>B: solid\nB-->>A: dotted\nA->B: open\nB-->A: dotted open\nA-xB: cross\nB--xA: dotted cross\nA-)B: point\nB--)A: dotted point\nA<<->>B: both\nB<<-->>A: dotted both\nA->>A: self\nA ()->>() B: central\nNote left of A: left\nNote right of B: right\nNote over A: one\nNote over A,B: both\nloop outer\nopt inner\nA->>B: nested\nend\nend\nalt yes\nA->>B: yes\nelse no\nB->>A: no\nend\npar first\nA->>B: first\nand second\nB->>A: second\nend\ncritical primary\nA->>B: primary\noption fallback\nB->>A: fallback\nend\nbreak stop\nA->>B: stop\nend\nrect rgba(0,0,0,0.5) shade\nA->>B: shade\nend"
	parsed, err := Parse(source)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, useASCII := range []bool{false, true} {
		config := diagram.DefaultConfig()
		config.UseAscii = useASCII
		output, err := Render(parsed, config)
		if err != nil {
			t.Fatalf("ascii=%v: unexpected error: %v", useASCII, err)
		}
		for _, want := range []string{"Alice", "Bob", "solid", "self", "central", "left", "right", "nested", "fallback", "shade"} {
			if !strings.Contains(output, want) {
				t.Errorf("ascii=%v: expected %q in output", useASCII, want)
			}
		}
	}
}

func TestSequenceParserRejectsEveryInvalidStatementClass(t *testing.T) {
	for name, source := range map[string]string{
		"empty":                 "",
		"comments only":         "%% nothing",
		"wrong keyword":         "graph LR\nA --> B",
		"no participants":       "sequenceDiagram",
		"duplicate participant": "sequenceDiagram\nparticipant A\nparticipant A",
		"invalid participant":   "sequenceDiagram\nparticipant :",
		"empty note target":     "sequenceDiagram\nNote over : text",
		"unknown statement":     "sequenceDiagram\nA nonsense",
		"orphan else":           "sequenceDiagram\nA->>B: x\nelse no",
		"orphan and":            "sequenceDiagram\nA->>B: x\nand other",
		"orphan option":         "sequenceDiagram\nA->>B: x\noption other",
		"orphan end":            "sequenceDiagram\nA->>B: x\nend",
		"unclosed fragment":     "sequenceDiagram\nloop forever\nA->>B: x",
	} {
		if _, err := Parse(source); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestSequenceRendererHandlesLegacyAndUnbalancedEvents(t *testing.T) {
	alice := &Participant{ID: "A", Label: "Alice", Index: 0}
	bob := &Participant{ID: "B", Label: "Bob", Index: 1}
	message := &Message{From: alice, To: bob, Label: "legacy", ArrowType: SolidArrow}
	legacy := &SequenceDiagram{Participants: []*Participant{alice, bob}, Messages: []*Message{message}}
	output, err := Render(legacy, nil)
	if err != nil || !strings.Contains(output, "legacy") {
		t.Fatalf("legacy messages: got output %q and error %v", output, err)
	}

	unbalanced := &SequenceDiagram{
		Participants: []*Participant{alice, bob},
		Events: []Event{
			{Kind: EventFragmentDivider},
			{Kind: EventFragmentEnd},
			{Kind: EventMessage, Message: message},
		},
	}
	if _, err := Render(unbalanced, nil); err != nil {
		t.Fatalf("unbalanced events: unexpected error: %v", err)
	}

	if _, err := Render(nil, nil); err == nil {
		t.Error("expected a nil diagram error")
	}
	if _, err := Render(&SequenceDiagram{}, nil); err == nil {
		t.Error("expected a no-participants error")
	}
}

func TestSequenceEnumStringsCoverKnownAndUnknownValues(t *testing.T) {
	for value := FragmentLoop; value <= FragmentRect; value++ {
		if strings.Contains(value.String(), "FragmentType") {
			t.Errorf("known fragment %d rendered as unknown", value)
		}
	}
	if got := FragmentType(99).String(); !strings.Contains(got, "99") {
		t.Errorf("unknown fragment rendered as %q", got)
	}

	for value := EventMessage; value <= EventNote; value++ {
		if strings.Contains(value.String(), "EventKind") {
			t.Errorf("known event %d rendered as unknown", value)
		}
	}
	if got := EventKind(99).String(); !strings.Contains(got, "99") {
		t.Errorf("unknown event rendered as %q", got)
	}

	for value := SolidArrow; value <= BidirectionalDotted; value++ {
		if strings.Contains(value.String(), "ArrowType") {
			t.Errorf("known arrow %d rendered as unknown", value)
		}
	}
	if got := ArrowType(99).String(); !strings.Contains(got, "99") {
		t.Errorf("unknown arrow rendered as %q", got)
	}
}
