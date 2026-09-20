package sessions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/pkg/session"
)

func TestGoldenAnArchivedSessionInAnOlderFormatIsReportedOnlyWhereArchivesAreRead(t *testing.T) {
	directory := t.TempDir()
	writeOutdatedJournal(t, directory, "able-dolphin")
	archive(t, directory, "able-dolphin")

	startupError := ValidateStoredFormats(directory)
	if startupError != nil {
		t.Errorf("expected an archived session to be left out of the startup check, got %v", startupError)
	}

	pickerError := ValidateFormats(directory)
	if pickerError == nil {
		t.Fatal("expected the archived session to be reported in the listing that reads every session")
	}

	comparePickerGolden(t, "archived-format-refusal.txt", strings.Join([]string{
		"=== startup ===\n",
		said(startupError),
		"=== picker ===\n",
		said(pickerError),
	}, ""))
}

func TestGoldenAStoredSessionInAnOlderFormatIsReportedAtStartup(t *testing.T) {
	directory := t.TempDir()
	writeOutdatedJournal(t, directory, "able-dolphin")

	startupError := ValidateStoredFormats(directory)
	if startupError == nil {
		t.Fatal("expected a stored session in an older format to be reported")
	}

	comparePickerGolden(t, "stored-format-refusal.txt", said(startupError))
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

	if err := os.MkdirAll(filepath.Join(directory, name), 0o700); err != nil {
		t.Fatal(err)
	}

	head := fmt.Sprintf(
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":1,"id":%q,"name":%q}`+"\n", name, name,
	)
	path := filepath.Join(directory, name, "session.jsonl")
	if err := os.WriteFile(path, []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}
}
