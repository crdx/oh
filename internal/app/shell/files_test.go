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

func TestTemporaryMountIsWritableWithoutAShell(t *testing.T) {
	workspaceRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	temporaryDirectory := t.TempDir()
	temporaryRoot := file.NewLazy(temporaryDirectory, func(string) error { return nil })
	shell.MountScratch(files, temporaryDirectory, temporaryRoot, false)
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
	roots := shell.MountScratch(files, scratch, file.NewLazy(scratch, func(string) error { return nil }), true)
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

func TestGitAloneCannotWriteTheShellHomeMetadata(t *testing.T) {
	workspaceRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	home := t.TempDir()
	mode := caps.NewMode(caps.Read | caps.Git)
	homeRoot := file.NewLazy(home, shell.RefuseHomeWrite(mode))
	files.Mount(home, homeRoot)
	defer func() { _ = homeRoot.Close() }()
	if err := homeRoot.WriteFile(".git/config", []byte("write"), 0o600); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("home metadata write with g but not w: %v", err)
	}
}
