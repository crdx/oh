package drops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"crdx.org/io/internal/file"
)

func TestDropsUseADirectoryInsideTheSession(t *testing.T) {
	sessionDirectory := filepath.Join("state", "sessions", "tame-impala")
	want := filepath.Join(sessionDirectory, "drops")
	if got := GetDirectory(sessionDirectory); got != want {
		t.Errorf("drops directory is %q, want %q", got, want)
	}
}

func TestOnlyADirectoryCanBeDrops(t *testing.T) {
	for name, prepareInvalidPath := range map[string]func(string) error{
		"file": func(path string) error {
			return os.WriteFile(path, []byte("not a directory"), 0o600)
		},
		"symbolic link": func(path string) error {
			return os.Symlink(t.TempDir(), path)
		},
	} {
		t.Run(name, func(t *testing.T) {
			sessionDirectory := t.TempDir()
			if err := prepareInvalidPath(GetDirectory(sessionDirectory)); err != nil {
				t.Fatal(err)
			}

			if _, err := Prepare(sessionDirectory, func() error { return nil }); err == nil {
				t.Error("invalid drops path was prepared")
			}

			workspaceRoot, err := os.OpenRoot(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = workspaceRoot.Close() }()
			files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
			if _, _, err := Mount(files, sessionDirectory); err == nil {
				t.Error("invalid drops path was mounted")
			}
		})
	}
}

func TestAFileIsCopiedPrivatelyIntoDrops(t *testing.T) {
	sourceDirectory := t.TempDir()
	sourcePath := filepath.Join(sourceDirectory, "chat.md")
	if err := os.WriteFile(sourcePath, []byte("conversation"), 0o600); err != nil {
		t.Fatal(err)
	}

	sessionDirectory := filepath.Join(t.TempDir(), "tame-impala")
	path, err := CopyFile(sessionDirectory, func() error {
		return os.Mkdir(sessionDirectory, 0o700)
	}, sourcePath, "oaken-elephant.chat.md")
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(sessionDirectory, "drops", "oaken-elephant.chat.md")
	if path != wantPath {
		t.Errorf("copied path is %q, want %q", path, wantPath)
	}

	dropsRoot, err := os.OpenRoot(GetDirectory(sessionDirectory))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dropsRoot.Close() }()
	content, err := dropsRoot.ReadFile("oaken-elephant.chat.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "conversation" {
		t.Errorf("copied chat reads %q", content)
	}
	fileInfo, err := dropsRoot.Stat("oaken-elephant.chat.md")
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Errorf("copied chat mode is %o, want 600", fileInfo.Mode().Perm())
	}
}

func TestADropFileNameCannotLeaveDrops(t *testing.T) {
	wasEnsured := false
	_, err := CopyFile(t.TempDir(), func() error {
		wasEnsured = true
		return nil
	}, filepath.Join(t.TempDir(), "chat.md"), "../chat.md")
	if err == nil {
		t.Fatal("escaping drop file name was accepted")
	}
	if wasEnsured {
		t.Error("escaping drop file name persisted the session")
	}
}

func TestExistingDropsAreMountedReadOnly(t *testing.T) {
	sessionDirectory := t.TempDir()
	directory := GetDirectory(sessionDirectory)
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "image.png"), []byte("image bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	workspaceRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()
	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	closeDrops, areDropsMounted, err := Mount(files, sessionDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = closeDrops() }()
	if !areDropsMounted {
		t.Fatal("drops directory was not mounted")
	}

	path := filepath.Join(directory, "image.png")
	mountedRoot, name, err := files.Resolve(path)
	if err != nil {
		t.Fatal(err)
	}
	content, err := mountedRoot.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "image bytes" {
		t.Errorf("mounted drop reads %q", content)
	}
	if err := mountedRoot.RefuseWrite(name); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("mounted drop write got %v, want read-only", err)
	}
}

func TestAbsentDropsHaveNothingToMount(t *testing.T) {
	workspaceRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspaceRoot.Close() }()
	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })

	closeDrops, areDropsMounted, err := Mount(files, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := closeDrops(); err != nil {
		t.Fatal(err)
	}
	if areDropsMounted {
		t.Error("absent drops directory was mounted")
	}
}
