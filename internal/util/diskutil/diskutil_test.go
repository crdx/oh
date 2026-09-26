package diskutil

import (
	"os"
	"path/filepath"
	"testing"
)

const measuredBytes = 4096

func TestTheDirectoriesHoldingAFileAreNotCountedAgainstIt(t *testing.T) {
	flat := t.TempDir()
	write(t, filepath.Join(flat, "one"), measuredBytes)

	nested := t.TempDir()
	write(t, filepath.Join(nested, "first", "second", "third", "one"), measuredBytes)

	flatBytes, err := Occupied(flat)
	if err != nil {
		t.Fatal(err)
	}
	nestedBytes, err := Occupied(nested)
	if err != nil {
		t.Fatal(err)
	}

	if flatBytes != nestedBytes {
		t.Errorf("a directory measured %d bytes flat and %d bytes nested", flatBytes, nestedBytes)
	}
	if flatBytes < measuredBytes {
		t.Errorf("a directory holding %d bytes measured %d bytes", measuredBytes, flatBytes)
	}
}

func TestALoneFileIsMeasuredByItself(t *testing.T) {
	path := filepath.Join(t.TempDir(), "one")
	write(t, path, measuredBytes)

	occupied, err := Occupied(path)
	if err != nil {
		t.Fatal(err)
	}
	if occupied < measuredBytes {
		t.Errorf("a file of %d bytes measured %d bytes", measuredBytes, occupied)
	}
}

func TestSomethingThatIsNotThereOccupiesNothing(t *testing.T) {
	occupied, err := Occupied(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatal(err)
	}
	if occupied != 0 {
		t.Errorf("an absent path measured %d bytes", occupied)
	}
}

func write(t *testing.T, path string, bytes int) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, bytes), 0o600); err != nil {
		t.Fatal(err)
	}
}
