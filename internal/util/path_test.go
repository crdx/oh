package util_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/util"
)

func TestAnAbsolutePathCanBeNamedFromARelativeRoot(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)

	openedRoot, err := os.OpenRoot(".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = openedRoot.Close() })

	root := file.New(openedRoot, func(string) error { return nil })
	got, err := util.RootName(root, filepath.Join(directory, "somewhere", "file.go"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := filepath.Join("somewhere", "file.go")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAnAbsolutePathOutsideTheRootSaysSo(t *testing.T) {
	directory := t.TempDir()
	openedRoot, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = openedRoot.Close() })

	root := file.New(openedRoot, func(string) error { return nil })
	if _, err := util.RootName(root, filepath.Join(filepath.Dir(directory), "outside")); !errors.Is(err, file.ErrOutsideRoot) {
		t.Errorf("expected a path outside the root to say so, got %v", err)
	}
}
