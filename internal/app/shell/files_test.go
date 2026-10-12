package shell_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/sandbox"
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

	homeRoot := shell.MountHomeDirectory(files, home, caps.NewMode(caps.Read))
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
	temporaryRoot := shell.MountTemporaryDirectory(files, temporaryDirectory)
	defer func() { _ = temporaryRoot.Close() }()

	resolvedRoot, name, err := files.Resolve("/tmp/proof")
	if err != nil {
		t.Fatal(err)
	}
	if err := resolvedRoot.WriteFile(name, []byte("written"), 0o600); err != nil {
		t.Fatalf("tmp was not writable: %v", err)
	}
}

func TestAnUnconfinedSessionReadsHostTmpRatherThanItsScratch(t *testing.T) {
	openedRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = openedRoot.Close() }()
	files := file.New(openedRoot, func(string) error { return file.ErrReadOnly })
	hostDirectory, err := os.MkdirTemp(sandbox.TmpDir, "oh-yolo-host-") //nolint:usetesting // this fixture must be in host /tmp
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(hostDirectory) })
	hostFile := filepath.Join(hostDirectory, "note")
	if err := os.WriteFile(hostFile, []byte("host"), 0o600); err != nil {
		t.Fatal(err)
	}
	scratch := t.TempDir()
	scratchFile := filepath.Join(scratch, filepath.Base(hostDirectory), "note")
	if err := os.MkdirAll(filepath.Dir(scratchFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scratchFile, []byte("scratch"), 0o600); err != nil {
		t.Fatal(err)
	}
	roots := shell.MountHostTemporaryDirectory(files, scratch)
	defer func() {
		for _, root := range roots {
			_ = root.Close()
		}
	}()

	for path, wanted := range map[string]string{hostFile: "host", scratchFile: "scratch"} {
		resolvedRoot, name, err := files.Resolve(path)
		if err != nil {
			t.Fatal(err)
		}
		if content, err := resolvedRoot.ReadFile(name); err != nil || string(content) != wanted {
			t.Errorf("%s read %q and %v, want %q", path, content, err, wanted)
		}
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
			homeRoot := shell.MountHomeDirectory(files, home, mode)
			defer func() { _ = homeRoot.Close() }()
			cacheRoot := shell.MountHomeCache(files, home)
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

func TestGitAloneCannotWriteTheShellHomeMetadata(t *testing.T) {
	workspaceRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	home := t.TempDir()
	mode := caps.NewMode(caps.Read | caps.Git)
	homeRoot := shell.MountHomeDirectory(files, home, mode)
	defer func() { _ = homeRoot.Close() }()
	if err := homeRoot.WriteFile(".git/config", []byte("write"), 0o600); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("home metadata write with g but not w: %v", err)
	}
}
