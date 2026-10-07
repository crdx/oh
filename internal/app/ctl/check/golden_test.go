package check

import (
	"archive/tar"
	"compress/gzip"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/ctl/console"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestGoldenUsageMatchesTheGolden(t *testing.T) {
	assertGolden(t, "usage.txt", strings.ReplaceAll(usage, "$0", "oh"))
}

func TestGoldenAHealthyEmptyStoreMatchesTheGolden(t *testing.T) {
	var screen, failure strings.Builder
	err := run(paths{sessionsDir: filepath.Join(t.TempDir(), "sessions")}, nil, console.Output{
		Screen:  &screen,
		Failure: &failure,
	})
	if err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "empty.txt", report(screen.String(), failure.String(), err))
}

func TestGoldenAnInvalidConfigurationMatchesTheGolden(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.toml")
	if err := os.WriteFile(configPath, []byte("version = nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var screen, failure strings.Builder
	err := run(paths{
		configSources: []config.Source{{Path: configPath}},
		sessionsDir:   filepath.Join(directory, "sessions"),
	}, nil, console.Output{Screen: &screen, Failure: &failure})
	if err == nil {
		t.Fatal("expected the invalid configuration to fail the check")
	}

	drawn := strings.ReplaceAll(report(screen.String(), failure.String(), err), configPath, "/config/config.toml")
	assertGolden(t, "config.txt", drawn)
}

func TestGoldenSessionProblemsMatchTheGolden(t *testing.T) {
	directory := t.TempDir()
	healthyName := storedSession(t, directory)
	archivedName := storedSession(t, directory)
	if err := session.Archive(directory, archivedName); err != nil {
		t.Fatal(err)
	}
	brokenName := storedSession(t, directory)
	journalPath := filepath.Join(directory, brokenName, "session.jsonl")
	journal, err := os.OpenFile(journalPath, os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.WriteString("not json\n"); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	unexpectedName := "000-unfinished.tgz"
	if err := os.WriteFile(filepath.Join(directory, unexpectedName), []byte("unfinished"), 0o600); err != nil {
		t.Fatal(err)
	}
	foreign, err := os.Create(session.ArchivePath(directory, "zzz-otter"))
	if err != nil {
		t.Fatal(err)
	}
	compressor := gzip.NewWriter(foreign)
	archive := tar.NewWriter(compressor)
	if err := archive.WriteHeader(&tar.Header{Name: "elsewhere/meta.json", Mode: 0o600, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressor.Close(); err != nil {
		t.Fatal(err)
	}
	if err := foreign.Close(); err != nil {
		t.Fatal(err)
	}

	var screen, failure strings.Builder
	err = run(paths{sessionsDir: directory}, nil, console.Output{Screen: &screen, Failure: &failure})
	if err == nil {
		t.Fatal("expected the unhealthy sessions to fail the check")
	}

	drawn := report(screen.String(), failure.String(), err)
	if !strings.Contains(drawn, "zzz-otter.tgz: archive: it holds") {
		t.Fatalf("the malformed archive was not checked: %s", drawn)
	}
	drawn = strings.NewReplacer(
		healthyName, "healthy-otter",
		archivedName, "archived-mole",
		brokenName, "broken-newt",
	).Replace(drawn)
	assertGolden(t, "problems.txt", drawn)
}

func TestOnlyTheNamedSessionsAreChecked(t *testing.T) {
	directory := t.TempDir()
	healthyName := storedSession(t, directory)
	brokenName := storedSession(t, directory)
	if err := os.WriteFile(filepath.Join(directory, brokenName, "meta.json"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	var screen, failure strings.Builder
	if err := run(paths{sessionsDir: directory}, []string{healthyName}, console.Output{
		Screen: &screen, Failure: &failure,
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(failure.String(), brokenName) {
		t.Errorf("the unrequested session was checked: %s", failure.String())
	}
}

func storedSession(t *testing.T, directory string) string {
	t.Helper()

	writer, err := store.Create(directory, store.Meta{WorkspaceDir: "/workspace", Model: "gpt-5.6-sol"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return writer.Name()
}

func report(screen string, failure string, err error) string {
	return fmt.Sprintf("=== screen ===\n%s=== failure ===\n%s=== error ===\n%v\n", screen, failure, err)
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
