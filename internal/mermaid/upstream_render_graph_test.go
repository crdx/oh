package mermaid

import (
	"fmt"
	"strings"
	"testing"

	"crdx.org/oh/internal/mermaid/runewidth"
)

func TestRenderGraphHandlesLongChainWithoutPanic(t *testing.T) {
	config := upstreamASCIIFlowchartConfig()

	var builder strings.Builder
	builder.WriteString("graph TD\n")
	for i := 1; i < 30; i++ {
		fmt.Fprintf(&builder, "N%d --> N%d\n", i, i+1)
	}

	output, err := renderUpstreamDiagram(builder.String(), config)
	if err != nil {
		t.Fatalf("renderUpstreamDiagram() error = %v", err)
	}

	if !strings.Contains(output, "N30") {
		t.Fatalf("expected output to contain the last node\noutput:\n%s", output)
	}
}

func TestRenderGraphKeepsDisplayWidthForWideNodeLabels(t *testing.T) {
	config := upstreamASCIIFlowchartConfig()
	output, err := renderUpstreamDiagram("graph LR\nA[\"中A\"] --> B", config)
	if err != nil {
		t.Fatalf("renderUpstreamDiagram() error = %v", err)
	}

	assertUniformDisplayWidth(t, output)
}

func TestRenderGraphKeepsDisplayWidthForWideSubgraphTitles(t *testing.T) {
	config := upstreamASCIIFlowchartConfig()
	output, err := renderUpstreamDiagram("graph LR\nsubgraph sg [数据库]\nA --> B\nend", config)
	if err != nil {
		t.Fatalf("renderUpstreamDiagram() error = %v", err)
	}

	assertUniformDisplayWidth(t, output)
}

func TestRenderGraphKeepsExplicitTargetLabelAfterBareReference(t *testing.T) {
	config := upstreamASCIIFlowchartConfig()
	output, err := renderUpstreamDiagram("graph TD\nA[\"Foo\"] --> B[\"Bar\"]\nB --> C[\"Baz\"]", config)
	if err != nil {
		t.Fatalf("renderUpstreamDiagram() error = %v", err)
	}

	if !strings.Contains(output, "Bar") {
		t.Fatalf("expected output to contain Bar\noutput:\n%s", output)
	}
	if strings.Contains(output, "\n|  B  |") || strings.Contains(output, "\n| B |\n") {
		t.Fatalf("expected B node to keep explicit label\noutput:\n%s", output)
	}
}

func TestRenderGraphKeepsStandaloneSubgraphLabelWhenReferencedLater(t *testing.T) {
	config := upstreamASCIIFlowchartConfig()
	output, err := renderUpstreamDiagram("graph TD\nsubgraph one\n    A[\"VcpuManager\"]\nend\nA --> B", config)
	if err != nil {
		t.Fatalf("renderUpstreamDiagram() error = %v", err)
	}

	if !strings.Contains(output, "VcpuManager") {
		t.Fatalf("expected output to contain VcpuManager\noutput:\n%s", output)
	}
	if strings.Contains(output, "\n| A |\n") || strings.Contains(output, "\n|  A  |\n") {
		t.Fatalf("expected A node to keep standalone explicit label\noutput:\n%s", output)
	}
}

func TestRenderGraphSupportsLiteralNewlineInNodeLabel(t *testing.T) {
	config := upstreamASCIIFlowchartConfig()
	output, err := renderUpstreamDiagram("graph LR\nA[\"line1\nline2\"] --> B", config)
	if err != nil {
		t.Fatalf("renderUpstreamDiagram() error = %v", err)
	}

	if !strings.Contains(output, "line1") || !strings.Contains(output, "line2") {
		t.Fatalf("expected output to contain both label lines\noutput:\n%s", output)
	}
	if strings.Contains(output, "A[\"line1") || strings.Contains(output, "line2\"]") {
		t.Fatalf("expected parser to keep literal newline inside the label\noutput:\n%s", output)
	}
}

func TestRenderGraphSeparatesDuplicateEdgeLabels(t *testing.T) {
	config := upstreamASCIIFlowchartConfig()
	output, err := renderUpstreamDiagram("graph LR\nA -->|miss| B\nA -->|hit| B", config)
	if err != nil {
		t.Fatalf("renderUpstreamDiagram() error = %v", err)
	}

	if strings.Contains(output, "mhit") {
		t.Fatalf("expected duplicate edge labels not to merge\noutput:\n%s", output)
	}
	if !strings.Contains(output, "miss") || !strings.Contains(output, "hit") {
		t.Fatalf("expected output to contain both duplicate edge labels\noutput:\n%s", output)
	}

	missLine := -1
	hitLine := -1
	for i, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "miss") {
			missLine = i
		}
		if strings.Contains(line, "hit") {
			hitLine = i
		}
	}
	if missLine == -1 || hitLine == -1 || missLine == hitLine {
		t.Fatalf("expected duplicate edge labels on separate lines\noutput:\n%s", output)
	}
}

func TestRenderGraphSeparatesBidirectionalEdgeLabelsLR(t *testing.T) {
	config := upstreamASCIIFlowchartConfig()
	output, err := renderUpstreamDiagram("graph LR\nA -->|workload exits| B\nB -->|run| A", config)
	if err != nil {
		t.Fatalf("renderUpstreamDiagram() error = %v", err)
	}

	if strings.Contains(output, "worklorunexits") {
		t.Fatalf("expected bidirectional edge labels not to merge\noutput:\n%s", output)
	}
	if !strings.Contains(output, "workload") || !strings.Contains(output, "exits") || !strings.Contains(output, "run") {
		t.Fatalf("expected output to contain both bidirectional edge labels\noutput:\n%s", output)
	}

	workloadLine := -1
	runLine := -1
	for i, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "workload") {
			workloadLine = i
		}
		if strings.Contains(line, "run") {
			runLine = i
		}
	}
	if workloadLine == -1 || runLine == -1 || workloadLine == runLine {
		t.Fatalf("expected bidirectional edge labels on separate lines\noutput:\n%s", output)
	}
}

func TestRenderGraphSeparatesBidirectionalEdgeLabelsTD(t *testing.T) {
	config := upstreamASCIIFlowchartConfig()
	output, err := renderUpstreamDiagram("graph TD\nA -->|forward| B\nB -->|back| A", config)
	if err != nil {
		t.Fatalf("renderUpstreamDiagram() error = %v", err)
	}

	if strings.Contains(output, "fbackrd") {
		t.Fatalf("expected bidirectional edge labels not to merge\noutput:\n%s", output)
	}
	if !strings.Contains(output, "forward") || !strings.Contains(output, "back") {
		t.Fatalf("expected output to contain both bidirectional edge labels\noutput:\n%s", output)
	}
}

func assertUniformDisplayWidth(t *testing.T, output string) {
	t.Helper()

	lines := strings.Split(output, "\n")
	if len(lines) == 0 {
		t.Fatal("expected rendered output")
	}

	want := runewidth.StringWidth(lines[0])
	for i, line := range lines[1:] {
		if got := runewidth.StringWidth(line); got != want {
			t.Fatalf("line %d display width = %d, want %d\noutput:\n%s", i+2, got, want, output)
		}
	}
}
