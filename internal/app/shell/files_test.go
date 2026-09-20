package shell_test

import (
	"os"
	"path/filepath"
	"testing"

	"crdx.org/io/internal/app/caps"
	"crdx.org/io/internal/app/shell"
	"crdx.org/io/internal/file"
)

func TestHomeMountIsReadableByFileTools(t *testing.T) {
	workspaceRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	home := t.TempDir()
	path := filepath.Join(home, "reference")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	homeRoot, err := shell.MountHomeDirectory(files, home, caps.NewMode(caps.Read))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = homeRoot.Close() }()

	resolvedRoot, name, err := files.Resolve(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := resolvedRoot.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("got %q, want hello", data)
	}
}

func TestTemporaryMountIsWritableWithoutAShell(t *testing.T) {
	workspaceRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	temporaryDirectory := t.TempDir()
	temporaryRoot, err := shell.MountTemporaryDirectory(files, temporaryDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = temporaryRoot.Close() }()

	resolvedRoot, name, err := files.Resolve("/tmp/proof")
	if err != nil {
		t.Fatal(err)
	}
	if err := resolvedRoot.WriteFile(name, []byte("written"), 0o600); err != nil {
		t.Fatalf("tmp was not writable: %v", err)
	}
}

func TestPrivateCacheIsWritableByFileToolsAtEveryWorkspaceWriteState(t *testing.T) {
	for name, currentCaps := range map[string]caps.Set{
		"read-only workspace": caps.Read,
		"writable workspace":  caps.Read | caps.Write,
	} {
		t.Run(name, func(t *testing.T) {
			workspaceRoot, err := os.OpenRoot(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = workspaceRoot.Close() }()

			mode := caps.NewMode(currentCaps)
			files := file.New(workspaceRoot, caps.RefuseWrite(mode))
			home := t.TempDir()
			if err := os.Mkdir(filepath.Join(home, ".cache"), 0o700); err != nil {
				t.Fatal(err)
			}
			homeRoot, err := shell.MountHomeDirectory(files, home, mode)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = homeRoot.Close() }()
			cacheRoot, err := shell.MountHomeCache(files, home)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cacheRoot.Close() }()

			resolvedRoot, resolvedName, err := files.Resolve(filepath.Join(home, ".cache", "proof"))
			if err != nil {
				t.Fatal(err)
			}
			if err := resolvedRoot.WriteFile(resolvedName, []byte("written"), 0o600); err != nil {
				t.Errorf("private cache was not writable: %v", err)
			}
		})
	}
}
