package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/output"
)

const watchTestTimeout = 2 * time.Second

func observeConfig(t *testing.T, path string) (Config, *Observer) {
	t.Helper()

	settings, observer, err := Observe(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(observer.Close)
	return settings, observer
}

func awaitObservedConfig(t *testing.T, observer *Observer) (Config, error) {
	t.Helper()

	timeout := time.NewTimer(watchTestTimeout)
	defer timeout.Stop()
	for {
		select {
		case failure, isOpen := <-observer.Changes():
			if !isOpen {
				t.Fatal("config watch closed before reporting the change")
			}
			settings, changes, err := observer.refresh(failure)
			if err != nil || len(changes) > 0 {
				return settings, err
			}
		case <-timeout.C:
			t.Fatal("timed out waiting for a config change")
		}
	}
}

func TestWritingAnObservedConfigLoadsTheNewRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[ui]\ncurrency = \"GBP\"\n"); err != nil {
		t.Fatal(err)
	}

	settings, observer := observeConfig(t, path)
	if settings.Ui.Currency != "GBP" {
		t.Errorf("got initial currency %q", settings.Ui.Currency)
	}
	if err := writeConfigFile(path, "[ui]\ncurrency = \"EUR\"\n"); err != nil {
		t.Fatal(err)
	}

	settings, err := awaitObservedConfig(t, observer)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Ui.Currency != "EUR" {
		t.Errorf("got changed currency %q", settings.Ui.Currency)
	}
}

