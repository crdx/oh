package sessions

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"

	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/work"
)

func TestGoldenASessionInAnOlderFormatIsRefusedWhereverItIsRead(t *testing.T) {
	var drawn strings.Builder
	for _, isArchived := range []bool{false, true} {
		directory := t.TempDir()
		workspace := work.At(t.TempDir())
		writeOutdatedJournal(t, directory, "able-dolphin")
		heading := "stored"
		if isArchived {
			archive(t, directory, "able-dolphin")
			heading = "archived"
		}

		_, resumeError := LoadForResume(directory, workspace, "able-dolphin")
		_, forkError := GetForkSource(directory, workspace, "able-dolphin", "")
		var listed strings.Builder
		listingError := RefreshListing(directory, &listed, "able-dolphin")
		pickerError := ValidateFormats(directory)
		for _, refusal := range []error{resumeError, forkError, listingError, pickerError} {
			if refusal == nil {
				t.Fatalf("expected the %s session in an older format to be refused everywhere it is read", heading)
			}
		}

		_, _ = fmt.Fprintf(&drawn, "=== %s: resume ===\n%s", heading, said(resumeError))
		_, _ = fmt.Fprintf(&drawn, "=== %s: fork ===\n%s", heading, said(forkError))
		_, _ = fmt.Fprintf(&drawn, "=== %s: listing ===\n%s%s", heading, listed.String(), said(listingError))
		_, _ = fmt.Fprintf(&drawn, "=== %s: picker ===\n%s", heading, said(pickerError))
	}

	directory := t.TempDir()
	writeJournalInFormat(t, directory, "able-dolphin", session.JournalFormat+1)
	_, aheadError := LoadForResume(directory, work.At(t.TempDir()), "able-dolphin")
	if aheadError == nil {
		t.Fatal("expected a session in a newer format to be refused")
	}
	_, _ = fmt.Fprintf(&drawn, "=== newer: resume ===\n%s", said(aheadError))

	comparePickerGolden(t, "format-refusal.txt", drawn.String())
}

func TestASessionInAnOlderFormatLeavesEveryOtherSessionAlone(t *testing.T) {
	directory := t.TempDir()
	workspaceDir := t.TempDir()
	writeOutdatedJournal(t, directory, "able-dolphin")

	writer, err := store.Create(directory, store.Meta{WorkspaceDir: workspaceDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "begin"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.CompleteTurn(session.TurnSummary{}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if err := RefreshListing(directory, io.Discard, writer.Name()); err != nil {
		t.Errorf("expected the current session's listing to refresh, got %v", err)
	}
	if _, err := LoadForResume(directory, work.At(workspaceDir), writer.Name()); err != nil {
		t.Errorf("expected the current session to resume, got %v", err)
	}
	if _, err := GetForkSource(directory, work.At(workspaceDir), writer.Name(), ""); err != nil {
		t.Errorf("expected the current session to be forked, got %v", err)
	}
}

func said(err error) string {
	if err == nil {
		return ""
	}

	return err.Error() + "\n"
}

func archive(t *testing.T, directory string, name string) {
	t.Helper()

	if err := session.Archive(directory, name); err != nil {
		t.Fatal(err)
	}
	if !session.IsArchived(directory, name) {
		t.Fatalf("expected %s to be archived", name)
	}
}

func writeOutdatedJournal(t *testing.T, directory string, name string) {
	t.Helper()

	writeJournalInFormat(t, directory, name, 1)
}

func writeJournalInFormat(t *testing.T, directory string, name string, journalFormat int) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(directory, name), 0o700); err != nil {
		t.Fatal(err)
	}

	head := fmt.Sprintf(
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":%d,"id":%q,"name":%q}`+"\n",
		journalFormat, name, name,
	)
	path := filepath.Join(directory, name, "session.jsonl")
	if err := os.WriteFile(path, []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}
}
