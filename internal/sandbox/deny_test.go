package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

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

func TestAGrantContainingTheScratchIsNotSearched(t *testing.T) {
	farm := t.TempDir()
	scratch := filepath.Join(farm, "session")
	if err := os.Mkdir(scratch, 0o700); err != nil {
		t.Fatal(err)
	}
	policy := Policy{Read: []string{farm}, TmpDir: scratch, Write: []string{TmpDir}}

	if slices.Contains(policy.denyRoots(), farm) {
		t.Errorf("scratch container %s remained a deny discovery root", farm)
	}
}
