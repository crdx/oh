package mermaid

import (
	"strings"
	"testing"

	"crdx.org/oh/internal/mermaid/diagram"
)

func TestRenderWithFrontmatter(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantType  string
		wantParts []string
	}{
		{
			"sequence with title",
			"---\ntitle: Login flow\n---\nsequenceDiagram\nAlice->>Bob: Hi",
			"sequence",
			[]string{"Login flow", "Alice", "Bob", "Hi"},
		},
		{
			"er with theme config",
			"---\nconfig:\n  themeCSS: |\n    rect { fill: red; }\n---\nerDiagram\n  CUSTOMER ||--o{ ORDER : places",
			"entity-relationship",
			[]string{"CUSTOMER", "ORDER", "places"},
		},
		{
			"graph with title",
			"---\ntitle: Deps\n---\ngraph LR\nA-->B",
			"flowchart",
			[]string{"Deps", "A", "B"},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			stripped, _ := diagram.StripFrontmatter(test.input)
			diag := rendererFor(stripped)
			if diag.Type() != test.wantType {
				t.Errorf("detected %q, want %q", diag.Type(), test.wantType)
			}
			out, err := renderUpstreamDiagram(test.input, diagram.DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range test.wantParts {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q:\n%s", want, out)
				}
			}
		})
	}
}

func TestRenderTitleAboveDiagram(t *testing.T) {
	out, err := renderUpstreamDiagram("---\ntitle: My title\n---\ngraph LR\nA-->B", diagram.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	if lines[0] != "My title" || lines[1] != "" {
		t.Errorf("want title + blank line first, got %q, %q", lines[0], lines[1])
	}
}
