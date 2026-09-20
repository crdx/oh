package mermaid

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/mermaid/diagram"
	"crdx.org/oh/internal/mermaid/er"
	"crdx.org/oh/internal/mermaid/sequence"
)

var intentionalUpstreamRenderingDifferences = map[string]string{
	"ascii/subgraph_complex_mixed.txt":                  "subgraph border includes the external-node offset",
	"ascii/subgraph_mixed_nodes.txt":                    "subgraph border includes the external-node offset",
	"ascii/subgraph_mixed_nodes_td.txt":                 "subgraph border includes the external-node offset",
	"ascii/subgraph_nested_with_external.txt":           "subgraph border includes the external-node offset",
	"ascii/subgraph_node_outside_lr.txt":                "subgraph border includes the external-node offset",
	"multibyte/accented_latin_node_and_edge_labels.txt": "edge labels use terminal cells rather than UTF-8 bytes",
	"multibyte/cyrillic_node_and_edge_labels.txt":       "edge labels use terminal cells rather than UTF-8 bytes",
	"multibyte/greek_node_and_edge_labels.txt":          "edge labels use terminal cells rather than UTF-8 bytes",
}

func TestUpstreamRenderingFixtures(t *testing.T) {
	for _, test := range []struct {
		directory string
		diagram   string
		useASCII  bool
	}{
		{"ascii", "flowchart", true},
		{"extended-chars", "flowchart", false},
		{"multibyte", "flowchart", true},
		{"er", "entity-relationship", false},
		{"er-ascii", "entity-relationship", true},
		{"sequence", "sequence", false},
		{"sequence-ascii", "sequence", true},
	} {
		t.Run(test.directory, func(t *testing.T) {
			directory := filepath.Join("testdata", "upstream", test.directory)
			entries, err := os.ReadDir(directory)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.IsDir() || filepath.Ext(entry.Name()) != ".txt" {
					continue
				}
				t.Run(entry.Name(), func(t *testing.T) {
					source, expected := readUpstreamRenderingFixture(t, filepath.Join(directory, entry.Name()))
					actual := renderUpstreamFixture(t, source, test.diagram, test.useASCII)
					actual = normalizeUpstreamRendering(actual)
					expected = normalizeUpstreamRendering(expected)
					fixture := filepath.Join(test.directory, entry.Name())
					if reason, isIntentionalDifference := intentionalUpstreamRenderingDifferences[fixture]; isIntentionalDifference {
						if actual == "" {
							t.Errorf("intentional difference %q rendered nothing", reason)
						}
						if actual == expected {
							t.Errorf("rendering now matches upstream; remove the intentional difference %q", reason)
						}
						return
					}
					if actual != expected {
						t.Errorf("rendering differs\nexpected:\n%s\nactual:\n%s", visualizeUpstreamWhitespace(expected), visualizeUpstreamWhitespace(actual))
					}
				})
			}
		})
	}
}

func readUpstreamRenderingFixture(t *testing.T, path string) (string, string) {
	t.Helper()
	//nolint:gosec // path is assembled from the controlled fixture directory
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(content), "\n---\n")
	if len(parts) != 2 {
		t.Fatalf("%s does not contain exactly one separator", path)
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func renderUpstreamFixture(t *testing.T, source string, diagramType string, shouldUseASCII bool) string {
	t.Helper()
	config := diagram.DefaultConfig()
	config.UseAscii = shouldUseASCII

	switch diagramType {
	case "flowchart":
		properties, err := mermaidFileToMap(source)
		if err != nil {
			t.Fatal(err)
		}
		properties.useAscii = shouldUseASCII
		rendering, err := drawMap(properties)
		return mustUpstreamRendering(t, rendering, err)
	case "entity-relationship":
		parsed, err := er.Parse(source)
		if err != nil {
			t.Fatal(err)
		}
		return er.Render(parsed, shouldUseASCII)
	case "sequence":
		parsed, err := sequence.Parse(source)
		if err != nil {
			t.Fatal(err)
		}
		rendering, err := sequence.Render(parsed, config)
		return mustUpstreamRendering(t, rendering, err)
	default:
		t.Fatalf("unknown diagram type %q", diagramType)
		return ""
	}
}

func mustUpstreamRendering(t *testing.T, rendering string, err error) string {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return rendering
}

func normalizeUpstreamRendering(rendering string) string {
	lines := strings.Split(rendering, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

func visualizeUpstreamWhitespace(rendering string) string {
	return strings.ReplaceAll(normalizeUpstreamRendering(rendering), " ", "·")
}
