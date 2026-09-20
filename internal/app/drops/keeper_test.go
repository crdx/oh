package drops

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"crdx.org/oh/internal/file"
)

func newFiles(t *testing.T) *file.Root {
	t.Helper()

	workspaceRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspaceRoot.Close() })

	return file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
}

func openKeeper(t *testing.T) (*Keeper, string) {
	t.Helper()

	sessionDirectory := filepath.Join(t.TempDir(), "tame-impala")
	keeper, err := Open(newFiles(t), sessionDirectory, func() error {
		return os.MkdirAll(sessionDirectory, 0o700)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = keeper.Close() })

	return keeper, sessionDirectory
}

func isReadable(t *testing.T, keeper *Keeper, path string) bool {
	t.Helper()

	root, name, err := keeper.files.Resolve(path)
	if err != nil {
		return false
	}
	contents, err := root.ReadFile(name)

	return err == nil && len(contents) > 0
}

func TestAKeeperOpensWithoutADropsDirectory(t *testing.T) {
	keeper, sessionDirectory := openKeeper(t)

	if _, err := os.Stat(GetDirectory(sessionDirectory)); !os.IsNotExist(err) {
		t.Error("opening a keeper made a drops directory before anything was dropped")
	}
	if keeper.GetDirectory() != GetDirectory(sessionDirectory) {
		t.Errorf("the keeper names %q", keeper.GetDirectory())
	}
}

func TestWhatAKeeperSavesCanBeReadBack(t *testing.T) {
	for name, save := range map[string]func(*Keeper) (string, error){
		"HTML": func(keeper *Keeper) (string, error) {
			return keeper.SaveHTML([]byte("<!DOCTYPE html><title>page</title>"))
		},
		"image": func(keeper *Keeper) (string, error) {
			return keeper.SaveImage("image/png", []byte("\x89PNG\r\n"))
		},
		"output": func(keeper *Keeper) (string, error) {
			return keeper.SaveOutput("the whole of it\n")
		},
	} {
		t.Run(name, func(t *testing.T) {
			keeper, _ := openKeeper(t)

			path, err := save(keeper)
			if err != nil {
				t.Fatal(err)
			}

			if !isReadable(t, keeper, path) {
				t.Errorf("%s was saved to %q, which nothing can read", name, path)
			}
		})
	}
}

func TestAKeeperMountsTheDropsItMadeOnlyOnce(t *testing.T) {
	keeper, _ := openKeeper(t)

	first, err := keeper.SaveOutput("one\n")
	if err != nil {
		t.Fatal(err)
	}
	second, err := keeper.SaveOutput("another\n")
	if err != nil {
		t.Fatal(err)
	}

	if !isReadable(t, keeper, first) || !isReadable(t, keeper, second) {
		t.Error("a later drop lost the mount the first one made")
	}
}

func TestAKeeperCanSaveConcurrentHTML(t *testing.T) {
	keeper, _ := openKeeper(t)

	const calls = 8
	errors := make(chan error, calls)
	var group sync.WaitGroup
	for range calls {
		group.Go(func() {
			_, err := keeper.SaveHTML([]byte("<!DOCTYPE html><title>page</title>"))
			errors <- err
		})
	}
	group.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Error(err)
		}
	}
}

func TestACopiedFileIsReadableThroughTheKeeper(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "chat.md")
	if err := os.WriteFile(sourcePath, []byte("conversation"), 0o600); err != nil {
		t.Fatal(err)
	}

	keeper, _ := openKeeper(t)

	path, err := keeper.CopyFile(sourcePath, "source.md")
	if err != nil {
		t.Fatal(err)
	}

	if !isReadable(t, keeper, path) {
		t.Errorf("the copy at %q cannot be read", path)
	}
}
