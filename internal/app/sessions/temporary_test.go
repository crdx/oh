package sessions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/location"
)

func TestPrepareTemporaryDirectoryCreatesTheSessionDirectoryPrivately(t *testing.T) {
	stateDirectory := t.TempDir()
	t.Setenv(location.StateDirVariable, stateDirectory)

	temporaryDirectory, err := PrepareTemporaryDirectory("tame-impala")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(stateDirectory, "farm", "tame-impala")
	if temporaryDirectory != want {
		t.Errorf("got %q, want %q", temporaryDirectory, want)
	}

	info, err := os.Stat(temporaryDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("got permissions %04o, want 0700", got)
	}
}

func TestPrepareTemporaryDirectoryKeepsAnExistingDirectory(t *testing.T) {
	stateDirectory := t.TempDir()
	t.Setenv(location.StateDirVariable, stateDirectory)

	temporaryDirectory, err := PrepareTemporaryDirectory("tame-impala")
	if err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(temporaryDirectory, "marker")
	if err := os.WriteFile(markerPath, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := PrepareTemporaryDirectory("tame-impala"); err != nil {
		t.Fatal(err)
	}
	if marker, err := os.ReadFile(markerPath); err != nil || string(marker) != "kept" { //nolint:gosec // the test's own path
		t.Errorf("existing contents: got %q, error %v", marker, err)
	}
}

func TestPrepareTemporaryDirectoryReportsCreationFailure(t *testing.T) {
	stateDirectory := t.TempDir()
	t.Setenv(location.StateDirVariable, stateDirectory)

	temporaryParentPath := filepath.Join(stateDirectory, "farm")
	if err := os.WriteFile(temporaryParentPath, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	temporaryDirectory, err := PrepareTemporaryDirectory("tame-impala")
	if err == nil || !strings.Contains(err.Error(), "could not prepare the tmp dir:") {
		t.Fatalf("got %v, want the preparation error", err)
	}
	if temporaryDirectory != "" {
		t.Errorf("got path %q after failure", temporaryDirectory)
	}
}
