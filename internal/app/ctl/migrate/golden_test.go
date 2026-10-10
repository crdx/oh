package migrate

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/ctl/console"
	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/pkg/session"
)

const (
	goldenName      = "tame-impala"
	goldenStateDir  = "/state"
	goldenConfigDir = "/config"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestGoldenUsageMatchesTheGolden(t *testing.T) {
	assertGolden(t, "usage.txt", strings.ReplaceAll(usage, "$0", "oh"))
}

func TestGoldenADryRunMatchesTheGolden(t *testing.T) {
	directory := goldenSessions(t, oldJournal())

	assertGolden(t, "dry-run.txt", migration(t, directory, &inputOpts{DryRun: true}))
}

func TestGoldenAMigrationMatchesTheGolden(t *testing.T) {
	directory := goldenSessions(t, oldJournal())

	assertGolden(t, "migrated.txt", migration(t, directory, &inputOpts{}))

	if _, err := session.ReadMeta(directory, goldenName); err != nil {
		t.Errorf("expected the migrated session to be listed, got %v", err)
	}
}

func TestGoldenASessionInUseMatchesTheGolden(t *testing.T) {
	directory := goldenSessions(t, oldJournal())

	heldLock, err := session.AcquireLock(directory, goldenName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = heldLock.Release() })

	assertGolden(t, "in-use.txt", migration(t, directory, &inputOpts{Sessions: []string{goldenName}}))
}

func TestGoldenNothingLeftToMigrateMatchesTheGolden(t *testing.T) {
	head := fmt.Sprintf(
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":%d,"id":"one","name":%q,"meta":{"workspaceDir":"/workspace"}}`,
		session.JournalFormat,
		goldenName,
	)
	directory := goldenSessions(t, head)

	assertGolden(t, "nothing-to-do.txt", migration(t, directory, &inputOpts{}))
}

func TestGoldenAnArchivedSessionIsMigratedLikeAnyOtherMatchingTheGolden(t *testing.T) {
	directory := goldenSessions(t, currentCodexJournal()...)
	archiveWithAnOldListing(t, directory)

	assertGolden(t, "archived-session.txt", migration(t, directory, &inputOpts{}))
}

func TestGoldenAnArchivedSessionDryRunMatchesTheGolden(t *testing.T) {
	directory := goldenSessions(t, currentCodexJournal()...)
	archiveWithAnOldListing(t, directory)

	assertGolden(t, "archived-session-dry-run.txt", migration(t, directory, &inputOpts{DryRun: true}))
}

