package shell

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

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

func TestDenySearchKeyDependsOnThePatternSetRatherThanItsOrder(t *testing.T) {
	left := denySearchKey([]string{"*.env", "secrets.yml"}, "/read")
	right := denySearchKey([]string{"secrets.yml", "*.env"}, "/read")
	if left != right {
		t.Error("equivalent patterns produced different cache keys")
	}
	if denySearchKey([]string{"*.env"}, "/other") == left {
		t.Error("a different root produced the same cache key")
	}
}

func TestAGrantSearchesOnlyTheRootItAdds(t *testing.T) {
	access, searched := denySearchRecorder(t)
	first := t.TempDir()
	second := t.TempDir()

	policy := sandbox.Policy{Deny: []string{"*.env"}, Read: []string{first}}
	if _, err := access.getDenyPaths(policy); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(searched(), first) {
		t.Fatalf("the first search covered %v, want %s among them", searched(), first)
	}

	policy.Read = []string{first, second}
	if _, err := access.getDenyPaths(policy); err != nil {
		t.Fatal(err)
	}
	if roots := searched(); !slices.Equal(roots, []string{second}) {
		t.Errorf("the search after the grant covered %v, want only %s", roots, second)
	}
}

func TestAPathIsGrantedWhileADenySearchIsStillWalking(t *testing.T) {
	mode := caps.NewMode(caps.All())
	access, err := NewPathAccess(configuredPathTestRoot(t, mode), mode, Paths{Deny: []string{"*.env"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(access.Close)

	hasStarted := make(chan struct{})
	release := make(chan struct{})
	finish := sync.OnceFunc(func() { close(release) })
	t.Cleanup(finish)
	hold := sync.OnceFunc(func() {
		close(hasStarted)
		<-release
	})
	access.searchDeny = func([]string, string) ([]string, error) {
		hold()
		return nil, nil
	}

	policy := sandbox.Policy{Deny: []string{"*.env"}, Read: []string{t.TempDir()}}
	searched := make(chan error, 1)
	go func() {
		_, err := access.getDenyPaths(policy)
		searched <- err
	}()
	<-hasStarted

	directory := t.TempDir()
	granted := make(chan error, 1)
	go func() {
		_, err := access.Grant(directory, ReadAccess)
		granted <- err
	}()

	select {
	case err := <-granted:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(denyGrantWait):
		t.Fatalf("the grant waited %s for the deny search", denyGrantWait)
	}

	finish()
	if err := <-searched; err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentPoliciesShareOneSearchOfTheSameRoot(t *testing.T) {
	mode := caps.NewMode(caps.All())
	access, err := NewPathAccess(configuredPathTestRoot(t, mode), mode, Paths{Deny: []string{"*.env"}})
	if err != nil {
		t.Fatal(err)
	}
	defer access.Close()

	var mutex sync.Mutex
	searches := make(map[string]int)
	access.searchDeny = func(_ []string, root string) ([]string, error) {
		mutex.Lock()
		searches[root]++
		mutex.Unlock()
		time.Sleep(denyOverlapWait)
		return nil, nil
	}

	root := t.TempDir()
	policy := sandbox.Policy{Deny: []string{"*.env"}, Read: []string{root}}

	var group sync.WaitGroup
	for range 4 {
		group.Go(func() {
			if _, err := access.getDenyPaths(policy); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()

	mutex.Lock()
	defer mutex.Unlock()
	if searches[root] != 1 {
		t.Errorf("%s was searched %d times, want once", root, searches[root])
	}
}

const (
	denyGrantWait   = 10 * time.Second
	denyOverlapWait = 50 * time.Millisecond
)

func denySearchRecorder(t *testing.T) (*PathAccess, func() []string) {
	t.Helper()

	mode := caps.NewMode(caps.All())
	access, err := NewPathAccess(configuredPathTestRoot(t, mode), mode, Paths{Deny: []string{"*.env"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(access.Close)

	var mutex sync.Mutex
	var roots []string
	access.searchDeny = func(_ []string, root string) ([]string, error) {
		mutex.Lock()
		defer mutex.Unlock()
		roots = append(roots, root)
		return nil, nil
	}

	return access, func() []string {
		mutex.Lock()
		defer mutex.Unlock()
		taken := slices.Clone(roots)
		roots = nil
		return taken
	}
}
