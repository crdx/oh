package session

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGoldens = flag.Bool("update", false, "update golden files")

func TestGoldenArchivedNamesRemainReserved(t *testing.T) {
	restoreWordsAfterwards(t)
	adjectives = []string{"brave"}
	animals = []string{"otter"}

	var output strings.Builder

	archivedDirectory := t.TempDir()
	archived := storeSession(t, archivedDirectory)
	if err := Archive(archivedDirectory, archived.Name()); err != nil {
		t.Fatal(err)
	}
	_, creationError := Create(archivedDirectory, nil, nil)
	writeGoldenState(&output, "after archiving", archivedDirectory, archived.Name(), "create", creationError)

	lazyDirectory := t.TempDir()
	lazy, err := Create(lazyDirectory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	stored := storeSession(t, lazyDirectory)
	if err := Archive(lazyDirectory, stored.Name()); err != nil {
		t.Fatal(err)
	}
	writeGoldenState(
		&output,
		"name archived after selection",
		lazyDirectory,
		lazy.Name(),
		"persist",
		lazy.EnsurePersisted(),
	)

	compareNameGolden(t, output.String())
}

func writeGoldenState(
	output *strings.Builder,
	title string,
	directory string,
	name string,
	action string,
	actionError error,
) {
	_, _ = fmt.Fprintf(output, "=== %s ===\n", title)
	_, _ = fmt.Fprintf(output, "%s: %s\n", action, errorText(actionError))
	_, _ = fmt.Fprintf(output, "directory: %s\n", pathState(Dir(directory, name)))
	_, _ = fmt.Fprintf(output, "archive: %s\n", pathState(ArchivePath(directory, name)))
}

func errorText(err error) string {
	if err == nil {
		return "no error"
	}

	return err.Error()
}

func pathState(path string) string {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "absent"
	case err != nil:
		return err.Error()
	case info.IsDir():
		return "directory"
	case info.Mode().IsRegular():
		return "regular file"
	default:
		return info.Mode().String()
	}
}

func compareNameGolden(t *testing.T, got string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", "archived-names.golden")
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("session states differ from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, got, want)
	}
}
