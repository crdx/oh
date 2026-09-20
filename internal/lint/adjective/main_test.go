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
		"an adjective": {
			source:   "package example\n\nvar parsed value\n",
			expected: []string{"example.go:3:5: parsed: say what was parsed, since a name is a noun rather than an adjective"},
		},
		"a compound name": {
			source: "package example\n\nvar parsedValue value\n",
		},
		"an adjective type": {
			source:   "package example\n\ntype parsed struct{}\n",
			expected: []string{"example.go:3:6: parsed: say what was parsed, since a name is a noun rather than an adjective"},
		},
		"a compound type": {
			source: "package example\n\ntype parsedValue struct{}\n",
		},
		"an explicitly typed boolean variable": {
			source: "package example\n\nvar stopped bool\n",
		},
		"an explicitly typed boolean parameter": {
			source: "package example\n\nfunc shellIs(granted bool) {}\n",
		},
		"an explicitly typed named boolean result": {
			source: "package example\n\nfunc read() (stopped bool) {\n\treturn\n}\n",
		},
		"a non-boolean adjective parameter": {
			source:   "package example\n\nfunc read(stored cache) {}\n",
			expected: []string{"example.go:3:11: stored: say what was stored, since a name is a noun rather than an adjective"},
		},
		"a constant": {
			source: "package example\n\nconst stopped = true\n",
		},
		"a function": {
			source: "package example\n\nfunc parsed() {}\n",
		},
		"an interface method": {
			source: "package example\n\ntype reader interface {\n\tparsed()\n}\n",
		},
		"a word ending in ed": {
			source: "package example\n\nvar need value\n",
		},
		"an irregular participle": {
			source:   "package example\n\nvar written path\n",
			expected: []string{"example.go:3:5: written: say what was written, since a name is a noun rather than an adjective"},
		},
		"a compound irregular participle": {
			source: "package example\n\nvar writtenPath path\n",
		},
		"an explicitly typed boolean named for an irregular participle": {
			source: "package example\n\nvar held bool\n",
		},
		"a present participle": {
			source:   "package example\n\nvar opening job\n",
			expected: []string{"example.go:3:5: opening: say what is opening, since a name is a noun rather than an adjective"},
		},
		"a compound present participle": {
			source: "package example\n\nvar openingJob job\n",
		},
		"a present participle type": {
			source:   "package example\n\ntype pending struct{}\n",
			expected: []string{"example.go:3:6: pending: say what is pending, since a name is a noun rather than an adjective"},
		},
		"a present participle parameter": {
			source:   "package example\n\nfunc watch(ending job) {}\n",
			expected: []string{"example.go:3:12: ending: say what is ending, since a name is a noun rather than an adjective"},
		},
		"an explicitly typed boolean named for a present participle": {
			source: "package example\n\nvar running bool\n",
		},
		"a noun ending in ing": {
			source: "package example\n\nvar listing []job\n",
		},
		"a short word ending in ing": {
			source: "package example\n\nvar ring shape\n",
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
