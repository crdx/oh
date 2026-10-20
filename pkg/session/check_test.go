package session_test

import (
	"os"
	"path/filepath"
	"testing"

	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
)

func TestAStoredAndAnArchivedSessionPassTheHealthCheck(t *testing.T) {
	directory := t.TempDir()
	storedName := checkedSession(t, directory)
	archivedName := checkedSession(t, directory)
	if err := session.Archive(directory, archivedName); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{storedName, archivedName} {
		if err := session.Check(directory, name); err != nil {
			t.Errorf("%s did not pass the health check: %v", name, err)
		}
	}
}

func TestTheHealthCheckRejectsAnIncompleteJournalTail(t *testing.T) {
	directory := t.TempDir()
	name := checkedSession(t, directory)
	journalPath := filepath.Join(directory, name, "session.jsonl")

	journal, err := os.OpenFile(journalPath, os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.WriteString(`{"kind":"event"`); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := session.Read(directory, name); err != nil {
		t.Fatalf("the ordinary reader did not tolerate the incomplete tail: %v", err)
	}
	if err := session.Check(directory, name); err == nil {
		t.Fatal("the health check accepted the incomplete tail")
	}
}

func TestTheHealthCheckReadsAnArchiveToItsEnd(t *testing.T) {
	directory := t.TempDir()
	name := checkedSession(t, directory)
	if err := session.Archive(directory, name); err != nil {
		t.Fatal(err)
	}

	archivePath := session.ArchivePath(directory, name)
	info, err := os.Stat(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(archivePath, info.Size()-1); err != nil {
		t.Fatal(err)
	}

	if err := session.Check(directory, name); err == nil {
		t.Fatal("the health check accepted the truncated archive")
	}
}

func TestTheHealthCheckRejectsASessionStoredTwice(t *testing.T) {
	directory := t.TempDir()
	name := checkedSession(t, directory)
	storedDirectory := session.Dir(directory, name)
	archivePath := session.ArchivePath(directory, name)
	if err := os.WriteFile(archivePath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := session.Check(directory, name); err == nil {
		t.Fatal("the health check accepted both copies")
	}
	if _, err := os.Stat(storedDirectory); err != nil {
		t.Fatalf("the stored copy disappeared: %v", err)
	}
}

func checkedSession(t *testing.T, directory string) string {
	t.Helper()

	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return writer.Name()
}

func TestInspectionReportsAnIncompleteTailWithoutLosingTheSession(t *testing.T) {
	directory := t.TempDir()
	name := checkedSession(t, directory)
	journalPath := filepath.Join(directory, name, "session.jsonl")
	journal, err := os.OpenFile(journalPath, os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.WriteString(`{"kind":"event"`); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	inspection, err := session.Inspect(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	if !inspection.HasIncompleteTail || inspection.Session == nil {
		t.Errorf("unexpected inspection: %+v", inspection)
	}

	changed, err := session.RepairJournalTail(directory, name)
	if err != nil || !changed {
		t.Fatalf("truncate returned %t, %v", changed, err)
	}
	if err := session.Check(directory, name); err != nil {
		t.Errorf("truncated session remains unhealthy: %v", err)
	}
}

func TestInspectionSkipsAStoredSessionThatIsInUse(t *testing.T) {
	directory := t.TempDir()
	name := checkedSession(t, directory)
	writer, err := session.Open(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Close() }()
	if err := os.WriteFile(filepath.Join(directory, name, "meta.json"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	inspection, err := session.Inspect(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	if !inspection.IsInUse || inspection.Session != nil || inspection.MetaError != nil {
		t.Errorf("inspection read a mutable session: %+v", inspection)
	}
}

func TestInspectionReturnsRepairableMetadataSeparately(t *testing.T) {
	directory := t.TempDir()
	name := checkedSession(t, directory)
	if err := os.WriteFile(filepath.Join(directory, name, "meta.json"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	inspection, err := session.Inspect(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Session == nil || inspection.MetaError == nil {
		t.Errorf("unexpected inspection: %+v", inspection)
	}
}

func BenchmarkInspectStoredSession(benchmark *testing.B) {
	directory := benchmark.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		benchmark.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
		benchmark.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		benchmark.Fatal(err)
	}
	name := writer.Name()
	benchmark.ResetTimer()
	for benchmark.Loop() {
		if _, err := session.Inspect(directory, name); err != nil {
			benchmark.Fatal(err)
		}
	}
}

func BenchmarkInspectArchivedSession(benchmark *testing.B) {
	directory := benchmark.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		benchmark.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
		benchmark.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		benchmark.Fatal(err)
	}
	name := writer.Name()
	if err := session.Archive(directory, name); err != nil {
		benchmark.Fatal(err)
	}
	benchmark.ResetTimer()
	for benchmark.Loop() {
		if _, err := session.Inspect(directory, name); err != nil {
			benchmark.Fatal(err)
		}
	}
}

func TestReopeningASessionEndsATornJournalBeforeAppending(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		tail   string
		events int
	}{
		{name: "an incomplete final record", tail: `{"kind":"event","event":{"kind":"user_mess`, events: 2},
		{name: "a complete final record without its newline", tail: `{"kind":"event","time":"2026-01-01T00:00:00Z","event":{"kind":"model_message","text":"kept"}}`, events: 3},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			directory := t.TempDir()
			name := checkedSession(t, directory)
			journalPath := filepath.Join(directory, name, "session.jsonl")
			journal, err := os.OpenFile(journalPath, os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the test's own path
			if err != nil {
				t.Fatal(err)
			}
			if _, err := journal.WriteString(testCase.tail); err != nil {
				t.Fatal(err)
			}
			if err := journal.Close(); err != nil {
				t.Fatal(err)
			}

			writer, err := session.Open(directory, name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "after"}); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}

			reopened, err := session.Read(directory, name)
			if err != nil {
				t.Fatalf("the reopened journal could not be read: %v", err)
			}
			if len(reopened.Events) != testCase.events || reopened.Events[len(reopened.Events)-1].Text != "after" {
				t.Errorf("got events %+v", reopened.Events)
			}
			if err := session.Check(directory, name); err != nil {
				t.Errorf("the reopened session is unhealthy: %v", err)
			}
		})
	}
}

func TestReopeningASessionRefusesAMalformedFinalRecord(t *testing.T) {
	directory := t.TempDir()
	name := checkedSession(t, directory)
	journalPath := filepath.Join(directory, name, "session.jsonl")
	journal, err := os.OpenFile(journalPath, os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.WriteString(`{"kind":}`); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(journalPath) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	if writer, err := session.Open(directory, name); err == nil {
		_ = writer.Close()
		t.Fatal("expected a malformed final record to be refused")
	}
	after, err := os.ReadFile(journalPath) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("refusing the malformed record changed the journal")
	}
}
