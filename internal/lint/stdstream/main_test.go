package main

import (
	"reflect"
	"testing"

	"crdx.org/io/internal/lint/runner"
)

func TestAnalyse(t *testing.T) {
	tests := map[string]struct {
		source   string
		expected []string
	}{
		"a writer taken as a parameter": {
			source: "package example\n\nimport \"io\"\n\nfunc write(writer io.Writer) { _ = writer }\n",
		},
		"a main package": {
			source: "package main\n\nimport \"os\"\n\nfunc write() { _ = os.Stdout }\n",
		},
		"the process's output": {
			source:   "package example\n\nimport \"os\"\n\nfunc write() { _ = os.Stdout }\n",
			expected: []string{"example.go:5:20: a library package minds its own business"},
		},
		"the process's input": {
			source:   "package example\n\nimport \"os\"\n\nfunc read() { _ = os.Stdin }\n",
			expected: []string{"example.go:5:19: a library package minds its own business"},
		},
		"the process's failures": {
			source:   "package example\n\nimport \"os\"\n\nfunc fail() { _ = os.Stderr }\n",
			expected: []string{"example.go:5:19: a library package minds its own business"},
		},
		"handed to a child process": {
			source:   "package example\n\nimport (\n\t\"os\"\n\t\"os/exec\"\n)\n\nfunc run(command *exec.Cmd) { command.Stdout = os.Stdout }\n",
			expected: []string{"example.go:8:48: a library package minds its own business"},
		},
		"an aliased import": {
			source:   "package example\n\nimport operating \"os\"\n\nfunc write() { _ = operating.Stdout }\n",
			expected: []string{"example.go:5:20: a library package minds its own business"},
		},
		"somebody else's package called os": {
			source: "package example\n\nimport \"example.com/os\"\n\nfunc write() { _ = os.Stdout }\n",
		},
		"a field of the same name": {
			source: "package example\n\nimport \"os\"\n\nfunc write(options struct{ Stdout int }) { _ = os.Getenv(\"\"); _ = options.Stdout }\n",
		},
		"the os package used for something else": {
			source: "package example\n\nimport \"os\"\n\nfunc read() ([]byte, error) { return os.ReadFile(\"a\") }\n",
		},
		"every stream in one file": {
			source: "package example\n\nimport \"os\"\n\nfunc all() { _, _, _ = os.Stdin, os.Stdout, os.Stderr }\n",
			expected: []string{
				"example.go:5:24: a library package minds its own business",
				"example.go:5:34: a library package minds its own business",
				"example.go:5:45: a library package minds its own business",
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

func TestAnalyseRejectsInvalidGo(t *testing.T) {
	_, err := runner.CheckSource(analyse, "example.go", []byte("package"))
	if err == nil {
		t.Error("expected an error")
	}
}

func TestOnlyLibraryPackagesAreClaimed(t *testing.T) {
	tests := map[string]struct {
		packageName string
		expected    bool
	}{
		"pkg/toolbox/notify/notify.go":           {packageName: "notify"},
		"pkg/agent/agent.go":                     {packageName: "agent"},
		"pkg/wire/openai/responses/responses.go": {packageName: "responses"},
		"main.go":                                {packageName: "main", expected: true},
		"cmd/simulate/main.go":                   {packageName: "main", expected: true},
		"internal/app/ctl/console/console.go":    {packageName: "console", expected: true},
		"internal/sandbox/exec.go":               {packageName: "sandbox", expected: true},
		"internal/lint/stdstream/main.go":        {packageName: "main", expected: true},
		"notify.go":                              {packageName: "notify"},
	}

	for filename, test := range tests {
		if actual := isApplication(filename, test.packageName); actual != test.expected {
			t.Errorf("got isApplication(%q, %q) = %v, want %v", filename, test.packageName, actual, test.expected)
		}
	}
}
