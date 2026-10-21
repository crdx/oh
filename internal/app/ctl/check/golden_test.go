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
	assertGolden(t, "usage.txt", strings.ReplaceAll(Usage, "$0", "oh"))
}

func TestGoldenAHealthyEmptyStoreMatchesTheGolden(t *testing.T) {
	var screen, failure strings.Builder
	err := run(paths{sessionsDir: filepath.Join(t.TempDir(), "sessions")}, nil, options{}, console.Output{
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
	}, nil, options{}, console.Output{Screen: &screen, Failure: &failure})
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
	err = run(paths{sessionsDir: directory}, nil, options{}, console.Output{Screen: &screen, Failure: &failure})
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
	if err := run(paths{sessionsDir: directory}, []string{healthyName}, options{}, console.Output{
		Screen: &screen, Failure: &failure,
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(failure.String(), brokenName) {
		t.Errorf("the unrequested session was checked: %s", failure.String())
	}
}

func TestGoldenNamedSessionsAreDeduplicatedBeforeFixingMatchesTheGolden(t *testing.T) {
	directory := t.TempDir()
	name := unfinishedSession(t, directory)
	candidates, err := sessionCandidates(directory, []string{name, name})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].name != name {
		t.Fatalf("got duplicate candidates %+v", candidates)
	}

	var screen, failure strings.Builder
	runError := run(paths{sessionsDir: directory}, []string{name, name}, options{isFixing: true}, console.Output{
		Screen: &screen, Failure: &failure,
	})
	if runError != nil {
		t.Fatalf("duplicate requested session failed its fix: %v\n%s", runError, failure.String())
	}
	drawn := strings.NewReplacer(
		name, "duplicate-otter",
		directory, "/state/sessions",
	).Replace(report(screen.String(), failure.String(), runError))
	assertGolden(t, "duplicate-fix.txt", drawn)
}

func TestGoldenRecoverableAndFixedSessionsMatchTheGolden(t *testing.T) {
	directory := t.TempDir()
	warningName := unfinishedSession(t, directory)

	var warningScreen, warningFailure strings.Builder
	warningError := run(paths{sessionsDir: directory}, []string{warningName}, options{}, console.Output{
		Screen: &warningScreen, Failure: &warningFailure,
	})
	warning := strings.ReplaceAll(report(warningScreen.String(), warningFailure.String(), warningError), warningName, "recoverable-otter")
	assertGolden(t, "recoverable.txt", warning)

	fixName := unfinishedSession(t, directory)
	bundle := session.Dir(directory, fixName)
	if err := os.WriteFile(filepath.Join(bundle, "meta.json"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(bundle, "chat.md")); err != nil {
		t.Fatal(err)
	}
	journal, err := os.OpenFile(filepath.Join(bundle, "session.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the test's own journal
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.WriteString(`{"kind":"event"`); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	var dryScreen, dryFailure strings.Builder
	dryError := run(paths{sessionsDir: directory}, []string{fixName}, options{isFixing: true, isDryRun: true}, console.Output{
		Screen: &dryScreen, Failure: &dryFailure,
	})
	var fixedScreen, fixedFailure strings.Builder
	fixedError := run(paths{sessionsDir: directory}, []string{fixName}, options{isFixing: true}, console.Output{
		Screen: &fixedScreen, Failure: &fixedFailure,
	})
	drawn := "=== dry run ===\n" + report(dryScreen.String(), dryFailure.String(), dryError) +
		"=== fix ===\n" + report(fixedScreen.String(), fixedFailure.String(), fixedError)
	drawn = strings.ReplaceAll(drawn, fixName, "repairable-newt")
	drawn = strings.ReplaceAll(drawn, directory, "/state/sessions")
	assertGolden(t, "fix.txt", drawn)
}

func TestAnUnfinishedTurnIsARecoverableWarning(t *testing.T) {
	directory := t.TempDir()
	name := unfinishedSession(t, directory)

	var screen, failure strings.Builder
	err := run(paths{sessionsDir: directory}, []string{name}, options{}, console.Output{Screen: &screen, Failure: &failure})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(screen.String(), "warning") || !strings.Contains(screen.String(), "unfinished turn is recoverable") {
		t.Errorf("recovery warning was not shown: %q", screen.String())
	}
	if failure.Len() != 0 {
		t.Errorf("recoverable session failed the check: %q", failure.String())
	}
}

func TestFixRecoversAnUnfinishedTurnAndKeepsACopy(t *testing.T) {
	directory := t.TempDir()
	name := unfinishedSession(t, directory)

	var screen, failure strings.Builder
	err := run(paths{sessionsDir: directory}, []string{name}, options{isFixing: true}, console.Output{Screen: &screen, Failure: &failure})
	if err != nil {
		t.Fatalf("fix failed: %v\n%s", err, failure.String())
	}
	stored, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.CanResume() || !stored.HasAbandonedTurn {
		t.Errorf("unexpected recovered session: %+v", stored)
	}
	if _, err := os.Stat(filepath.Join(directory+"_pre_check", name, "session.jsonl")); err != nil {
		t.Errorf("no backup was kept: %v", err)
	}
	if !strings.Contains(screen.String(), "fixed") || !strings.Contains(screen.String(), "abandon an unfinished turn") {
		t.Errorf("fix was not reported: %q", screen.String())
	}
}

func TestDryRunReportsButDoesNotRepair(t *testing.T) {
	directory := t.TempDir()
	name := unfinishedSession(t, directory)

	var screen, failure strings.Builder
	err := run(paths{sessionsDir: directory}, []string{name}, options{isFixing: true, isDryRun: true}, console.Output{Screen: &screen, Failure: &failure})
	if err != nil {
		t.Fatalf("dry run failed a session that the check only warns about: %v", err)
	}
	stored, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CanResume() {
		t.Error("dry run repaired the session")
	}
	if _, err := os.Stat(directory + "_pre_check"); !os.IsNotExist(err) {
		t.Errorf("dry run created a backup: %v", err)
	}
	if !strings.Contains(screen.String(), "would fix") {
		t.Errorf("dry run did not report the repair: %q", screen.String())
	}
}

func TestFixRepairsDerivedFilesAndAnIncompleteTail(t *testing.T) {
	directory := t.TempDir()
	name := storedSession(t, directory)
	bundle := session.Dir(directory, name)
	if err := os.WriteFile(filepath.Join(bundle, "meta.json"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(bundle, "chat.md")); err != nil {
		t.Fatal(err)
	}
	journal, err := os.OpenFile(filepath.Join(bundle, "session.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the test's own journal
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.WriteString(`{"kind":"event"`); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	var screen, failure strings.Builder
	if err := run(paths{sessionsDir: directory}, []string{name}, options{isFixing: true}, console.Output{Screen: &screen, Failure: &failure}); err != nil {
		t.Fatalf("fix failed: %v\n%s", err, failure.String())
	}
	if err := session.Check(directory, name); err != nil {
		t.Errorf("session remains unhealthy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bundle, "chat.md")); err != nil {
		t.Errorf("transcript was not rebuilt: %v", err)
	}
	for _, want := range []string{"incomplete final journal record", "listing metadata", "transcript"} {
		if !strings.Contains(screen.String(), want) {
			t.Errorf("fix output omitted %q: %s", want, screen.String())
		}
	}
}

func TestFixSkipsASessionThatIsInUse(t *testing.T) {
	directory := t.TempDir()
	name := unfinishedSession(t, directory)
	writer, err := store.Open(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Close() }()

	var screen, failure strings.Builder
	if err := run(paths{sessionsDir: directory}, []string{name}, options{isFixing: true}, console.Output{Screen: &screen, Failure: &failure}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(screen.String(), "skipped") || !strings.Contains(screen.String(), "in use") {
		t.Errorf("running session was not skipped: %q", screen.String())
	}
	if _, err := os.Stat(directory + "_pre_check"); !os.IsNotExist(err) {
		t.Errorf("skipped session was backed up: %v", err)
	}
}

func TestFixRefusesToReplaceAnEarlierBackup(t *testing.T) {
	directory := t.TempDir()
	name := unfinishedSession(t, directory)
	backup := filepath.Join(directory+"_pre_check", name)
	if err := os.MkdirAll(backup, 0o700); err != nil {
		t.Fatal(err)
	}

	var screen, failure strings.Builder
	err := run(paths{sessionsDir: directory}, []string{name}, options{isFixing: true}, console.Output{Screen: &screen, Failure: &failure})
	if err == nil {
		t.Fatal("fix replaced an earlier backup")
	}
	if !strings.Contains(failure.String(), "a copy is already kept") {
		t.Errorf("backup collision was not explained: %q", failure.String())
	}
	stored, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CanResume() {
		t.Error("session was repaired despite the backup collision")
	}
}

func TestFixRefusesToMutateAnArchivedSession(t *testing.T) {
	directory := t.TempDir()
	name := unfinishedSession(t, directory)
	if err := session.Archive(directory, name); err != nil {
		t.Fatal(err)
	}
	archivePath := session.ArchivePath(directory, name)
	before, err := os.ReadFile(archivePath) //nolint:gosec // the test's own archive
	if err != nil {
		t.Fatal(err)
	}

	var screen, failure strings.Builder
	err = run(paths{sessionsDir: directory}, []string{name}, options{isFixing: true}, console.Output{
		Screen: &screen, Failure: &failure,
	})
	if err == nil || !strings.Contains(failure.String(), "restore the archived session") {
		t.Fatalf("archived repair returned %v and %q", err, failure.String())
	}
	after, err := os.ReadFile(archivePath) //nolint:gosec // the test's own archive
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("refused archived repair changed the archive")
	}
	if _, err := os.Stat(directory + "_pre_check"); !os.IsNotExist(err) {
		t.Errorf("refused archived repair created a backup: %v", err)
	}
}

func TestFixRecoversAnUnfinishedTurnWhoseFinalRecordLostItsNewline(t *testing.T) {
	directory := t.TempDir()
	name := unfinishedSession(t, directory)
	journalPath := filepath.Join(directory, name, "session.jsonl")
	journal, err := os.ReadFile(journalPath) //nolint:gosec // the test's own session
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, journal[:len(journal)-1], 0o600); err != nil { //nolint:gosec // the test's own session
		t.Fatal(err)
	}

	var screen, failure strings.Builder
	err = run(paths{sessionsDir: directory}, []string{name}, options{isFixing: true}, console.Output{Screen: &screen, Failure: &failure})
	if err != nil {
		t.Fatalf("fix failed: %v\n%s", err, failure.String())
	}
	stored, err := store.Read(directory, name)
	if err != nil {
		t.Fatalf("fix left an unreadable journal: %v", err)
	}
	if !stored.CanResume() || !stored.HasAbandonedTurn {
		t.Errorf("unexpected recovered session: %+v", stored)
	}
}

func TestANewerListingIsRefusedRatherThanRebuilt(t *testing.T) {
	directory := t.TempDir()
	name := storedSession(t, directory)
	metaPath := filepath.Join(directory, name, "meta.json")
	newerMeta := []byte(`{"version":99,"name":"` + name + `"}`)
	if err := os.WriteFile(metaPath, newerMeta, 0o600); err != nil {
		t.Fatal(err)
	}

	var screen, failure strings.Builder
	err := run(paths{sessionsDir: directory}, []string{name}, options{isFixing: true}, console.Output{Screen: &screen, Failure: &failure})
	if err == nil || !strings.Contains(failure.String(), "newer build") {
		t.Errorf("expected the newer listing to be refused, got %v: %q", err, failure.String())
	}
	kept, err := os.ReadFile(metaPath) //nolint:gosec // the test's own session
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != string(newerMeta) {
		t.Errorf("the newer listing was rewritten as %s", kept)
	}
}

func unfinishedSession(t *testing.T, directory string) string {
	t.Helper()
	writer, err := store.Create(directory, store.Meta{WorkspaceDir: "/workspace", Model: "gpt-5.6-sol"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "unfinished"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return writer.Name()
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
	if err := writer.CompleteTurn(session.TurnSummary{}); err != nil {
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
