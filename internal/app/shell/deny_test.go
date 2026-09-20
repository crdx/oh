package shell

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/sandbox"
)

func TestDenyGlobsMatchNamesAtEveryDepth(t *testing.T) {
	root := t.TempDir()
	denials, err := newPathDenials([]string{"*.env"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		filepath.Join(root, ".env"),
		filepath.Join(root, "nested", "deeper", "local.env"),
	} {
		if err := denials.Refuse(name); !errors.Is(err, ErrDenied) {
			t.Errorf("%s got %v, want a denial", name, err)
		}
	}
}

func TestADeniedDirectoryCoversItsContents(t *testing.T) {
	root := t.TempDir()
	private := filepath.Join(root, "private")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}

	denials, err := newPathDenials([]string{"private"})
	if err != nil {
		t.Fatal(err)
	}
	if err := denials.Refuse(filepath.Join(private, "secret")); !errors.Is(err, ErrDenied) {
		t.Errorf("got %v, want a denial", err)
	}
	if err := denials.Refuse(filepath.Join(root, "public")); err != nil {
		t.Errorf("public path was refused: %v", err)
	}
}

func TestDenyPatternsMustBeNamesAndValid(t *testing.T) {
	for _, pattern := range []string{"", "directory/name", "broken["} {
		if _, err := newPathDenials([]string{pattern}); err == nil {
			t.Errorf("pattern %q was accepted", pattern)
		}
	}
}

func TestPathToolsCannotReadDeniedPathsOrGrantThem(t *testing.T) {
	workspace := t.TempDir()
	secret := filepath.Join(workspace, ".env")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(workspace, "alias")); err != nil {
		t.Fatal(err)
	}
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()

	files := file.New(workspaceRoot, func(string) error { return nil })
	access, err := NewPathAccess(files, caps.NewMode(caps.All()), Paths{
		Deny: []string{"*.env"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	if _, err := files.ReadFile(".env"); !errors.Is(err, ErrDenied) {
		t.Errorf("read got %v, want a denial", err)
	}
	if _, err := files.FS().Open(".env"); !errors.Is(err, ErrDenied) {
		t.Errorf("filesystem open got %v, want a denial", err)
	}
	if _, err := files.ReadFile("alias"); !errors.Is(err, ErrDenied) {
		t.Errorf("symlink read got %v, want a denial", err)
	}
	if _, err := files.Stat(".env"); !errors.Is(err, ErrDenied) {
		t.Errorf("stat got %v, want a denial", err)
	}
	if err := files.WriteFile(".env", []byte("changed"), 0o600); !errors.Is(err, ErrDenied) {
		t.Errorf("write got %v, want a denial", err)
	}
	entries, err := files.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == ".env" || entry.Name() == "alias" {
			t.Errorf("listing disclosed denied entry %s", entry.Name())
		}
	}
	if changed, err := access.Grant(secret, ReadAccess); changed || !errors.Is(err, ErrDenied) {
		t.Errorf("grant changed=%t, err=%v, want a denial", changed, err)
	}
}

func TestDenyCacheKeyDependsOnReachablePathsRatherThanTheirRights(t *testing.T) {
	left := sandbox.Policy{
		Deny:  []string{"*.env", "secrets.yml"},
		Read:  []string{"/read", "/shared"},
		Write: []string{"/write"},
	}
	right := sandbox.Policy{
		Deny:  []string{"secrets.yml", "*.env"},
		Read:  []string{"/write"},
		Write: []string{"/shared", "/read"},
	}
	if denyPolicyKey(left) != denyPolicyKey(right) {
		t.Error("equivalent reachable paths produced different cache keys")
	}
}
