package mermaid

import (
	"fmt"
	"strings"
	"testing"
)

func TestSupportedDiagramsRender(t *testing.T) {
	for name, test := range map[string]struct {
		source string
		want   []string
	}{
		"flowchart": {
			"graph LR\nA[Start] --> B[Done]",
			[]string{"Start", "Done", "►"},
		},
		"sequence": {
			"sequenceDiagram\nAlice->>Bob: Hello",
			[]string{"Alice", "Bob", "Hello"},
		},
		"entity relationship": {
			"erDiagram\nCUSTOMER ||--o{ ORDER : places",
			[]string{"CUSTOMER", "ORDER", "places"},
		},
	} {
		output, err := Render(test.source)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", name, err)
		}

		for _, want := range test.want {
			if !strings.Contains(output, want) {
				t.Errorf("%s: expected %q in %q", name, want, output)
			}
		}
	}
}

func TestALoopBesideASubgraphStaysOnTheCanvas(t *testing.T) {
	for name, source := range map[string]string{
		"top down":   "flowchart TD\nsubgraph S\nA\nend\nS-->S",
		"left right": "graph LR\nsubgraph S\nA\nB\nend\nS-->S",
	} {
		output, err := Render(source)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", name, err)
		}
		if !strings.Contains(output, "◄") && !strings.Contains(output, "▲") {
			t.Errorf("%s: expected the loop's arrow head in %q", name, output)
		}
	}
}

func TestAnEdgeLabelTakingNoColumnsIsLeftOff(t *testing.T) {
	for name, source := range map[string]string{
		"between two nodes": "graph TD\nA-->|\u200b|B",
		"on a loop":         "graph LR\nA-->|\u200b|A",
	} {
		output, err := Render(source)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", name, err)
		}
		if strings.Contains(output, "\u200b") {
			t.Errorf("%s: expected the label left off, got %q", name, output)
		}
	}
}

func TestALabelledEdgeWithNoRouteStillLetsTheRestDraw(t *testing.T) {
	output, err := Render("graph TD\nA-->A\nA-->|yes|A")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(output, "A") || !strings.Contains(output, "◄") {
		t.Errorf("expected the node and its first loop in %q", output)
	}
}

func TestMalformedSourceReturnsAnError(t *testing.T) {
	for name, source := range map[string]string{
		"unknown direction":    "graph SIDEWAYS\nA --> B",
		"unsupported edge":     "graph LR\nA -.-> B",
		"unsupported shape":    "graph LR\nA((round))",
		"unsupported class":    "graph LR\nA\nclass A hot",
		"unsupported click":    "graph LR\nA\nclick A https://example.com",
		"invalid node":         "graph LR\nthis is not a node",
		"invalid chain member": "graph LR\nA -.-> B --> C",
		"unclosed subgraph":    "graph LR\nsubgraph one\nA",
		"malformed class":      "graph LR\nclassDef hot fill",
	} {
		if _, err := Render(source); err == nil {
			t.Errorf("%s: expected malformed source to return an error", name)
		}
	}
}

func TestValidStandaloneFlowchartNodesRender(t *testing.T) {
	source := "graph LR\nAlpha;\nβ[Unicode label]:::hot\nclassDef hot color:#fff\nAlpha --> β"
	output, err := Render(source)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"Alpha", "Unicode label"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in %q", want, output)
		}
	}
}

func TestResourceLimitsRejectOversizedDiagrams(t *testing.T) {
	for name, test := range map[string]struct {
		source string
		want   string
	}{
		"source bytes": {
			strings.Repeat("x", maximumSourceBytes+1),
			"source exceeds",
		},
		"source lines": {
			"graph LR\n" + strings.Repeat("A\n", maximumSourceLines),
			"source exceeds",
		},
		"line cells": {
			"graph LR\nA[" + strings.Repeat("x", maximumLineCells+1) + "]",
			"line 2 exceeds",
		},
		"nodes": {
			flowchartNodes(maximumFlowchartNodes + 1),
			"flowchart exceeds",
		},
		"edges": {
			repeatedFlowchartEdges(maximumFlowchartEdges + 1),
			"flowchart exceeds",
		},
		"subgraphs": {
			nestedFlowchartSubgraphs(maximumFlowchartSubgraphs + 1),
			"flowchart exceeds",
		},
		"label lines": {
			"graph LR\nA[" + strings.Repeat(`x\n`, maximumLabelLines) + "x]",
			"label exceeds",
		},
		"participants": {
			sequenceParticipants(maximumSequenceParticipants + 1),
			"participants",
		},
		"events": {
			sequenceEvents(maximumSequenceEvents + 1),
			"events",
		},
		"entities": {
			entityRelationshipEntities(maximumEntities + 1),
			"entities",
		},
		"relationships": {
			repeatedEntityRelationships(maximumRelationships + 1),
			"relationships",
		},
		"attributes": {
			entityAttributes(maximumAttributes + 1),
			"attributes",
		},
		"canvas": {
			wideFlowchart(),
			"columns",
		},
	} {
		_, err := Render(test.source)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: got error %v, want one containing %q", name, err, test.want)
		}
	}
}

func flowchartNodes(count int) string {
	var source strings.Builder
	source.WriteString("graph LR\n")
	for i := range count {
		fmt.Fprintf(&source, "N%d\n", i)
	}
	return source.String()
}

func repeatedFlowchartEdges(count int) string {
	return "graph LR\n" + strings.Repeat("A --> B\n", count)
}

func nestedFlowchartSubgraphs(count int) string {
	return "graph LR\n" + strings.Repeat("subgraph group\n", count) + "A\n" + strings.Repeat("end\n", count)
}

func sequenceParticipants(count int) string {
	var source strings.Builder
	source.WriteString("sequenceDiagram\n")
	for i := range count {
		fmt.Fprintf(&source, "participant P%d\n", i)
	}
	return source.String()
}

func sequenceEvents(count int) string {
	return "sequenceDiagram\n" + strings.Repeat("A->>B: hello\n", count)
}

func entityRelationshipEntities(count int) string {
	var source strings.Builder
	source.WriteString("erDiagram\n")
	for i := range count {
		fmt.Fprintf(&source, "E%d\n", i)
	}
	return source.String()
}

func repeatedEntityRelationships(count int) string {
	return "erDiagram\n" + strings.Repeat("A ||--|| B : owns\n", count)
}

func entityAttributes(count int) string {
	return "erDiagram\nA {\n" + strings.Repeat("string value\n", count) + "}\n"
}

func wideFlowchart() string {
	var source strings.Builder
	source.WriteString("graph LR\n")
	for i := range maximumFlowchartNodes {
		fmt.Fprintf(&source, "N%d[%s]\n", i, strings.Repeat("x", 150))
		if i > 0 {
			fmt.Fprintf(&source, "N%d --> N%d\n", i-1, i)
		}
	}
	return source.String()
}

func FuzzRender(fuzzer *testing.F) {
	for _, source := range []string{
		"graph LR\nA --> B",
		"sequenceDiagram\nA->>B: hello",
		"erDiagram\nA ||--o{ B : owns",
		"graph LR\nsubgraph broken",
		"\xff\xfe",
	} {
		fuzzer.Add(source)
	}

	fuzzer.Fuzz(func(t *testing.T, source string) {
		_, _ = Render(source)
	})
}
