package diskutil

import (
	"os"
	"path/filepath"
	"testing"
)

const measuredBytes = 4096

func TestTheDirectoriesHoldingAFileAreNotCountedAgainstIt(t *testing.T) {
	flat := t.TempDir()
	write(t, filepath.Join(flat, "one"))

	nested := t.TempDir()
	write(t, filepath.Join(nested, "first", "second", "third", "one"))

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
	write(t, path)

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

func TestAReadOnlyModuleCacheIsRemovedWhole(t *testing.T) {
	root := filepath.Join(t.TempDir(), "scratch")
	module := filepath.Join(root, "home", ".cache", "go-mod", "gopkg.in", "lumberjack.v2@v2.2.1")
	write(t, filepath.Join(module, "LICENSE"))
	write(t, filepath.Join(module, "nested", "lumberjack.go"))
	for _, path := range []string{filepath.Join(module, "nested"), module} {
		if err := os.Chmod(path, 0o500); err != nil { //nolint:gosec // a read-only cache is the point
			t.Fatal(err)
		}
	}

	if err := RemoveAll(root); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Errorf("a scratch directory holding a read-only module cache survived removal: %v", err)
	}
}

func TestRemovingSomethingThatIsNotThereSucceeds(t *testing.T) {
	if err := RemoveAll(filepath.Join(t.TempDir(), "absent")); err != nil {
		t.Errorf("removing an absent path failed: %v", err)
	}
}

func write(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, measuredBytes), 0o600); err != nil {
		t.Fatal(err)
	}
}
