package file_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/file"
)

func testRoot(t *testing.T, writable *bool) (*file.Root, string) {
	t.Helper()

	directory := t.TempDir()

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = root.Close() })

	return file.New(root, func(string) error {
		if *writable {
			return nil
		}

		return file.ErrReadOnly
	}), directory
}

func TestWritingGoesThroughWhileTheTreeMayBeChanged(t *testing.T) {
	isWritable := true
	root, directory := testRoot(t, &isWritable)

	if err := root.WriteFile("made.txt", []byte("content"), 0o644); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(directory, "made.txt")) //nolint:gosec // a path this test made itself
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(data) != "content" {
		t.Errorf("got %q, want %q", data, "content")
	}
}

func TestWritingIsRefusedWhileTheTreeIsReadOnly(t *testing.T) {
	isWritable := false
	root, directory := testRoot(t, &isWritable)

	if err := root.WriteFile("made.txt", []byte("content"), 0o644); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("expected the write to be refused, got %v", err)
	}

	if _, err := os.Stat(filepath.Join(directory, "made.txt")); err == nil {
		t.Error("expected no file to have been made")
	}
}

func TestMakingDirectoriesIsRefusedWhileTheTreeIsReadOnly(t *testing.T) {
	isWritable := false
	root, directory := testRoot(t, &isWritable)

	if err := root.MkdirAll("one/two", 0o755); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("expected the directory to be refused, got %v", err)
	}

	if _, err := os.Stat(filepath.Join(directory, "one")); err == nil {
		t.Error("expected no directory to have been made")
	}
}

func TestWhetherTheTreeMayBeChangedIsAskedForEveryCall(t *testing.T) {
	isWritable := true
	root, _ := testRoot(t, &isWritable)

	if err := root.WriteFile("one.txt", nil, 0o644); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	isWritable = false

	if err := root.WriteFile("two.txt", nil, 0o644); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("expected the write to be refused, got %v", err)
	}

	isWritable = true

	if err := root.WriteFile("three.txt", nil, 0o644); err != nil {
		t.Errorf("expected the write to be allowed again, got %v", err)
	}
}

func TestReadingGoesThroughWhileTheTreeIsReadOnly(t *testing.T) {
	isWritable := false
	root, directory := testRoot(t, &isWritable)

	if err := os.WriteFile(filepath.Join(directory, "there.txt"), []byte("content"), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if data, err := root.ReadFile("there.txt"); err != nil || string(data) != "content" {
		t.Errorf("got %q and %v", data, err)
	}

	if _, err := root.Stat("there.txt"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	openRoot, err := root.Open("there.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_ = openRoot.Close()

	if data, err := fs.ReadFile(root.FS(), "there.txt"); err != nil || string(data) != "content" {
		t.Errorf("got %q and %v", data, err)
	}
}

func TestTheTreeSaysWhetherItMayBeChanged(t *testing.T) {
	isWritable := true
	root, directory := testRoot(t, &isWritable)

	if err := root.RefuseWrite("."); err != nil {
		t.Errorf("expected a writable tree to stand in nothing's way, got %v", err)
	}

	isWritable = false

	if err := root.RefuseWrite("."); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("expected a read-only tree to say so, got %v", err)
	}

	if root.Name() != directory {
		t.Errorf("got %q, want %q", root.Name(), directory)
	}
}

func openRoot(t *testing.T) (*os.Root, string) {
	t.Helper()

	directory := t.TempDir()

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = root.Close() })

	return root, directory
}

func TestARefusalNamesThePathItWasAskedAbout(t *testing.T) {
	root, directory := openRoot(t)

	refusal := errors.New("not that one")

	guardedRoot := file.New(root, func(name string) error {
		if strings.Contains(name, "secret") {
			return refusal
		}

		return nil
	})

	if err := guardedRoot.WriteFile("secret.txt", []byte("x"), 0o644); !errors.Is(err, refusal) {
		t.Errorf("expected the write to be refused, got %v", err)
	}

	if err := guardedRoot.WriteFile("plain.txt", []byte("x"), 0o644); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(directory, "plain.txt")) //nolint:gosec // a path this test made itself
	if err != nil || string(data) != "x" {
		t.Errorf("got %q and %v", data, err)
	}
}

