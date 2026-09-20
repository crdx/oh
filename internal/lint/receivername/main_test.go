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
		"self pointer receiver": {
			source: "package example\n\nfunc (self *thing) method() {}\n",
		},
		"self value receiver": {
			source: "package example\n\nfunc (self thing) method() {}\n",
		},
		"unnamed receiver": {
			source: "package example\n\nfunc (*thing) method() {}\n",
		},
		"wrong receiver": {
			source:   "package example\n\nfunc (thing *thing) method() {}\n",
			expected: []string{"example.go:3:7: method receiver must be named self"},
		},
		"blank receiver": {
			source:   "package example\n\nfunc (_ *thing) method() {}\n",
			expected: []string{"example.go:3:7: method receiver must be named self"},
		},
		"ordinary function": {
			source: "package example\n\nfunc function() {}\n",
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
