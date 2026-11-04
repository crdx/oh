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
		"plain import": {
			source: "package example\n\nimport \"crdx.org/oh/pkg/question\"\n",
		},
		"blank import": {
			source: "package example\n\nimport _ \"embed\"\n",
		},
		"renamed import": {
			source:   "package example\n\nimport questions \"crdx.org/oh/pkg/question\"\n",
			expected: []string{"example.go:3:8: imports must not be renamed"},
		},
		"redundant name": {
			source:   "package example\n\nimport question \"crdx.org/oh/pkg/question\"\n",
			expected: []string{"example.go:3:8: imports must not be renamed"},
		},
		"dot import": {
			source:   "package example\n\nimport . \"strings\"\n",
			expected: []string{"example.go:3:8: imports must not be renamed"},
		},
		"grouped imports": {
			source:   "package example\n\nimport (\n\t\"go/ast\"\n\t_ \"embed\"\n\textensionast \"github.com/yuin/goldmark/extension/ast\"\n)\n",
			expected: []string{"example.go:6:2: imports must not be renamed"},
		},
		"no imports": {
			source: "package example\n",
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

func TestAnalyseRejectsInvalidGo(t *testing.T) {
	_, err := runner.CheckSource(analyse, "example.go", []byte("package"))
	if err == nil {
		t.Error("expected an error")
	}
}
