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

func TestVerifyPathPassesWhileTheDirectoryStaysPut(t *testing.T) {
	isWritable := false
	root, directory := testRoot(t, &isWritable)
	if err := root.VerifyPath(); err != nil {
		t.Errorf("an unchanged directory was refused: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "note"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := root.VerifyPath(); err != nil {
		t.Errorf("content changes inside the directory were refused: %v", err)
	}
}

func TestVerifyPathRefusesAReplacedDirectory(t *testing.T) {
	isWritable := false
	root, directory := testRoot(t, &isWritable)
	moved := directory + "-moved"
	if err := os.Rename(directory, moved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(moved) })
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := root.VerifyPath(); !errors.Is(err, file.ErrMountChanged) {
		t.Errorf("a replaced directory got %v, want a mount-changed refusal", err)
	}
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

func TestAWritableFileWithMultipleHardLinksIsRefusedInEveryMode(t *testing.T) {
	isWritable := true
	root, directory := testRoot(t, &isWritable)
	isGitAllowed := false
	root.SetGitWriteAllowed(func() bool { return isGitAllowed })
	original := filepath.Join(directory, "original")
	alias := filepath.Join(directory, "alias")
	if err := os.WriteFile(original, []byte("intact"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(original, alias); err != nil {
		t.Fatal(err)
	}
	for _, isGitGranted := range []bool{false, true} {
		isGitAllowed = isGitGranted
		if err := root.WriteFile("alias", []byte("changed"), 0o600); !errors.Is(err, file.ErrMultipleHardLinks) {
			t.Errorf("writing a shared inode with g=%t: %v", isGitGranted, err)
		}
	}
	for _, name := range []string{"original", "alias"} {
		if content, err := root.ReadFile(name); err != nil || string(content) != "intact" {
			t.Errorf("%s changed to %q: %v", name, content, err)
		}
	}
	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile("alias", []byte("allowed"), 0o600); err != nil {
		t.Errorf("a file with one link remained blocked: %v", err)
	}
}

func TestAnExactFileRootRefusesMultipleHardLinksWithoutAMode(t *testing.T) {
	directory := t.TempDir()
	original := filepath.Join(directory, "original")
	alias := filepath.Join(directory, "alias")
	if err := os.WriteFile(original, []byte("intact"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(original, alias); err != nil {
		t.Fatal(err)
	}
	root := file.NewExactFile(alias, func(string) error { return nil })
	if err := root.WriteFile("alias", []byte("changed"), 0o600); !errors.Is(err, file.ErrMultipleHardLinks) {
		t.Errorf("exact root write got %v, want hard-link refusal", err)
	}
	if content, err := root.ReadFile("alias"); err != nil || string(content) != "intact" {
		t.Errorf("the exact root changed to %q: %v", content, err)
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

func TestAnImplicitReadMountDefersWritesToAnExplicitParent(t *testing.T) {
	isWritable := true
	root, workspace := testRoot(t, &isWritable)
	parent := filepath.Join(workspace, "granted")
	nested := filepath.Join(parent, "baseline")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	writeRoot := file.NewLazy(parent, func(string) error { return nil })
	readRoot := file.NewLazy(nested, func(string) error { return file.ErrReadOnly })
	root.Mount(parent, writeRoot)
	root.Mount(nested, readRoot)
	if err := root.RefuseWrite("granted/baseline/file"); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("an explicit nested read grant got %v, want read-only", err)
	}
	readRoot.DeferWrites()
	for name, check := range map[string]func() error{
		"enclosing root": func() error { return root.RefuseWrite("granted/baseline/file") },
		"mounted root":   func() error { return readRoot.RefuseWrite("file") },
	} {
		if err := check(); err != nil {
			t.Errorf("%s did not inherit the explicit parent write: %v", name, err)
		}
	}
}

func TestEqualRealMountsKeepAHardRefusalRegardlessOfOrder(t *testing.T) {
	isWritable := true
	root, workspace := testRoot(t, &isWritable)
	target := filepath.Join(workspace, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(workspace, "alias")
	if err := os.Symlink("target", alias); err != nil {
		t.Fatal(err)
	}
	root.Mount(target, file.NewLazy(target, func(string) error { return nil }))
	root.Mount(alias, file.NewLazy(alias, func(string) error { return file.ErrGitDir }))
	for range 100 {
		if err := root.RefuseWrite("target/note"); !errors.Is(err, file.ErrGitDir) {
			t.Fatalf("a writable alias bypassed a hard refusal: %v", err)
		}
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

func TestAnExactFileRootRefusesASymlinkSwappedOntoItsFile(t *testing.T) {
	directory := t.TempDir()
	exactPath := filepath.Join(directory, "exact")
	siblingPath := filepath.Join(directory, "sibling")
	const canary = "canary-secret-4d7c11"
	if err := os.WriteFile(exactPath, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(siblingPath, []byte(canary), 0o600); err != nil {
		t.Fatal(err)
	}
	root := file.NewExactFile(exactPath, func(string) error { return nil })

	if err := os.Remove(exactPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("sibling", exactPath); err != nil {
		t.Fatal(err)
	}

	if data, err := root.ReadFile("exact"); !errors.Is(err, file.ErrOutsideRoot) {
		t.Errorf("read got %q and %v, want an outside-the-root refusal", data, err)
	}
	if _, err := root.Open("exact"); !errors.Is(err, file.ErrOutsideRoot) {
		t.Errorf("open got %v, want an outside-the-root refusal", err)
	}
	if info, err := root.Stat("exact"); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("stat got %v and %v, want the symlink itself", info, err)
	}
	if err := root.WriteFile("exact", []byte("tampered"), 0o600); !errors.Is(err, file.ErrOutsideRoot) {
		t.Errorf("write got %v, want an outside-the-root refusal", err)
	}
	after, err := os.ReadFile(siblingPath) //nolint:gosec // a path this test made itself
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != canary {
		t.Errorf("sibling changed to %q", after)
	}
}

func TestALazyRootOpensItsDirectoryOnlyWhenFirstUsed(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "later")

	root := file.NewLazy(directory, func(string) error { return nil })
	t.Cleanup(func() { _ = root.Close() })
	if root.Name() != directory {
		t.Errorf("got root name %q, want %q", root.Name(), directory)
	}

	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "note"), []byte("note"), 0o600); err != nil {
		t.Fatal(err)
	}

	if data, err := root.ReadFile("note"); err != nil || string(data) != "note" {
		t.Errorf("read got %q and %v", data, err)
	}
	if info, err := root.Stat("note"); err != nil || info.Name() != "note" {
		t.Errorf("stat got %v and %v", info, err)
	}
	if entries, err := root.ReadDir("."); err != nil || len(entries) != 1 {
		t.Errorf("listing got %v and %v", entries, err)
	}
	if err := root.WriteFile("written", []byte("written"), 0o600); err != nil {
		t.Errorf("write got %v", err)
	}
	if err := root.MkdirAll("nested", 0o700); err != nil {
		t.Errorf("mkdir got %v", err)
	}
}

func TestALazyRootPinsItsGrantedDirectoryIdentityOnFirstUse(t *testing.T) {
	parent := t.TempDir()
	granted := filepath.Join(parent, "granted")
	private := filepath.Join(parent, "private")
	for _, directory := range []string{granted, private} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	initialInfo, err := os.Stat(granted)
	if err != nil {
		t.Fatal(err)
	}
	root := file.NewLazyWithIdentity(granted, initialInfo, func(string) error { return nil })
	t.Cleanup(func() { _ = root.Close() })
	if err := os.Rename(granted, granted+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(private, granted); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := root.ReadFile("note"); !errors.Is(err, file.ErrMountChanged) {
			t.Errorf("changed root opened with %v, want persistent refusal", err)
		}
	}
}

func TestAnUnchangedLazyRootReadsNewContent(t *testing.T) {
	directory := t.TempDir()
	initialInfo, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	root := file.NewLazyWithIdentity(directory, initialInfo, func(string) error { return nil })
	t.Cleanup(func() { _ = root.Close() })
	if err := os.WriteFile(filepath.Join(directory, "later"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if content, err := root.ReadFile("later"); err != nil || string(content) != "new" {
		t.Errorf("unchanged mounted directory read %q and %v", content, err)
	}
}

func TestALazyRootWhoseDirectoryIsMissingFailsOnlyWhenUsed(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "missing")
	root := file.NewLazy(directory, func(string) error { return nil })
	t.Cleanup(func() { _ = root.Close() })

	for operation, run := range map[string]func() error{
		"open":  func() error { _, err := root.Open("note"); return err },
		"read":  func() error { _, err := root.ReadFile("note"); return err },
		"stat":  func() error { _, err := root.Stat("note"); return err },
		"write": func() error { return root.WriteFile("note", nil, 0o600) },
		"mkdir": func() error { return root.MkdirAll("nested", 0o700) },
	} {
		if err := run(); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s got %v, want the directory reported missing", operation, err)
		}
	}
}

func TestALazyRootClosedBeforeUseOpensNothing(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "note"), []byte("note"), 0o600); err != nil {
		t.Fatal(err)
	}

	unused := file.NewLazy(directory, func(string) error { return nil })
	if err := unused.Close(); err != nil {
		t.Fatalf("closing an unused root got %v", err)
	}
	if _, err := unused.ReadFile("note"); !errors.Is(err, os.ErrClosed) {
		t.Errorf("read after close got %v, want closed", err)
	}

	used := file.NewLazy(directory, func(string) error { return nil })
	if _, err := used.ReadFile("note"); err != nil {
		t.Fatal(err)
	}
	if err := used.Close(); err != nil {
		t.Fatalf("closing a used root got %v", err)
	}
	if _, err := used.ReadFile("note"); err == nil {
		t.Error("expected a read after close to fail")
	}
}

func TestOnlyALazyRootIsClosedThroughTheTree(t *testing.T) {
	isWritable := true
	root, directory := testRoot(t, &isWritable)
	if err := root.Close(); err != nil {
		t.Fatalf("closing a borrowed root got %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "note"), []byte("note"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := root.ReadFile("note"); err != nil {
		t.Errorf("expected the borrowed root to stay open, got %v", err)
	}
}
