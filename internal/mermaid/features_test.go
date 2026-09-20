package mermaid

import (
	"strings"
	"testing"

	"crdx.org/oh/internal/mermaid/diagram"
	"crdx.org/oh/internal/mermaid/runewidth"
)

func TestSupportedFlowchartFeatures(t *testing.T) {
	for name, test := range map[string]struct {
		source string
		want   []string
	}{
		"directions": {
			"flowchart RL\nA --> B",
			[]string{"A", "B"},
		},
		"labelled and bidirectional edges": {
			"graph LR\nA <-->|both| B\nB <--> C\nC -->|next| D",
			[]string{"both", "next", "A", "B", "C", "D"},
		},
		"chains and groups": {
			"graph TD\nA & B --> C --> D & E",
			[]string{"A", "B", "C", "D", "E"},
		},
		"multiline and unicode labels": {
			`graph LR
A["first<br/>second"] --> B[資料\n完了]`,
			[]string{"first", "second", "資料", "完了"},
		},
		"comments and styles": {
			"graph LR\n%% ignored\nA:::hot --> B %% trailing\nclassDef hot color:#fff",
			[]string{"A", "B"},
		},
		"nested subgraphs": {
			"graph LR\nX --> A\nsubgraph outer [Outer group]\nA\nsubgraph inner [Inner group]\nB\nend\nA --> B\nend\nsubgraph separate [Separate group]\nC\nend",
			[]string{"Outer group", "Inner group", "Separate group", "X", "A", "B", "C"},
		},
		"cycles and self references": {
			"graph TD\nA --> A\nA --> B\nB --> A",
			[]string{"A", "B"},
		},
		"frontmatter and padding": {
			"---\ntitle: Dependency map\nconfig:\n  theme: dark\n---\npaddingX=2\npaddingY=3\ngraph LR\nA --> B",
			[]string{"Dependency map", "A", "B"},
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

	for _, direction := range []string{"TD", "TB", "BT", "LR", "RL"} {
		if _, err := Render("graph " + direction + "\nA --> B"); err != nil {
			t.Errorf("direction %s: unexpected error: %v", direction, err)
		}
	}
}

func TestSupportedFlowchartASCIIOutput(t *testing.T) {
	properties, err := mermaidFileToMap("graph LR\nA --> B")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	renderer := &flowchartRenderer{properties: properties}
	config := diagram.DefaultConfig()
	config.UseAscii = true
	output, err := renderer.Render(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"+", "-", ">"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in %q", want, output)
		}
	}
}

func TestSupportedSequenceDiagramFeatures(t *testing.T) {
	for name, test := range map[string]struct {
		source string
		want   []string
	}{
		"all arrows": {
			"sequenceDiagram\nA->>B: solid\nB-->>A: dotted\nA->B: open\nB-->A: dotted open\nA-xB: cross\nB--xA: dotted cross\nA-)B: point\nB--)A: dotted point\nA<<->>B: both\nB<<-->>A: dotted both",
			[]string{"solid", "dotted", "open", "cross", "point", "both"},
		},
		"participants aliases and autonumber": {
			"sequenceDiagram\nparticipant A as Alice Smith\nactor B as Bob\nautonumber\nA->>B: hello",
			[]string{"Alice Smith", "Bob", "1. hello"},
		},
		"self and central messages": {
			"sequenceDiagram\nA->>A: think\nA ()->>() B: central",
			[]string{"think", "central"},
		},
		"notes": {
			"sequenceDiagram\nA->>B: begin\nNote left of A: left\nNote right of B: right\nNote over A: over one\nNote over A,B: over both",
			[]string{"left", "right", "over one", "over both"},
		},
		"fragments": {
			"sequenceDiagram\nloop retry\nA->>B: looped\nend\nopt maybe\nA->>B: optional\nend\nalt yes\nA->>B: first\nelse no\nB->>A: second\nend\npar one\nA->>B: parallel one\nand two\nB->>A: parallel two\nend\ncritical must\nA->>B: critical\noption fallback\nB->>A: option\nend\nbreak stop\nA->>B: broken\nend\nrect rgb(1,2,3) highlight\nA->>B: rectangle\nend",
			[]string{"loop", "optional", "first", "second", "parallel", "critical", "option", "broken", "rectangle"},
		},
		"nested fragments": {
			"sequenceDiagram\nloop outer\nalt branch\nopt inner\nA->>B: nested\nend\nelse other\nB->>A: return\nend\nend",
			[]string{"outer", "branch", "inner", "nested", "return"},
		},
		"comments and frontmatter": {
			"---\ntitle: Login flow\n---\nsequenceDiagram\n%% ignored\nA->>B: hello %% trailing",
			[]string{"Login flow", "hello"},
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

func TestSupportedEntityRelationshipFeatures(t *testing.T) {
	for name, test := range map[string]struct {
		source string
		want   []string
	}{
		"all cardinalities": {
			"erDiagram\nA ||--|| B : exactly\nB |o..o| C : optional\nC }o--o{ D : many\nD }|..|{ E : required",
			[]string{"exactly", "optional", "many", "required"},
		},
		"attributes keys and comments": {
			"erDiagram\nCUSTOMER {\nint id PK\nstring email UK \"login address\"\nint owner_id FK\n}\nCUSTOMER ||--o{ ORDER : places",
			[]string{"CUSTOMER", "id", "PK", "email", "UK", "login address", "owner_id", "FK", "ORDER", "places"},
		},
		"aliases and self relationships": {
			"erDiagram\nEMPLOYEE[Staff member] {\nint id PK\n}\nEMPLOYEE ||--o{ EMPLOYEE : manages",
			[]string{"Staff member", "manages"},
		},
		"word and numeric cardinalities": {
			"erDiagram\nA only one to one or more B : words\nB 0+ optionally to 1 C : numeric",
			[]string{"words", "numeric"},
		},
		"comments styles and direction": {
			"erDiagram\ndirection LR\n%% ignored\nA:::hot ||--|| B : styled\nclassDef hot color:#fff\nclass A hot\nstyle B color:#000",
			[]string{"A", "B", "styled"},
		},
		"quoted names and empty entities": {
			"erDiagram\n\"ORDER ITEM\" {}\n\"ORDER ITEM\" ||--|| PRODUCT : contains",
			[]string{"ORDER ITEM", "PRODUCT", "contains"},
		},
		"frontmatter": {
			"---\ntitle: Data model\n---\nerDiagram\nA ||--|| B : owns",
			[]string{"Data model", "owns"},
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

func TestEmojiGraphemesKeepDiagramBordersAligned(t *testing.T) {
	for name, test := range map[string]struct {
		source    string
		rowCount  int
		graphemes []string
	}{
		"flowchart": {
			source:    "graph LR\nA[👩‍🚀 Pilot] -->|1️⃣ launch| B[❤️ Ready]",
			rowCount:  5,
			graphemes: []string{"👩‍🚀", "1️⃣", "❤️"},
		},
		"sequence": {
			source:    "sequenceDiagram\nparticipant A as 👩‍🚀 Pilot\nparticipant B as 🤖 AI\nA->>B: ❤️ 1️⃣ launch",
			rowCount:  3,
			graphemes: []string{"👩‍🚀", "🤖", "❤️", "1️⃣"},
		},
		"entity relationship": {
			source:    "erDiagram\nA[👩‍🚀 Pilot] {\nstring state\n}\nB[🤖 AI] {\nstring state\n}\nA ||--|| B : ❤️ 1️⃣ link",
			rowCount:  5,
			graphemes: []string{"👩‍🚀", "🤖", "❤️", "1️⃣"},
		},
	} {
		output, err := Render(test.source)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, grapheme := range test.graphemes {
			if !strings.Contains(output, grapheme) {
				t.Errorf("%s: output lost grapheme %q:\n%s", name, grapheme, output)
			}
		}
		rows := strings.Split(output, "\n")
		wantWidth := runewidth.StringWidth(rows[0])
		for i, row := range rows[:min(test.rowCount, len(rows))] {
			if gotWidth := runewidth.StringWidth(row); gotWidth != wantWidth {
				t.Errorf("%s: row %d has width %d, want %d:\n%s", name, i, gotWidth, wantWidth, output)
			}
		}
	}
}