func TestARefusalAppliesAfterFollowingSymlinks(t *testing.T) {
	root, directory := openRoot(t)

	metadata := filepath.Join(directory, ".git")
	if err := os.Mkdir(metadata, 0o700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(metadata, "config")
	if err := os.WriteFile(config, []byte("intact"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(".git", "config"), filepath.Join(directory, "alias")); err != nil {
		t.Fatal(err)
	}

	guardedRoot := file.New(root, file.RefuseGitDir)
	if err := guardedRoot.WriteFile("alias", []byte("changed"), 0o600); !errors.Is(err, file.ErrGitDir) {
		t.Errorf("expected the symlink destination to be refused, got %v", err)
	}

	data, err := os.ReadFile(config) //nolint:gosec // a path this test made itself
	if err != nil || string(data) != "intact" {
		t.Errorf("got %q and %v", data, err)
	}
}

func TestMakingADirectoryIsAskedAboutItsOwnName(t *testing.T) {
	root, directory := openRoot(t)

	guardedRoot := file.New(root, file.RefuseGitDir)

	if err := guardedRoot.MkdirAll(filepath.Join(".git", "hooks"), 0o755); !errors.Is(err, file.ErrGitDir) {
		t.Errorf("expected the directory to be refused, got %v", err)
	}

	if _, err := os.Stat(filepath.Join(directory, ".git")); err == nil {
		t.Error("expected no directory to have been made")
	}
}

func TestRefuseGitDir(t *testing.T) {
	for _, name := range []string{".git", ".git/config", "a/.git/config", "./.git/HEAD"} {
		if err := file.RefuseGitDir(name); !errors.Is(err, file.ErrGitDir) {
			t.Errorf("expected %q to be refused, got %v", name, err)
		}
	}

	for _, name := range []string{".", "plain.txt", "a.git/config", "git/config", "a/.gitignore"} {
		if err := file.RefuseGitDir(name); err != nil {
			t.Errorf("expected %q to be allowed, got %v", name, err)
		}
	}
}

func TestTheMostSpecificMountResolvesANestedPath(t *testing.T) {
	isWritable := false
	root, _ := testRoot(t, &isWritable)
	parent, parentPath := testRoot(t, &isWritable)
	child, _ := testRoot(t, &isWritable)

	root.Mount(parentPath, parent)
	root.Mount(filepath.Join(parentPath, "nested"), child)

	resolved, name, err := root.Resolve(filepath.Join(parentPath, "nested", "file"))
	if err != nil {
		t.Fatal(err)
	}
	if resolved != child || name != "file" {
		t.Errorf("got root %p and name %q, want child root %p and name file", resolved, name, child)
	}
}

func TestAnUnmountedPathNoLongerResolves(t *testing.T) {
	isWritable := false
	root, _ := testRoot(t, &isWritable)
	mounted, mountedPath := testRoot(t, &isWritable)

	root.Mount(mountedPath, mounted)
	if _, _, err := root.Resolve(mountedPath); err != nil {
		t.Fatal(err)
	}
	root.Unmount(mountedPath)
	if _, _, err := root.Resolve(mountedPath); !errors.Is(err, file.ErrOutsideRoot) {
		t.Errorf("unmounted path resolved with %v", err)
	}
}

func TestAnExactFileMountResolvesOnlyTheNamedFile(t *testing.T) {
	isWritable := false
	root, _ := testRoot(t, &isWritable)
	mounted, mountedPath := testRoot(t, &isWritable)
	path := filepath.Join(mountedPath, "shared")

	root.MountFile(path, mounted, "shared")

	resolved, name, err := root.Resolve(path)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != mounted || name != "shared" {
		t.Errorf("got root %p and name %q, want mounted root %p and name shared", resolved, name, mounted)
	}

	for _, outside := range []string{
		filepath.Join(mountedPath, "sibling"),
		filepath.Join(path, "descendant"),
	} {
		if _, _, err := root.Resolve(outside); !errors.Is(err, file.ErrOutsideRoot) {
			t.Errorf("%s resolved with %v, want outside-root refusal", outside, err)
		}
	}
}

func TestAnExactFileRootReadsOnlyItsFile(t *testing.T) {
	directory := t.TempDir()
	exactPath := filepath.Join(directory, "exact")
	siblingPath := filepath.Join(directory, "sibling")
	if err := os.WriteFile(exactPath, []byte("exact"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(siblingPath, []byte("sibling"), 0o600); err != nil {
		t.Fatal(err)
	}

	root := file.NewExactFile(exactPath, func(string) error { return file.ErrReadOnly })
	if root.Name() != directory {
		t.Errorf("got root name %q, want %q", root.Name(), directory)
	}
	if data, err := root.ReadFile("exact"); err != nil || string(data) != "exact" {
		t.Errorf("read got %q and %v", data, err)
	}
	opened, err := root.Open("exact")
	if err != nil {
		t.Fatal(err)
	}
	_ = opened.Close()
	if info, err := root.Stat("exact"); err != nil || info.Name() != "exact" {
		t.Errorf("stat got %v and %v", info, err)
	}
	openedFS, err := root.FS().Open("exact")
	if err != nil {
		t.Fatal(err)
	}
	_ = openedFS.Close()
	if data, err := fs.ReadFile(root.FS(), "exact"); err != nil || string(data) != "exact" {
		t.Errorf("filesystem read got %q and %v", data, err)
	}

	for operation, run := range map[string]func() error{
		"open":       func() error { _, err := root.Open("sibling"); return err },
		"read":       func() error { _, err := root.ReadFile("sibling"); return err },
		"stat":       func() error { _, err := root.Stat("sibling"); return err },
		"filesystem": func() error { _, err := root.FS().Open("sibling"); return err },
	} {
		if err := run(); !errors.Is(err, file.ErrOutsideRoot) {
			t.Errorf("%s sibling got %v, want outside-root refusal", operation, err)
		}
	}
	if err := root.WriteFile("exact", []byte("changed"), 0o600); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("write got %v, want read-only", err)
	}
	if err := root.MkdirAll("directory", 0o700); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("mkdir got %v, want read-only", err)
	}
}

func TestAWritableExactFileRootStillRefusesSiblingsAndDirectories(t *testing.T) {
	directory := t.TempDir()
	exactPath := filepath.Join(directory, "exact")
	if err := os.WriteFile(exactPath, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := file.NewExactFile(exactPath, func(string) error { return nil })

	if err := root.WriteFile("exact", []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err := root.ReadFile("exact"); err != nil || string(data) != "after" {
		t.Errorf("written file got %q and %v", data, err)
	}
	if err := root.WriteFile("sibling", []byte("blocked"), 0o600); !errors.Is(err, file.ErrOutsideRoot) {
		t.Errorf("sibling write got %v, want outside-root refusal", err)
	}
	if err := root.MkdirAll("exact", 0o700); !errors.Is(err, file.ErrOutsideRoot) {
		t.Errorf("mkdir got %v, want outside-root refusal", err)
	}
}
