package sandbox

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestAnAbsoluteDenyPathDoesNotWalkTheWorkspace(t *testing.T) {
	workspace := t.TempDir()
	secret := filepath.Join(workspace, "auth.json")
	if err := os.WriteFile(secret, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	searches := 0
	policy := Policy{Deny: []string{secret}, Read: []string{workspace}}
	paths, err := policy.DiscoverDenyPathsWith(func([]string, string, string) ([]string, error) {
		searches++
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if searches != 0 || !slices.Equal(paths, []string{secret}) {
		t.Errorf("searched %d roots and denied %v, want no walk and %s", searches, paths, secret)
	}
}

func TestAnAbsoluteDeniedPathOutsideEveryGrantNeedsNoShellMount(t *testing.T) {
	workspace := t.TempDir()
	elsewhere := t.TempDir()
	secret := filepath.Join(elsewhere, "auth.json")
	if err := os.WriteFile(secret, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy := Policy{Deny: []string{secret}, Read: []string{workspace}}
	paths, err := policy.DiscoverDenyPaths()
	if err != nil || len(paths) != 0 {
		t.Errorf("found denied mounts %v with error %v for an unreachable file", paths, err)
	}
}

func TestAnAbsoluteScratchDenialUsesTheVirtualTmpPath(t *testing.T) {
	scratch := t.TempDir()
	secret := filepath.Join(scratch, "auth.json")
	if err := os.WriteFile(secret, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy := Policy{Deny: []string{secret}, TmpDir: scratch, Write: []string{TmpDir}}
	paths, err := policy.DiscoverDenyPaths()
	if err != nil || !slices.Equal(paths, []string{filepath.Join(TmpDir, "auth.json")}) {
		t.Errorf("found denied mounts %v with error %v, want the virtual scratch path", paths, err)
	}
}

func TestAnAbsentAbsoluteDenyPathFailsClosed(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "auth.json")
	policy := Policy{Deny: []string{secret}}
	if _, err := policy.DiscoverDenyPaths(); err == nil {
		t.Error("a missing absolute deny path was silently ignored")
	}
}

func TestAnUnreadableSubtreeIsDeniedAsAWhole(t *testing.T) {
	root := t.TempDir()
	hidden := filepath.Join(root, "unlistable")
	if err := os.Mkdir(hidden, 0o700); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := findDenyPathsWithWalk([]string{"auth.json"}, root, "", func(_ string, visit fs.WalkDirFunc) error {
		visitError := visit(hidden, entries[0], &fs.PathError{Op: "readdir", Path: hidden, Err: fs.ErrPermission})
		if errors.Is(visitError, filepath.SkipDir) {
			return nil
		}
		return visitError
	})
	if err != nil || !slices.Equal(paths, []string{hidden}) {
		t.Errorf("denied paths %v and error %v, want the whole unreadable directory", paths, err)
	}
}

func TestDenyNamesAreFoundAtEveryDepthOfGrantedTrees(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "foo.txt")
	second := filepath.Join(root, "nested", "deeper", "foo.txt")
	for _, name := range []string{first, second} {
		if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte("secret"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	paths, err := FindDenyPaths([]string{"foo.*"}, root)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(paths, []string{first, second}) {
		t.Errorf("got %v, want %v", paths, []string{first, second})
	}
}

func TestAMatchedDirectoryDeniesItsWholeTree(t *testing.T) {
	root := t.TempDir()
	private := filepath.Join(root, "private")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(private, "foo.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	paths, err := FindDenyPaths([]string{"private", "foo.txt"}, root)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(paths, []string{private}) {
		t.Errorf("got %v, want only %s", paths, private)
	}
}

func TestAPathDeniedThroughASymlinkResolvesToTheSameContent(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "foo.txt")); err != nil {
		t.Fatal(err)
	}

	paths, err := FindDenyPaths([]string{"foo.txt"}, root)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(paths, []string{target}) {
		t.Errorf("got %v, want %v", paths, []string{target})
	}
}

func TestScratchDenyPathsAreTranslatedToSandboxTmp(t *testing.T) {
	scratch := t.TempDir()
	if err := os.WriteFile(filepath.Join(scratch, ".env"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy := Policy{Deny: []string{".env"}, TmpDir: scratch, Write: []string{TmpDir}}

	paths, err := policy.DiscoverDenyPaths()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(paths, filepath.Join(TmpDir, ".env")) {
		t.Errorf("got %v, want the scratch match translated beneath %s", paths, TmpDir)
	}
}

func TestPrivateSharedMemoryIsNotSearchedForDeniedNames(t *testing.T) {
	if slices.Contains(Policy{Deny: []string{".env"}}.denyRoots(), SharedMemoryPath) {
		t.Errorf("the host's %s was searched, though a command only ever sees an empty one of its own", SharedMemoryPath)
	}
}

func TestThePrivateProcessFilesystemIsNotSearchedOnTheHost(t *testing.T) {
	if slices.Contains(Policy{Deny: []string{"auth.json"}}.denyRoots(), processFilesystemPath) {
		t.Error("the host's changing /proc tree was searched instead of the sandbox's private one")
	}
}

func TestAGrantContainingScratchStillFindsDeniedNamesBesideIt(t *testing.T) {
	farm := t.TempDir()
	scratch := filepath.Join(farm, "session")
	if err := os.Mkdir(scratch, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{filepath.Join(farm, "auth.json"), filepath.Join(scratch, "auth.json")} {
		if err := os.WriteFile(name, []byte("private"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	policy := Policy{Deny: []string{"auth.json"}, Read: []string{farm}, TmpDir: scratch, Write: []string{TmpDir}}

	var skips []string
	paths, err := policy.DiscoverDenyPathsWith(func(patterns []string, root string, skip string) ([]string, error) {
		skips = append(skips, root+":"+skip)
		return FindDenyPathsSkipping(patterns, root, skip)
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(TmpDir, "auth.json"), filepath.Join(farm, "auth.json"), filepath.Join(scratch, "auth.json")}
	for _, name := range want {
		if !slices.Contains(paths, name) {
			t.Errorf("denied paths %q miss %s", paths, name)
		}
	}
	if !slices.Contains(skips, farm+":"+scratch) {
		t.Errorf("searches %q scanned scratch as part of its parent", skips)
	}
}
