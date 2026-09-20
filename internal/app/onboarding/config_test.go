package onboarding

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"crdx.org/oh/internal/app/config"
)

func TestSetInitialModelCreatesAConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	selection := "codex/gpt-5.6-sol@medium"

	written, err := setInitialModel(path, selection)
	if err != nil {
		t.Fatal(err)
	}
	if !written {
		t.Fatal("expected the initial model to be written")
	}

	contents, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("version = %d\n\n[model]\nround_robin = [\"codex/gpt-5.6-sol@medium\"]\n", config.Format)
	if string(contents) != want {
		t.Errorf("got:\n%s\nwant:\n%s", contents, want)
	}
}

func TestSetInitialModelPreservesAnExistingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	contents := fmt.Sprintf("version = %d\n\n# Keep me.\n[model] # Models live here.\n\n[editor]\ncommand = [\"code\"]\n", config.Format)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	written, err := setInitialModel(path, "anthropic/claude-opus-5@high")
	if err != nil {
		t.Fatal(err)
	}
	if !written {
		t.Fatal("expected the initial model to be written")
	}

	updated, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(contents, "[model] # Models live here.\n", "[model] # Models live here.\nround_robin = [\"anthropic/claude-opus-5@high\"]\n", 1)
	if string(updated) != want {
		t.Errorf("got:\n%s\nwant:\n%s", updated, want)
	}
}

func TestSetInitialModelLeavesAnExistingSelectionAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	contents := fmt.Sprintf("version = %d\n\n[model]\nround_robin = [\"codex/already-there@medium\"]\n", config.Format)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	written, err := setInitialModel(path, "anthropic/new@high")
	if err != nil {
		t.Fatal(err)
	}
	if written {
		t.Fatal("expected the existing selection to win")
	}

	updated, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if string(updated) != contents {
		t.Errorf("config changed to:\n%s", updated)
	}
}

func TestConcurrentInitialModelsDoNotOverwriteEachOther(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	selections := []string{"codex/one@medium", "anthropic/two@high"}
	results := make(chan bool, len(selections))
	errors := make(chan error, len(selections))

	var wait sync.WaitGroup
	for _, selection := range selections {
		wait.Go(func() {
			written, err := setInitialModel(path, selection)
			results <- written
			errors <- err
		})
	}
	wait.Wait()
	close(results)
	close(errors)

	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}

	var writes int
	for written := range results {
		if written {
			writes++
		}
	}
	if writes != 1 {
		t.Errorf("got %d writes, want one", writes)
	}

	settings, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(settings.Model.RoundRobin) != 1 {
		t.Errorf("got model rotation %v", settings.Model.RoundRobin)
	}
}

func TestSetInitialModelLooksPastAModelHeaderInsideASnippet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	prompt := "x = \"\"\"\n[model]\nkeep me\n\"\"\"\n"
	contents := fmt.Sprintf("version = %d\n\n[snippets]\n%s", config.Format, prompt)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	written, err := setInitialModel(path, "anthropic/one@high")
	if err != nil {
		t.Fatal(err)
	}
	if !written {
		t.Fatal("expected the initial model to be written")
	}

	settings, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := settings.Model.RoundRobin; len(got) != 1 || got[0] != "anthropic/one@high" {
		t.Errorf("got round robin %q, want the model to have been written", got)
	}
	if got := settings.Snippets["x"].Prompt; !strings.Contains(got, "[model]\nkeep me") {
		t.Errorf("got snippet %q, want it left alone", got)
	}
}

func TestSetInitialModelFindsAHeaderAfterASnippetHasClosed(t *testing.T) {
	body := "\n[snippets]\nx = '''\n[model]\n'''\ny = \"a \\\"quoted\\\" word\"\n\n[model]\n"
	contents := fmt.Sprintf("version = %d\n%s", config.Format, body)
	want := contents + "round_robin = [\"anthropic/two@high\"]\n"

	if got := string(addInitialModel([]byte(contents), "anthropic/two@high")); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}
