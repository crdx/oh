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
		"a boolean parameter": {
			source:   "package example\n\nfunc read(writable bool) bool {\n\treturn writable\n}\n",
			expected: []string{`example.go:3:11: writable: name a boolean as a predicate, such as "isWritable"`},
		},
		"a boolean parameter named as a predicate": {
			source: "package example\n\nfunc read(isWritable bool) bool {\n\treturn isWritable\n}\n",
		},
		"a copular function's only boolean parameter": {
			source: "package example\n\nfunc shellIs(granted bool) string {\n\treturn \"\"\n}\n",
		},
		"a copular function with another parameter": {
			source:   "package example\n\nfunc shellIs(subject string, granted bool) string {\n\treturn \"\"\n}\n",
			expected: []string{`example.go:3:30: granted: name a boolean as a predicate, such as "isGranted"`},
		},
		"a copular function's named boolean result": {
			source:   "package example\n\nfunc shellIs() (granted bool) {\n\treturn\n}\n",
			expected: []string{`example.go:3:17: granted: name a boolean as a predicate, such as "isGranted"`},
		},
		"a boolean variable": {
			source:   "package example\n\nvar stopped bool\n",
			expected: []string{`example.go:3:5: stopped: name a boolean as a predicate, such as "isStopped"`},
		},
		"a boolean defined from a literal": {
			source:   "package example\n\nfunc read() bool {\n\tdrawn := true\n\treturn drawn\n}\n",
			expected: []string{`example.go:4:2: drawn: name a boolean as a predicate, such as "isDrawn"`},
		},
		"a boolean defined from a comparison": {
			source:   "package example\n\nfunc read(count int) bool {\n\tempty := count == 0\n\treturn empty\n}\n",
			expected: []string{`example.go:4:2: empty: name a boolean as a predicate, such as "isEmpty"`},
		},
		"a boolean defined from a negation": {
			source:   "package example\n\nfunc read(isOpen bool) bool {\n\tclosed := !isOpen\n\treturn closed\n}\n",
			expected: []string{`example.go:4:2: closed: name a boolean as a predicate, such as "isClosed"`},
		},
		"the second value of a comma-ok form": {
			source:   "package example\n\nfunc read(rows map[string]int) bool {\n\t_, present := rows[\"one\"]\n\treturn present\n}\n",
			expected: []string{`example.go:4:5: present: name a boolean as a predicate, such as "isPresent"`},
		},
		"the second value named ok": {
			source: "package example\n\nfunc read(rows map[string]int) bool {\n\t_, ok := rows[\"one\"]\n\treturn ok\n}\n",
		},
		"a boolean result": {
			source:   "package example\n\nfunc read() (running bool) {\n\treturn\n}\n",
			expected: []string{`example.go:3:14: running: name a boolean as a predicate, such as "isRunning"`},
		},
		"a struct field": {
			source: "package example\n\ntype thing struct {\n\tStream bool\n}\n",
		},
		"a value of another type": {
			source: "package example\n\nfunc read(count int) int {\n\ttotal := count + 1\n\treturn total\n}\n",
		},
		"a blank name": {
			source: "package example\n\nfunc read(rows map[string]int) {\n\t_, _ = rows[\"one\"]\n}\n",
		},
		"a name which merely starts with a predicate word": {
			source: "package example\n\nvar island bool\nvar canvas bool\n",
			expected: []string{
				`example.go:3:5: island: name a boolean as a predicate, such as "isIsland"`,
				`example.go:4:5: canvas: name a boolean as a predicate, such as "isCanvas"`,
			},
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

func TestIsPredicate(t *testing.T) {
	tests := map[string]bool{
		"isTerminal":   true,
		"hasResponded": true,
		"shouldKeep":   true,
		"wasStopped":   true,
		"ok":           true,
		"okToDraw":     true,
		"island":       false,
		"canvas":       false,
		"terminal":     false,
		"responded":    false,
	}

	for name, expected := range tests {
		t.Run(name, func(t *testing.T) {
			if actual := isPredicate(name); actual != expected {
				t.Errorf("got %t, want %t", actual, expected)
			}
		})
	}
}
