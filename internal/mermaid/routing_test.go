package mermaid

import (
	"fmt"
	"strings"
	"testing"
)

func TestUnrelatedEdgesDoNotRunAlongOneAnother(t *testing.T) {
	for name, source := range map[string]string{
		"arriving and leaving by one port":       "graph LR\nA --> M\nB --> M\nM --> C\nM --> D",
		"arriving and leaving by one port down":  "graph TD\nA --> M\nB --> M\nM --> C\nM --> D",
		"bidirectional beside an arrival":        "graph LR\nA --> M\nB --> M\nM --> C\nM <--> D",
		"backlink from the top":                  "graph LR\nA --> B\nB --> C\nA --> C\nB --> D\nD --> C",
		"labelled fan-in beside a bidirectional": "graph LR\nconf & sql -->|feed| box\nbox --> out --> reload\nbox <--> cache",
	} {
		t.Run(name, func(t *testing.T) {
			routed := routedGraph(t, source)
			for coordinate, occupants := range routed.occupants {
				for first := range occupants {
					for second := first + 1; second < len(occupants); second++ {
						one, other := occupants[first], occupants[second]
						if one.axes&other.axes != 0 && !canShareCells(one.edge, other.edge) {
							t.Errorf(
								"%s→%s and %s→%s both run through %v",
								one.edge.from.name, one.edge.to.name,
								other.edge.from.name, other.edge.to.name,
								coordinate,
							)
						}
					}
				}
			}
		})
	}
}

func TestEdgesSharingAnEndpointMayShareCells(t *testing.T) {
	source := &node{name: "source"}
	target := &node{name: "target"}
	elsewhere := &node{name: "elsewhere"}
	for name, test := range map[string]struct {
		first     *edge
		second    *edge
		wantShare bool
	}{
		"one source":        {&edge{from: source, to: target}, &edge{from: source, to: elsewhere}, true},
		"one target":        {&edge{from: source, to: target}, &edge{from: elsewhere, to: target}, true},
		"arriving, leaving": {&edge{from: elsewhere, to: source}, &edge{from: source, to: target}, false},
		"unrelated":         {&edge{from: source, to: target}, &edge{from: elsewhere, to: elsewhere}, false},
		"bidirectional":     {&edge{from: source, to: target, isBidirectional: true}, &edge{from: source, to: elsewhere}, false},
	} {
		if got := canShareCells(test.first, test.second); got != test.wantShare {
			t.Errorf("%s: got %v, want %v", name, got, test.wantShare)
		}
	}
}

func routedGraph(t *testing.T, source string) *graph {
	t.Helper()
	properties, err := mermaidFileToMap(source)
	if err != nil {
		t.Fatal(err)
	}
	routed, err := mapGraph(properties)
	if err != nil {
		t.Fatal(err)
	}
	return routed
}

func BenchmarkDenseFlowchartRouting(benchmark *testing.B) {
	const nodeCount = 60
	var source strings.Builder
	source.WriteString("graph LR\n")
	for index := range 2 * nodeCount {
		fmt.Fprintf(&source, "N%d --> N%d\n", index*7%nodeCount, index*13%nodeCount)
	}
	properties, err := mermaidFileToMap(source.String())
	if err != nil {
		benchmark.Fatal(err)
	}
	benchmark.ReportAllocs()
	for benchmark.Loop() {
		if _, err := mapGraph(properties); err != nil {
			benchmark.Fatal(err)
		}
	}
}