func TestWritingAnObservedSnippetFileLoadsTheNewPrompt(t *testing.T) {
	directory := t.TempDir()
	snippetsDirectory := filepath.Join(directory, "snippets")
	if err := os.Mkdir(snippetsDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	snippetPath := filepath.Join(snippetsDirectory, "review.md")
	if err := os.WriteFile(snippetPath, []byte("Review the first revision."), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(configPath, "[snippets]\nreview = { file = \"snippets/review.md\" }\n"); err != nil {
		t.Fatal(err)
	}

	settings, observer := observeConfig(t, configPath)
	if settings.Snippets["review"].Prompt != "Review the first revision." {
		t.Errorf("got initial snippet %q", settings.Snippets["review"].Prompt)
	}
	if err := os.WriteFile(snippetPath, []byte("Review the reloaded revision."), 0o600); err != nil {
		t.Fatal(err)
	}

	settings, err := awaitObservedConfig(t, observer)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Snippets["review"].Prompt != "Review the reloaded revision." {
		t.Errorf("got changed snippet %q", settings.Snippets["review"].Prompt)
	}
}

func TestCreatingAMissingObservedSnippetFileRecoversTheConfig(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(configPath, "[ui]\ncurrency = \"GBP\"\n"); err != nil {
		t.Fatal(err)
	}
	_, observer := observeConfig(t, configPath)

	if err := writeConfigFile(configPath, "[snippets]\nreview = { file = \"new/review.md\" }\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := awaitObservedConfig(t, observer); err == nil {
		t.Fatal("expected the missing snippet file to fail")
	}

	snippetPath := filepath.Join(directory, "new", "review.md")
	if err := os.Mkdir(filepath.Dir(snippetPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snippetPath, []byte("Review after recovery."), 0o600); err != nil {
		t.Fatal(err)
	}

	settings, err := awaitObservedConfig(t, observer)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Snippets["review"].Prompt != "Review after recovery." {
		t.Errorf("got recovered snippet %q", settings.Snippets["review"].Prompt)
	}
}

func TestAtomicallyReplacingAnObservedConfigLoadsTheNewRevision(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(path, "[ui]\ncurrency = \"GBP\"\n"); err != nil {
		t.Fatal(err)
	}
	_, observer := observeConfig(t, path)

	replacement := filepath.Join(directory, "replacement.toml")
	if err := writeConfigFile(replacement, "[ui]\ncurrency = \"CHF\"\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}

	settings, err := awaitObservedConfig(t, observer)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Ui.Currency != "CHF" {
		t.Errorf("got replacement currency %q", settings.Ui.Currency)
	}
}

func TestCreatingAConfigBelowMissingDirectoriesReplacesTheDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "one", "two", "config.toml")
	settings, observer := observeConfig(t, path)
	if settings.Ui.Currency != "" {
		t.Errorf("initial default currency=%q", settings.Ui.Currency)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeConfigFile(path, "[ui]\ncurrency = \"SEK\"\n"); err != nil {
		t.Fatal(err)
	}

	settings, err := awaitObservedConfig(t, observer)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Ui.Currency != "SEK" {
		t.Errorf("got created currency %q", settings.Ui.Currency)
	}
}

func TestDeletingAnObservedConfigRestoresTheDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[ui]\ncurrency = \"NOK\"\n"); err != nil {
		t.Fatal(err)
	}
	_, observer := observeConfig(t, path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	settings, err := awaitObservedConfig(t, observer)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Ui.Currency != "" {
		t.Errorf("got default currency %q", settings.Ui.Currency)
	}
}

func TestCreatingAnObservedOverrideReplacesTheGlobalConfig(t *testing.T) {
	directory := t.TempDir()
	globalPath := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(globalPath, "[ui]\ncurrency = \"AUD\"\n"); err != nil {
		t.Fatal(err)
	}
	overridePath := filepath.Join(directory, "oh.toml")
	settings, observer, err := ObserveSources(
		Source{Path: globalPath},
		Source{Path: overridePath, IsOverride: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(observer.Close)
	if settings.Ui.Currency != "AUD" {
		t.Errorf("got initial currency %q", settings.Ui.Currency)
	}
	if err := os.WriteFile(overridePath, []byte("[ui]\ncurrency = \"SEK\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	settings, err = awaitObservedConfig(t, observer)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Ui.Currency != "SEK" {
		t.Errorf("got overridden currency %q", settings.Ui.Currency)
	}
}

func TestEditingAnObservedOverrideReplacesItsLiveSettings(t *testing.T) {
	directory := t.TempDir()
	globalPath := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(globalPath, "[ui]\ncurrency = \"AUD\"\n"); err != nil {
		t.Fatal(err)
	}
	overridePath := filepath.Join(directory, "oh.toml")
	if err := os.WriteFile(overridePath, []byte("[ui]\ncurrency = \"GBP\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, observer, err := ObserveSources(
		Source{Path: globalPath},
		Source{Path: overridePath, IsOverride: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(observer.Close)
	if err := os.WriteFile(overridePath, []byte("[ui]\ncurrency = \"EUR\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	settings, err := awaitObservedConfig(t, observer)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Ui.Currency != "EUR" {
		t.Errorf("got changed currency %q", settings.Ui.Currency)
	}
}

func TestAtomicallyReplacingAnObservedOverrideLoadsItsNewSettings(t *testing.T) {
	directory := t.TempDir()
	globalPath := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(globalPath, "[ui]\ncurrency = \"AUD\"\n"); err != nil {
		t.Fatal(err)
	}
	overridePath := filepath.Join(directory, "oh.toml")
	if err := os.WriteFile(overridePath, []byte("[ui]\ncurrency = \"GBP\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, observer, err := ObserveSources(
		Source{Path: globalPath},
		Source{Path: overridePath, IsOverride: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(observer.Close)
	replacementPath := filepath.Join(directory, "replacement.toml")
	if err := os.WriteFile(replacementPath, []byte("[ui]\ncurrency = \"CHF\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacementPath, overridePath); err != nil {
		t.Fatal(err)
	}

	settings, err := awaitObservedConfig(t, observer)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Ui.Currency != "CHF" {
		t.Errorf("got replacement currency %q", settings.Ui.Currency)
	}
}

func TestDeletingAnObservedOverrideRestoresTheGlobalConfig(t *testing.T) {
	directory := t.TempDir()
	globalPath := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(globalPath, "[ui]\ncurrency = \"AUD\"\n"); err != nil {
		t.Fatal(err)
	}
	overridePath := filepath.Join(directory, "oh.toml")
	if err := os.WriteFile(overridePath, []byte("[ui]\ncurrency = \"CAD\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, observer, err := ObserveSources(
		Source{Path: globalPath},
		Source{Path: overridePath, IsOverride: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(observer.Close)
	if settings.Ui.Currency != "CAD" {
		t.Errorf("got initial currency %q", settings.Ui.Currency)
	}
	if err := os.Remove(overridePath); err != nil {
		t.Fatal(err)
	}

	settings, err = awaitObservedConfig(t, observer)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Ui.Currency != "AUD" {
		t.Errorf("got restored currency %q", settings.Ui.Currency)
	}
}

func TestAnInvalidObservedRevisionIsReportedOnlyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[ui]\ncurrency = \"GBP\"\n"); err != nil {
		t.Fatal(err)
	}
	_, observer := observeConfig(t, path)
	if err := os.WriteFile(path, []byte("not toml = ["), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := awaitObservedConfig(t, observer); err == nil {
		t.Fatal("expected the invalid revision to fail")
	}
	if _, changes, err := observer.refresh(nil); err != nil || len(changes) > 0 {
		t.Errorf("repeated revision changes=%v err=%v", changes, err)
	}
}

func TestAnUnrelatedDirectoryEventDoesNotChangeTheObservedConfig(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(path, "[ui]\ncurrency = \"PLN\"\n"); err != nil {
		t.Fatal(err)
	}
	_, observer := observeConfig(t, path)
	if err := os.WriteFile(filepath.Join(directory, "other"), []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case failure := <-observer.Changes():
		if _, changes, err := observer.refresh(failure); err != nil || len(changes) > 0 {
			t.Errorf("unrelated event changes=%v err=%v", changes, err)
		}
	case <-time.After(watchTestTimeout):
		t.Fatal("timed out waiting for the directory event")
	}
}

func TestAValidReloadAfterAFailureIsApplied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[ui]\nreasoning = \"plain\"\n"); err != nil {
		t.Fatal(err)
	}
	_, observer := observeConfig(t, path)

	failed := observer.Reload(errors.New("watch stopped"), testSegments())
	if failed.Status != ReloadFailed || failed.Failure == nil {
		t.Fatalf("failed reload status=%v failure=%v", failed.Status, failed.Failure)
	}
	if err := writeConfigFile(path, "[ui]\nreasoning = \"markdown\"\n"); err != nil {
		t.Fatal(err)
	}

	applied := observer.Reload(nil, testSegments())
	if applied.Status != ReloadApplied || applied.Failure != nil {
		t.Fatalf("applied reload status=%v failure=%v", applied.Status, applied.Failure)
	}
	if applied.LiveConfig.ReasoningRendering != output.ReasoningMarkdown {
		t.Errorf("applied reasoning=%v", applied.LiveConfig.ReasoningRendering)
	}
}

func TestAnAppliedReloadNamesTheFileThatChangedAndWhatItSupplies(t *testing.T) {
	directory := t.TempDir()
	globalPath := filepath.Join(directory, "config.toml")
	overridePath := filepath.Join(directory, "oh.toml")
	if err := writeConfigFile(globalPath, "[ui]\ncurrency = \"GBP\"\n"); err != nil {
		t.Fatal(err)
	}

	_, observer, err := ObserveSources(
		Source{Path: globalPath},
		Source{Path: overridePath, IsOverride: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(observer.Close)

	if err := os.WriteFile(overridePath, []byte("[ui]\ncurrency = \"EUR\"\nstreaming = \"asap\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	applied := observer.Reload(nil, testSegments())
	if applied.Status != ReloadApplied {
		t.Fatalf("reload status=%v failure=%v", applied.Status, applied.Failure)
	}
	if len(applied.Changes) != 1 {
		t.Fatalf("got %d changes, want the override alone: %v", len(applied.Changes), applied.Changes)
	}
	change := applied.Changes[0]
	if !strings.HasSuffix(change.Path, "oh.toml") || change.IsRemoved {
		t.Errorf("got path %q removed=%t", change.Path, change.IsRemoved)
	}
	if want := []string{"ui.currency", "ui.streaming"}; !slices.Equal(change.Settings, want) {
		t.Errorf("got settings %v, want %v", change.Settings, want)
	}

	if err := os.WriteFile(overridePath, []byte("[ui]\ncurrency = \"EUR\"\nstreaming = \"line\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	again := observer.Reload(nil, testSegments())
	if again.Status != ReloadApplied {
		t.Fatalf("second reload status=%v failure=%v", again.Status, again.Failure)
	}
	if want := []string{"ui.streaming"}; !slices.Equal(again.Changes[0].Settings, want) {
		t.Errorf("got settings %v, want only the one that changed: %v", again.Changes[0].Settings, want)
	}
}

func TestARetintedPaletteIsNamedAsOneSettingRatherThanEveryColour(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[ui.theme]\naccent = \"#c08050\"\ndim = \"#969896\"\n"); err != nil {
		t.Fatal(err)
	}
	_, observer := observeConfig(t, path)

	if err := writeConfigFile(path, "[ui.theme]\naccent = \"#a0d0f0\"\ndim = \"#404040\"\n"); err != nil {
		t.Fatal(err)
	}

	applied := observer.Reload(nil, testSegments())
	if applied.Status != ReloadApplied {
		t.Fatalf("reload status=%v failure=%v", applied.Status, applied.Failure)
	}
	if want := []string{"ui.theme"}; !slices.Equal(applied.Changes[0].Settings, want) {
		t.Errorf("got settings %v, want %v", applied.Changes[0].Settings, want)
	}
}

func TestAReloadOfAnUnchangedSettingBesideACommentNamesNoSetting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[ui]\ncurrency = \"GBP\"\n"); err != nil {
		t.Fatal(err)
	}
	_, observer := observeConfig(t, path)

	if err := writeConfigFile(path, "# a note to self\n[ui]\ncurrency = \"GBP\"\n"); err != nil {
		t.Fatal(err)
	}

	applied := observer.Reload(nil, testSegments())
	if applied.Status != ReloadApplied {
		t.Fatalf("reload status=%v failure=%v", applied.Status, applied.Failure)
	}
	if len(applied.Changes) != 1 || len(applied.Changes[0].Settings) != 0 {
		t.Errorf("got %v, want the file named with no setting", applied.Changes)
	}
}

func TestAReloadReportsAnOverrideThatWentAway(t *testing.T) {
	directory := t.TempDir()
	globalPath := filepath.Join(directory, "config.toml")
	overridePath := filepath.Join(directory, "oh.toml")
	if err := writeConfigFile(globalPath, "[ui]\ncurrency = \"GBP\"\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overridePath, []byte("[ui]\nstreaming = \"asap\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, observer, err := ObserveSources(
		Source{Path: globalPath},
		Source{Path: overridePath, IsOverride: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(observer.Close)

	if err := os.Remove(overridePath); err != nil {
		t.Fatal(err)
	}

	applied := observer.Reload(nil, testSegments())
	if applied.Status != ReloadApplied {
		t.Fatalf("reload status=%v failure=%v", applied.Status, applied.Failure)
	}
	if len(applied.Changes) != 1 || !applied.Changes[0].IsRemoved {
		t.Fatalf("got %v, want the override reported as gone", applied.Changes)
	}
	if want := []string{"ui.streaming"}; !slices.Equal(applied.Changes[0].Settings, want) {
		t.Errorf("got %v, want the settings it stopped supplying: %v", applied.Changes[0].Settings, want)
	}
}

func TestAChangedSnippetFileIsNamedBesideTheConfigThatReferencesIt(t *testing.T) {
	directory := t.TempDir()
	snippetsDirectory := filepath.Join(directory, "snippets")
	if err := os.Mkdir(snippetsDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	snippetPath := filepath.Join(snippetsDirectory, "review.md")
	if err := os.WriteFile(snippetPath, []byte("Review the first revision."), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(configPath, "[snippets]\nreview = { file = \"snippets/review.md\" }\n"); err != nil {
		t.Fatal(err)
	}
	_, observer := observeConfig(t, configPath)

	if err := os.WriteFile(snippetPath, []byte("Review the reloaded revision."), 0o600); err != nil {
		t.Fatal(err)
	}

	applied := observer.Reload(nil, testSegments())
	if applied.Status != ReloadApplied {
		t.Fatalf("reload status=%v failure=%v", applied.Status, applied.Failure)
	}
	if len(applied.Changes) != 1 || !strings.HasSuffix(applied.Changes[0].Path, "review.md") {
		t.Fatalf("got %v, want the snippet file alone", applied.Changes)
	}
	if want := []string{"snippets.review"}; !slices.Equal(applied.Changes[0].Settings, want) {
		t.Errorf("got %v, want the snippet it supplies: %v", applied.Changes[0].Settings, want)
	}
}

func TestASnippetFileTheChangedConfigAlreadyNamesIsNotRepeated(t *testing.T) {
	directory := t.TempDir()
	snippetPath := filepath.Join(directory, "review.md")
	if err := os.WriteFile(snippetPath, []byte("Review the first revision."), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(configPath, "[ui]\ncurrency = \"GBP\"\n"); err != nil {
		t.Fatal(err)
	}
	_, observer := observeConfig(t, configPath)

	if err := writeConfigFile(configPath, "[snippets]\nreview = { file = \"review.md\" }\n"); err != nil {
		t.Fatal(err)
	}

	applied := observer.Reload(nil, testSegments())
	if applied.Status != ReloadApplied {
		t.Fatalf("reload status=%v failure=%v", applied.Status, applied.Failure)
	}
	if len(applied.Changes) != 1 || !strings.HasSuffix(applied.Changes[0].Path, "config.toml") {
		t.Fatalf("got %v, want the config alone", applied.Changes)
	}
}

func TestObserveReportsAnInvalidInitialConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("not toml = ["), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := Observe(path)
	if err == nil || !strings.Contains(err.Error(), "config.toml") {
		t.Errorf("got %v", err)
	}
}

func TestClosingAnObserverClosesItsChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[ui]\ncurrency = \"GBP\"\n"); err != nil {
		t.Fatal(err)
	}
	_, observer, err := Observe(path)
	if err != nil {
		t.Fatal(err)
	}
	observer.Close()

	if _, isOpen := <-observer.Changes(); isOpen {
		t.Error("changes remained open")
	}
}
