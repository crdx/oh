package main

import (
	"reflect"
	"testing"

	"crdx.org/oh/internal/lint/runner"
)

const diagnostic = "example.go:3:13: " + message

func TestAnalyse(t *testing.T) {
	tests := map[string]struct {
		filename string
		source   string
		expected []string
	}{
		"a hex colour": {
			source:   "package example\n\nvar paint = \"#c08050\"\n",
			expected: []string{diagnostic},
		},
		"a hex colour with a decoration": {
			source:   "package example\n\nvar paint = \"bold #C08050\"\n",
			expected: []string{diagnostic},
		},
		"a truecolour foreground": {
			source:   "package example\n\nvar paint = \"\\x1b[38;2;192;128;80m\"\n",
			expected: []string{diagnostic},
		},
		"a truecolour background": {
			source:   "package example\n\nvar paint = \"\\x1b[48;2;52;53;65m\"\n",
			expected: []string{diagnostic},
		},
		"a 256 colour after a decoration": {
			source:   "package example\n\nvar paint = \"\\x1b[1;38;5;208m\"\n",
			expected: []string{diagnostic},
		},
		"a basic foreground": {
			source:   "package example\n\nvar paint = \"\\x1b[31m\"\n",
			expected: []string{diagnostic},
		},
		"a bright background": {
			source:   "package example\n\nvar paint = \"\\x1b[102m\"\n",
			expected: []string{diagnostic},
		},
		"a raw string": {
			source:   "package example\n\nvar paint = `#010203`\n",
			expected: []string{diagnostic},
		},
		"a decoration alone": {
			source: "package example\n\nvar paint = \"\\x1b[1m\\x1b[3m\\x1b[0m\\x1b[39m\\x1b[49m\"\n",
		},
		"a cursor movement": {
			source: "package example\n\nvar move = \"\\x1b[38;2H\\x1b[31A\"\n",
		},
		"a hash that is not a colour": {
			source: "package example\n\nvar heading = \"#heading #abc #c08050ff\"\n",
		},
		"the palette itself": {
			filename: "internal/app/style/theme.go",
			source:   "package style\n\nvar paint = \"#c08050\"\n",
		},
		"a picture's identifier": {
			filename: "internal/app/graphics/graphics.go",
			source:   "package graphics\n\nvar identity = \"\\x1b[38;2;%d;%d;%dm\"\n",
		},
		"a package named like the palette elsewhere": {
			filename: "internal/app/stylish/theme.go",
			source:   "package stylish\n\nvar paint = \"#c08050\"\n",
			expected: []string{"internal/app/stylish/theme.go:3:13: " + message},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			filename := test.filename
			if filename == "" {
				filename = "example.go"
			}
			diagnostics, err := runner.CheckSource(analyse, filename, []byte(test.source))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var actual []string
			for _, found := range diagnostics {
				actual = append(actual, found.Position.String()+": "+found.Message)
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
