package dispatch_test

import (
	"os"
	"path/filepath"
	"testing"

	"crdx.org/oh/internal/app/dispatch"
	"crdx.org/oh/internal/app/slash"
)

func TestHandleTreatsAValidPathAsAnOrdinaryMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "valid path")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	registry := newTestRegistry(t)
	result, failure := dispatch.Handle(registry, dispatch.Actions{}, path)
	if result != dispatch.Proceed || failure != "" {
		t.Errorf("got result %d and failure %q", result, failure)
	}
}

func TestHandleRejectsExistingPathsWithFewerThanTwoParts(t *testing.T) {
	registry := newTestRegistry(t)
	for _, path := range []string{"/", "/home"} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}

		result, failure := dispatch.Handle(registry, dispatch.Actions{}, path)
		wantFailure := "Command not found: " + path + " (alt+enter to send)"
		if result != dispatch.Rejected || failure != wantFailure {
			t.Errorf("Handle(%q) got result %d and failure %q, want %q", path, result, failure, wantFailure)
		}
	}
}

func newTestRegistry(t *testing.T) slash.Registry {
	t.Helper()

	set, err := slash.NewCommandSet("/")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := slash.NewRegistry(set)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestAPastedPathIsAMessageRatherThanACommand(t *testing.T) {
	directory := t.TempDir()
	first := filepath.Join(directory, "image-8222fda1e66613ed.png")
	second := filepath.Join(directory, "image-d2667353b4d6223d.png")

	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("\x89PNG"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	for name, message := range map[string]string{
		"one path":               first,
		"the same path repeated": first + "\n" + first + "\n" + first,
		"several paths":          first + "\n" + second,
		"a path and a question":  first + " what is in this screenshot?",
		"a question and a path":  "what is in " + first + "?",
		"a path that is gone":    filepath.Join(directory, "image-0000000000000000.png"),
	} {
		t.Run(name, func(t *testing.T) {
			result, feedback := dispatch.Handle(newTestRegistry(t), dispatch.Actions{}, message)

			if result != dispatch.Proceed {
				t.Errorf("got %v with %q, want it sent as a message", result, feedback)
			}
		})
	}
}

func TestACommandIsStillACommandBesideThatChange(t *testing.T) {
	commands, err := slash.NewCommandSet("/")
	if err != nil {
		t.Fatal(err)
	}
	snippets, err := slash.NewCommandSet("//")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := slash.NewRegistry(snippets, commands)
	if err != nil {
		t.Fatal(err)
	}

	for name, message := range map[string]string{
		"a command":            "/nosuchcommand",
		"a command with words": "/nosuchcommand and some arguments",
		"a snippet":            "//nosuchsnippet",
	} {
		t.Run(name, func(t *testing.T) {
			result, feedback := dispatch.Handle(registry, dispatch.Actions{}, message)

			if result != dispatch.Rejected {
				t.Errorf("got %v with %q, want it refused as an unknown command", result, feedback)
			}
		})
	}
}
