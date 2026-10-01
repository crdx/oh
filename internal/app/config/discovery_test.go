package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/snippets"
)

func writeSnippetFile(t *testing.T, directory string, name string, contents string) string {
	t.Helper()

	snippetsDirectory := filepath.Join(directory, snippetDirectoryName)
	if err := os.MkdirAll(snippetsDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(snippetsDirectory, name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestASnippetFileBesideTheConfigIsDiscovered(t *testing.T) {
	directory := t.TempDir()
	path := writeSnippetFile(t, directory, "review.md", "---\ndescription: review the changes\narguments: optional\n---\n\nReview {{ .Arg }}.\n")
	configPath := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(configPath, ""); err != nil {
		t.Fatal(err)
	}

	config, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	want := snippets.Definition{
		Prompt:      "Review {{ .Arg }}.",
		File:        path,
		Description: "review the changes",
		Arguments:   snippets.ArgumentsOptional,
	}
	if got := config.Snippets["review"]; got != want {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestASnippetFileNeedsNoFrontmatter(t *testing.T) {
	directory := t.TempDir()
	writeSnippetFile(t, directory, "review.md", "Review it.\n")
	configPath := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(configPath, ""); err != nil {
		t.Fatal(err)
	}

	config, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := config.Snippets["review"]; got.Prompt != "Review it." || got.Description != "" || got.Arguments != "" {
		t.Errorf("got %#v", got)
	}
}

func TestSnippetsAreDiscoveredWithoutAConfigFile(t *testing.T) {
	directory := t.TempDir()
	writeSnippetFile(t, directory, "review.md", "Review it.")

	config, err := Load(filepath.Join(directory, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if config.Snippets["review"].Prompt != "Review it." {
		t.Errorf("got %#v", config.Snippets)
	}
}

func TestOnlyVisibleMarkdownFilesAreDiscoveredAsSnippets(t *testing.T) {
	directory := t.TempDir()
	writeSnippetFile(t, directory, "review.md", "Review it.")
	writeSnippetFile(t, directory, "notes.txt", "Not a snippet.")
	writeSnippetFile(t, directory, ".hidden.md", "Not a snippet.")
	writeSnippetFile(t, directory, "review.md~", "Not a snippet.")
	writeSnippetFile(t, directory, ".md", "Not a snippet.")
	if err := os.Mkdir(filepath.Join(directory, snippetDirectoryName, "nested.md"), 0o700); err != nil {
		t.Fatal(err)
	}

	config, err := Load(filepath.Join(directory, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if names := slices.Sorted(func(yield func(string) bool) {
		for name := range config.Snippets {
			if !yield(name) {
				return
			}
		}
	}); !slices.Equal(names, []string{"review"}) {
		t.Errorf("got %v, want the markdown file alone", names)
	}
}

func TestAConfiguredSnippetOutranksADiscoveredOneOfTheSameName(t *testing.T) {
	directory := t.TempDir()
	writeSnippetFile(t, directory, "review.md", "---\ndescription: from the file\n---\nFrom the file.")
	configPath := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(configPath, "[snippets]\nreview = \"From the config.\"\n"); err != nil {
		t.Fatal(err)
	}

	config, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := config.Snippets["review"]; got.Prompt != "From the config." || got.Description != "" {
		t.Errorf("got %#v", got)
	}
}

func TestASnippetFileTheConfigNamesIsNotDiscoveredUnderItsOwnName(t *testing.T) {
	directory := t.TempDir()
	writeSnippetFile(t, directory, "review.md", "Review it.")
	configPath := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(configPath, "[snippets]\nr = { file = \"snippets/review.md\" }\n"); err != nil {
		t.Fatal(err)
	}

	config, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, isDiscovered := config.Snippets["review"]; isDiscovered || config.Snippets["r"].Prompt != "Review it." {
		t.Errorf("got %#v", config.Snippets)
	}
}

func TestATableDescriptionOutranksTheFrontmatter(t *testing.T) {
	directory := t.TempDir()
	writeSnippetFile(t, directory, "review.md", "---\ndescription: from the file\narguments: none\n---\nReview {{ .Arg }}.")
	configPath := filepath.Join(directory, "config.toml")
	contents := "[snippets]\nreview = { file = \"snippets/review.md\", description = \"from the config\" }\n"
	if err := writeConfigFile(configPath, contents); err != nil {
		t.Fatal(err)
	}

	config, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := config.Snippets["review"]; got.Description != "from the config" || got.Arguments != snippets.ArgumentsNone {
		t.Errorf("got %#v", got)
	}
}

func TestAWorkspaceSnippetDirectoryIsNotDiscovered(t *testing.T) {
	workspace := t.TempDir()
	writeSnippetFile(t, workspace, "review.md", "Review it.")
	overridePath := filepath.Join(workspace, "oh.toml")
	if err := os.WriteFile(overridePath, []byte("[ui]\ncurrency = \"EUR\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := LoadSources(
		Source{Path: filepath.Join(t.TempDir(), "config.toml")},
		Source{Path: overridePath, IsOverride: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, isDiscovered := config.Snippets["review"]; isDiscovered {
		t.Errorf("got %#v, want nothing from the workspace", config.Snippets)
	}
}

func TestAnInvalidSnippetFileIsNamedInItsError(t *testing.T) {
	for name, contents := range map[string]string{
		"unknown field":         "---\nsummary: x\n---\nReview it.",
		"invalid arguments":     "---\narguments: sometimes\n---\nReview it.",
		"multiline description": "---\ndescription: |\n  one\n  two\n---\nReview it.",
		"unterminated":          "---\ndescription: x\nReview it.",
		"empty prompt":          "---\ndescription: x\n---\n\n",
		"malformed":             "---\ndescription: [\n---\nReview it.",
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			writeSnippetFile(t, directory, "review.md", contents)

			_, err := Load(filepath.Join(directory, "config.toml"))
			if err == nil || !strings.Contains(err.Error(), "review.md") {
				t.Errorf("got error %v, want one naming the file", err)
			}
		})
	}
}

func TestADiscoveredSnippetsTemplateErrorNamesItsFile(t *testing.T) {
	directory := t.TempDir()
	writeSnippetFile(t, directory, "review.md", "Review {{ .Arg")

	config, err := Load(filepath.Join(directory, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.BuildLive(testSegments()); err == nil || !strings.Contains(err.Error(), "review.md") {
		t.Errorf("got error %v, want one naming the file", err)
	}
}

func TestAddingASnippetFileReloadsIt(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(configPath, ""); err != nil {
		t.Fatal(err)
	}
	_, observer := observeConfig(t, configPath)

	writeSnippetFile(t, directory, "review.md", "---\ndescription: review the changes\n---\nReview it.")

	applied := awaitReload(t, observer)
	if got := applied.LiveConfig.SnippetCommandSet.Usages(); !slices.Contains(got, "//review") {
		t.Errorf("got %v, want the new snippet", got)
	}
	if len(applied.Changes) != 1 || applied.Changes[0].Path != "review.md" ||
		!slices.Equal(applied.Changes[0].Settings, []string{"snippets.review"}) {
		t.Errorf("got %#v, want the new file", applied.Changes)
	}
}

func TestRemovingASnippetFileReloadsWithoutIt(t *testing.T) {
	directory := t.TempDir()
	path := writeSnippetFile(t, directory, "review.md", "Review it.")
	configPath := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(configPath, ""); err != nil {
		t.Fatal(err)
	}
	_, observer := observeConfig(t, configPath)

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	applied := awaitReload(t, observer)
	if got := applied.LiveConfig.SnippetCommandSet.Usages(); slices.Contains(got, "//review") {
		t.Errorf("got %v, want the snippet gone", got)
	}
	if len(applied.Changes) != 1 || !applied.Changes[0].IsRemoved ||
		!slices.Equal(applied.Changes[0].Settings, []string{"snippets.review"}) {
		t.Errorf("got %#v, want the removed file", applied.Changes)
	}
}

func awaitReload(t *testing.T, observer *Observer) ReloadResult {
	t.Helper()

	timeout := time.NewTimer(watchTestTimeout)
	defer timeout.Stop()
	for {
		select {
		case failure, isOpen := <-observer.Changes():
			if !isOpen {
				t.Fatal("config watch closed before reporting the change")
			}
			result := observer.Reload(failure, testSegments())
			switch result.Status {
			case ReloadApplied:
				return result
			case ReloadFailed:
				t.Fatal(result.Failure)
			case ReloadUnchanged:
			}
		case <-timeout.C:
			t.Fatal("timed out waiting for a config change")
		}
	}
}
