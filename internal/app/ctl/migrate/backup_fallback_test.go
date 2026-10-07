package migrate

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestABackupCopiesWhenHardLinkingIsNotPossible(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "journal.jsonl")
	backup := filepath.Join(directory, "copies", "journal.jsonl")
	if err := os.WriteFile(source, []byte("original journal"), 0o600); err != nil {
		t.Fatal(err)
	}

	attempts := 0
	err := keepLinkedWith(source, backup, func(from string, to string) error {
		attempts++
		if from != source || to != backup {
			t.Errorf("linking %q to %q, want %q to %q", from, to, source, backup)
		}
		return syscall.EXDEV
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Errorf("tried to link %d times, want once", attempts)
	}
	if content, err := os.ReadFile(backup); err != nil || string(content) != "original journal" { //nolint:gosec // the test's own backup
		t.Errorf("backup contains %q, %v", content, err)
	}
	if err := os.WriteFile(source, []byte("migrated journal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(backup); err != nil || string(content) != "original journal" { //nolint:gosec // the test's own backup
		t.Errorf("backup changed after the source: %q, %v", content, err)
	}

	if err := keepLinkedWith(source, backup, func(string, string) error {
		t.Fatal("an existing backup must not be linked over")
		return errors.New("unreachable")
	}); err == nil {
		t.Error("an existing backup was overwritten")
	}
}
