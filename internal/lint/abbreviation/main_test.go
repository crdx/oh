package main

import (
	"reflect"
	"testing"

	"crdx.org/oh/internal/lint/runner"
)

func TestAnalyse(t *testing.T) {
	tests := map[string]struct {
		source   string
		expected []string
	}{
		"the whole name": {
			source:   "package example\n\nfunc read() {\n\tidx := 0\n\t_ = idx\n}\n",
			expected: []string{`example.go:4:2: idx: write "idx" in full as "index"`},
		},
		"a shortened middle": {
			source:   "package example\n\nfunc read() {\n\tmedPrice := 0\n\t_ = medPrice\n}\n",
			expected: []string{`example.go:4:2: medPrice: write "med" in full as "medium"`},
		},
		"a word within a name": {
			source:   "package example\n\nfunc read() {\n\tlineIdx := 0\n\t_ = lineIdx\n}\n",
			expected: []string{`example.go:4:2: lineIdx: write "idx" in full as "index"`},
		},
		"an exported field": {
			source:   "package example\n\ntype thing struct {\n\tIdxOfRow int\n}\n",
			expected: []string{`example.go:4:2: IdxOfRow: write "idx" in full as "index"`},
		},
		"a parameter and a result": {
			source: "package example\n\nfunc read(cfg int) (val int) {\n\treturn cfg\n}\n",
			expected: []string{
				`example.go:3:11: cfg: write "cfg" in full as "configuration"`,
				`example.go:3:21: val: write "val" in full as "value"`,
			},
		},
		"a range variable": {
			source:   "package example\n\nfunc read(rows []int) {\n\tfor idx := range rows {\n\t\t_ = idx\n\t}\n}\n",
			expected: []string{`example.go:4:6: idx: write "idx" in full as "index"`},
		},
		"a type and its method": {
			source: "package example\n\ntype Stmt struct{}\n\nfunc (self Stmt) Val() {}\n",
			expected: []string{
				`example.go:3:6: Stmt: write "stmt" in full as "statement"`,
				`example.go:5:18: Val: write "val" in full as "value"`,
			},
		},
		"two abbreviations in one name": {
			source: "package example\n\nvar prevPos = 0\n",
			expected: []string{
				`example.go:3:5: prevPos: write "prev" in full as "previous"`,
				`example.go:3:5: prevPos: write "pos" in full as "position"`,
			},
		},
		"a plural abbreviation": {
			source: "package example\n\nvar nodeStmts = 0\nvar vals = 0\n",
			expected: []string{
				`example.go:3:5: nodeStmts: write "stmts" in full as "statements"`,
				`example.go:4:5: vals: write "vals" in full as "values"`,
			},
		},
		"a name spelt out": {
			source: "package example\n\nfunc read(previousIndex int) int {\n\treturn previousIndex\n}\n",
		},
		"a word which merely starts with an abbreviation": {
			source: "package example\n\nvar directive = \"\"\nvar positive = 0\nvar validity = 0\n",
		},
		"a foreign name only called": {
			source: "package example\n\nimport \"net/url\"\n\nfunc read(address *url.URL) string {\n\treturn address.RequestURI()\n}\n",
		},
		"an interface method declared here": {
			source:   "package example\n\ntype reader interface {\n\tPos() int\n}\n",
			expected: []string{`example.go:4:2: Pos: write "pos" in full as "position"`},
		},
		"the blank identifier": {
			source: "package example\n\nfunc read(rows []int) {\n\tfor _, _ = range rows {\n\t}\n}\n",
		},
		"a short word which is not an abbreviation here": {
			source: "package example\n\nfunc read(ctx int, args []int, err error, info int, max int, workspaceDir string) {\n\t_, _, _, _, _, _ = ctx, args, err, info, max, workspaceDir\n}\n",
		},
		"an acronym": {
			source: "package example\n\nvar readHTTPURL = \"\"\n",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			diagnostics, err := runner.CheckSource(analyse, "example.go", []byte(test.source))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var actual []string
			for _, diagnostic := range diagnostics {
				actual = append(actual, diagnostic.Position.String()+": "+diagnostic.Message)
			}
			if !reflect.DeepEqual(actual, test.expected) {
				t.Errorf("got %v, want %v", actual, test.expected)
			}
		})
	}
}

func TestWords(t *testing.T) {
	tests := map[string][]string{
		"idx":         {"idx"},
		"lineIdx":     {"line", "idx"},
		"LineIdx":     {"line", "idx"},
		"readHTTPURL": {"read", "h", "t", "t", "p", "u", "r", "l"},
		"":            nil,
	}

	for name, expected := range tests {
		t.Run(name, func(t *testing.T) {
			if actual := words(name); !reflect.DeepEqual(actual, expected) {
				t.Errorf("got %v, want %v", actual, expected)
			}
		})
	}
}
