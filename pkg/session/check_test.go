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
