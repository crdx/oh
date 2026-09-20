package sequence

import (
	"strings"
	"testing"

	"crdx.org/oh/internal/mermaid/diagram"
)

func TestSequenceDetectionAndParserEdgeCases(t *testing.T) {
	for source, want := range map[string]bool{
		"":                              false,
		"\n%% comment\nsequenceDiagram": true,
		"SEQUENCEDIAGRAM\t":             true,
		"sequenceDiagrammer":            false,
		"graph LR":                      false,
	} {
		if got := IsSequenceDiagram(source); got != want {
			t.Errorf("IsSequenceDiagram(%q) = %v, want %v", source, got, want)
		}
	}

	if _, err := Parse("sequenceDiagram\nNote over A: nowrap: compact\nNote over A: wrap: flowing"); err != nil {
		t.Fatalf("note prefixes: %v", err)
	}

	invalid := []string{
		"sequenceDiagram\nNote over ,,: empty",
		"sequenceDiagram\nelse branch",
		"sequenceDiagram\nloop x\nelse wrong",
		"sequenceDiagram\nparticipant A\nparticipant A",
	}
	for _, source := range invalid {
		if _, err := Parse(source); err == nil {
			t.Errorf("Parse(%q) unexpectedly succeeded", source)
		}
	}

	for _, line := range []string{`"A-B"->>C: quoted`, `A->>"B:C": quoted`, `A->>"B no colon`, `:bad->>B: invalid from`, `A->>: invalid to`, `loop ->> B: no`, `A ()->>() B: central`} {
		_, _ = splitMessage(line)
	}
	for _, raw := range []string{"", `""`, `"a"`, `"a"tail`, `"a"b"`, "a:b"} {
		_, _ = parseName(raw)
	}
	_, _ = findArrow(`"A->B" no arrow`)
}

func TestSequenceLayoutAndRenderingEdgeCases(t *testing.T) {
	alice := &Participant{ID: "A", Label: "", Index: 0}
	bob := &Participant{ID: "B", Label: "Bob", Index: 1}
	config := &diagram.Config{}
	layout := calculateLayout(&SequenceDiagram{Participants: []*Participant{alice, bob}}, config)
	if layout.messageSpacing != defaultMessageSpacing || layout.selfMessageWidth != defaultSelfMessageWidth {
		t.Fatalf("defaults were not applied: %#v", layout)
	}

	notes := []*Note{
		{Placement: NoteLeftOf, Participants: []*Participant{alice}, Text: "left<br/>wide"},
		{Placement: NoteRightOf, Participants: []*Participant{bob}, Text: "right<br />wide"},
		{Placement: NoteOver, Participants: []*Participant{bob, alice}, Text: "over<br>wide"},
	}
	for _, note := range notes {
		left, right := noteBoxColumns(note, layout)
		_ = renderNote(note, layout, Unicode)
		_ = renderNote(note, layout, ASCII)
		if right <= left {
			t.Errorf("invalid note columns %d..%d", left, right)
		}
	}

	events := []Event{
		{Kind: EventFragmentStart, Fragment: &Fragment{Type: FragmentLoop, Label: strings.Repeat("wide", 20)}},
		{Kind: EventNote, Note: notes[0]},
		{Kind: EventFragmentDivider, Fragment: &Fragment{Label: strings.Repeat("divider", 20)}},
		{Kind: EventNote, Note: notes[2]},
		{Kind: EventNote, Note: &Note{Placement: NoteRightOf, Participants: []*Participant{bob}, Text: strings.Repeat("right", 30)}},
		{Kind: EventFragmentEnd},
	}
	_ = noteLeftGutter(events, layout)
	_ = matchingFragmentEnd(events[:2], 0)
	_ = wrapFragment(events[0].Fragment, events[1:5], layout, Unicode)
	_, _ = involvedParticipants(events, layout)

	messages := []*Message{
		{From: alice, To: bob, Label: "", ArrowType: SolidOpen, CentralFrom: true, CentralTo: true},
		{From: bob, To: alice, Label: "numbered", Number: 2, ArrowType: BidirectionalDotted},
		{From: alice, To: alice, Label: strings.Repeat("self", 20), Number: 1, ArrowType: DottedOpen, CentralFrom: true, CentralTo: true},
	}
	for _, chars := range []BoxChars{Unicode, ASCII} {
		for _, message := range messages {
			if message.From == message.To {
				_ = renderSelfMessage(message, layout, chars)
			} else {
				_ = renderMessage(message, layout, chars)
			}
		}
	}

	for _, arrowType := range []ArrowType{SolidOpen, DottedOpen, ArrowType(99)} {
		_, _ = arrowType.head(Unicode, true)
		_, _ = arrowType.head(Unicode, false)
	}
}
