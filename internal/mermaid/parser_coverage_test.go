package mermaid

import (
	"strings"
	"testing"

	"crdx.org/oh/internal/mermaid/diagram"
	"crdx.org/oh/internal/mermaid/er"
	"crdx.org/oh/internal/mermaid/orderedmap"
	"crdx.org/oh/internal/mermaid/sequence"
)

func TestFlowchartParserEdgeCases(t *testing.T) {
	for _, source := range []string{
		"one\\ntwo",
		"one[quoted \\n text]\\nthree",
		`one["quoted ] text"]\ntwo`,
	} {
		_ = splitGraphLines(source)
	}

	for _, input := range []string{"[bad]", "bad[missing", "bad:::!", "bad name", "good[Label]:::hot"} {
		_, _ = parseNode(input)
	}
	for _, match := range [][]string{{"bad class", "color:red"}, {"hot", "bad"}, {"hot", ":red"}, {"hot", "color:"}, {"hot", "color:red,fill:blue"}} {
		_, _ = parseStyleClass(match)
	}

	styles := map[string]styleClass{}
	properties := &graphProperties{
		data:         orderedmap.NewOrderedMap[string, []textEdge](),
		nodeSpecs:    map[string]graphNodeSpec{},
		styleClasses: &styles,
	}
	for _, expression := range []string{
		"",
		"A <--> |both| B",
		"A <--> B",
		"A --> |label| B",
		"A --> B",
		"classDef hot color:red",
		"A & B",
		"A --> [bad]",
		"[bad] --> B",
		"A <--> [bad]",
		"[bad] <--> B",
		"A <--> |x| [bad]",
		"[bad] <--> |x| B",
		"A --> |x| [bad]",
		"[bad] --> |x| B",
		"[bad] & B",
		"A & [bad]",
		"not parseable text",
	} {
		_, _ = properties.parseString(expression)
	}
	_, _ = properties.parseExpression("standalone")
	_, _ = properties.parseExpression("[bad]")

	for _, source := range []string{
		"",
		"paddingX=999999999999999999999999999999999999999999\ngraph LR",
		"paddingX=2\npaddingY=3",
		"nonsense",
		"graph LR extra",
		"graph XX",
		"graph;\nA",
		"graph LR\nsubgraph open\nA",
		"graph LR\nend\nA",
		"graph LR\n%% comment\nA --> B %% trailing\n---\nignored",
	} {
		_, _ = mermaidFileToMap(source)
	}
}

func TestLimitEdgeCases(t *testing.T) {
	styles := map[string]styleClass{}
	properties := &graphProperties{
		data:         orderedmap.NewOrderedMap[string, []textEdge](),
		nodeSpecs:    map[string]graphNodeSpec{"A": {label: newGraphLabel(strings.Repeat("line\n", maximumLabelLines+1))}},
		styleClasses: &styles,
	}
	properties.data.Set("A", nil)
	if err := validateFlowchartLimits(properties); err == nil {
		t.Error("expected a flowchart label limit error")
	}
	properties.nodeSpecs = map[string]graphNodeSpec{}
	properties.subgraphs = []*textSubgraph{{label: newGraphLabel(strings.Repeat("line\n", maximumLabelLines+1))}}
	if err := validateFlowchartLimits(properties); err == nil {
		t.Error("expected a subgraph label limit error")
	}

	participants := make([]*sequence.Participant, maximumSequenceParticipants)
	for i := range participants {
		participants[i] = &sequence.Participant{Label: strings.Repeat("x", maximumCanvasColumns)}
	}
	if err := validateSequenceLimits(&sequence.SequenceDiagram{Participants: participants}); err == nil {
		t.Error("expected a sequence column limit error")
	}
	if err := validateCanvasLimits(1, maximumCanvasRows+1); err == nil {
		t.Error("expected a canvas row limit error")
	}
	if err := validateCanvasLimits(maximumCanvasColumns, maximumCanvasRows); err == nil {
		t.Error("expected a canvas cell limit error")
	}

	sequenceRenderer := &sequenceRenderer{}
	tooManyParticipants := "sequenceDiagram\n" + strings.Repeat("participant A\n", maximumSequenceParticipants+1)
	_ = sequenceRenderer.Parse(tooManyParticipants)
	entityRenderer := &entityRelationshipRenderer{}
	if err := entityRenderer.Parse("not an er diagram"); err == nil {
		t.Error("expected an entity-relationship parse error")
	}
	if err := entityRenderer.Parse("erDiagram\n" + strings.Repeat("A ||--|| B : x\n", maximumRelationships+1)); err == nil {
		t.Error("expected an entity-relationship limit error")
	}
	_ = diagram.DefaultConfig()
	_ = er.ErDiagram{}
}