func TestGoldenAnArchivedSessionInAnOlderFormatIsMigratedAndPutBack(t *testing.T) {
	directory := goldenSessions(t, oldJournal())
	archiveWithAnOldListing(t, directory)

	assertGolden(t, "archived-outdated.txt", migration(t, directory, &inputOpts{}))

	if !session.IsArchived(directory, goldenName) {
		t.Error("expected the migrated session to be archived again")
	}
	entries, err := session.Entries(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !entries[0].IsArchived {
		t.Fatalf("got entries %+v", entries)
	}
	if entries[0].Format != session.JournalFormat {
		t.Errorf("the archived journal is at format %d, want %d", entries[0].Format, session.JournalFormat)
	}
}

func archiveWithAnOldListing(t *testing.T, directory string) {
	t.Helper()

	meta := fmt.Sprintf(
		`{"version":1,"name":%q,"data":{"workspaceDir":"/workspace"},`+
			`"started":"2026-08-01T00:00:00Z","touched":"2026-08-01T00:00:00Z"}`+"\n",
		goldenName,
	)
	path := filepath.Join(directory, goldenName, "meta.json")
	if err := os.WriteFile(path, []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.Archive(directory, goldenName); err != nil {
		t.Fatal(err)
	}
}

func TestGoldenAConfigMigrationMatchesTheGolden(t *testing.T) {
	directory := goldenSessions(t)
	storedConfig(t, `provider = "codex"
model = "gpt-5.6-sol"
effort = "medium"
`)

	assertGolden(t, "config.txt", migration(t, directory, &inputOpts{}))
}

func TestGoldenNoStoredSessionsMatchesTheGolden(t *testing.T) {
	directory := goldenSessions(t)

	assertGolden(t, "no-sessions.txt", migration(t, directory, &inputOpts{}))
}

func currentCodexJournal() []string {
	return []string{
		fmt.Sprintf(
			`{"kind":"head","time":"2026-08-01T00:00:00Z","version":%d,"id":"one","name":%q,"meta":{"provider":"codex","model":"gpt-5.6-sol","effort":"high","workspaceDir":"/workspace"}}`,
			session.JournalFormat,
			goldenName,
		),
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"mode_change","text":"rx"}}`,
	}
}

func oldJournal() string {
	return `{"kind":"head","time":"2026-08-01T00:00:00Z","id":"one","name":"` + goldenName +
		`","meta":{"workspaceDir":"/workspace"}}`
}

func goldenSessions(t *testing.T, lines ...string) string {
	t.Helper()

	stateDir := t.TempDir()
	t.Setenv(location.StateDirVariable, stateDir)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	directory := location.GetSessionsDir()
	if len(lines) == 0 {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		return directory
	}

	if err := os.MkdirAll(filepath.Join(directory, goldenName), 0o700); err != nil {
		t.Fatal(err)
	}

	body := strings.Join(lines, "\n") + "\n"
	journal := filepath.Join(directory, goldenName, "session.jsonl")
	if err := os.WriteFile(journal, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return directory
}

func storedConfig(t *testing.T, body string) {
	t.Helper()

	path := location.GetConfigFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func migration(t *testing.T, directory string, options *inputOpts) string {
	t.Helper()

	var screen, failure strings.Builder
	if err := run(options, console.Output{Screen: &screen, Failure: &failure}); err != nil {
		t.Fatal(err)
	}

	text := strings.Join([]string{
		"=== screen ===\n", screen.String(),
		"=== failure ===\n", failure.String(),
	}, "")

	text = strings.ReplaceAll(text, filepath.Dir(directory), goldenStateDir)
	return strings.ReplaceAll(text, filepath.Dir(location.GetConfigFile()), goldenConfigDir)
}

func assertGolden(t *testing.T, name string, drawn string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(drawn), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if drawn != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}

const seenModelsBeforeWires = `{"version":1,"providers":{` +
	`"opencode-go":[` +
	`{"id":"deepseek-v4-pro","efforts":["high","max"],"output":384000},` +
	`{"id":"qwen3.8-max","efforts":["high"],"output":64000},` +
	`{"id":"minimax-m3","effortless":true,"output":64000},` +
	`{"id":"muse-spark-1.3-contributor","efforts":["high"],"output":128000},` +
	`{"id":"grok-4.7","efforts":["high"],"output":128000},` +
	`{"id":"gpt-6-luna","efforts":["high"],"output":128000}],` +
	`"anthropic":[{"id":"claude-opus-5","efforts":["high"],"output":128000}]}}`

func storedSeenModels(t *testing.T, body string) string {
	t.Helper()

	path := location.GetSeenModelsPath()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func indentedFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // a file the test wrote
	if err != nil {
		t.Fatal(err)
	}

	var indented bytes.Buffer
	if err := json.Indent(&indented, data, "", "  "); err != nil {
		t.Fatal(err)
	}

	return indented.String() + "\n"
}

func TestGoldenSeenModelsMigrateOntoTheWiresTheyWereSpokenOver(t *testing.T) {
	directory := goldenSessions(t)
	path := storedSeenModels(t, seenModelsBeforeWires)

	assertGolden(t, "seen-models.txt", migration(t, directory, &inputOpts{}))
	assertGolden(t, "seen-models.json", indentedFile(t, path))

	if kept := indentedFile(t, path+".pre-v2"); kept != indentedFile(t, storedSeenModels(t, seenModelsBeforeWires)) {
		t.Errorf("expected the copy kept aside to be the original, got %s", kept)
	}
}

func TestGoldenSeenModelsDryRunLeavesThemAlone(t *testing.T) {
	directory := goldenSessions(t)
	path := storedSeenModels(t, seenModelsBeforeWires)

	assertGolden(t, "seen-models-dry-run.txt", migration(t, directory, &inputOpts{DryRun: true}))

	data, err := os.ReadFile(path) //nolint:gosec // a file the test wrote
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != seenModelsBeforeWires {
		t.Errorf("expected a dry run to leave the seen models alone, got %s", data)
	}
	if _, err := os.Stat(path + ".pre-v2"); !os.IsNotExist(err) {
		t.Errorf("expected a dry run to keep no copy, got %v", err)
	}
}

func TestSeenModelsAlreadyCurrentAreLeftAlone(t *testing.T) {
	directory := goldenSessions(t)
	current := `{"version":2,"providers":{"opencode-go":[{"id":"qwen3.8-max","wire":"completions"}]}}`
	path := storedSeenModels(t, current)

	if output := migration(t, directory, &inputOpts{}); strings.Contains(output, "seen models") {
		t.Errorf("expected nothing said of current seen models, got %s", output)
	}

	data, err := os.ReadFile(path) //nolint:gosec // a file the test wrote
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != current {
		t.Errorf("expected current seen models to be left alone, got %s", data)
	}
}

func TestSeenModelsFromANewerOhAdviseAnUpgrade(t *testing.T) {
	goldenSessions(t)
	storedSeenModels(t, `{"version":3,"providers":{}}`)

	var screen, failure strings.Builder
	err := run(&inputOpts{}, console.Output{Screen: &screen, Failure: &failure})
	if err == nil || !strings.Contains(err.Error(), "seen models") || !strings.Contains(err.Error(), "upgrade oh") {
		t.Errorf("expected newer seen models to advise an upgrade, got %v", err)
	}
}
