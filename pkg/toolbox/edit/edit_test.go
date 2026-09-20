package edit_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/file"
	"crdx.org/oh/pkg/toolbox/edit"
)

func testRoot(t *testing.T, content string) (*file.Root, string) {
	t.Helper()

	directory := t.TempDir()

	if err := os.WriteFile(filepath.Join(directory, "a.txt"), []byte(content), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = root.Close() })

	return file.New(root, allowAll), directory
}

func exec(t *testing.T, root *file.Root, arguments string) error {
	t.Helper()

	var args edit.Args
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	snapshots := file.NewSnapshots()
	resolvedRoot, name, resolveErr := root.Resolve(args.Path)
	if resolveErr == nil {
		if content, err := resolvedRoot.ReadFile(name); err == nil {
			snapshots.Record(resolvedRoot, name, content)
		}
	}

	call, err := edit.New(root, snapshots).Parse(arguments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = call.Exec(t.Context())
	return err
}

func TestTheTextIsReplacedWhereItAppearsOnce(t *testing.T) {
	root, directory := testRoot(t, "one\ntwo\nthree\n")

	if err := exec(t, root, `{"path":"a.txt","old_text":"two","new_text":"2"}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	editedContents, err := os.ReadFile(filepath.Join(directory, "a.txt")) //nolint:gosec // a path this test made itself
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(editedContents) != "one\n2\nthree\n" {
		t.Errorf("expected the replacement, got %q", editedContents)
	}
}

func TestTextAppearingTwiceIsRefused(t *testing.T) {
	root, directory := testRoot(t, "one\none\n")

	err := exec(t, root, `{"path":"a.txt","old_text":"one","new_text":"1"}`)
	if err == nil {
		t.Fatal("expected ambiguous text to be refused")
	}

	if !strings.Contains(err.Error(), "not unique") {
		t.Errorf("expected the refusal to say why, got %q", err)
	}

	unchangedContents, err := os.ReadFile(filepath.Join(directory, "a.txt")) //nolint:gosec // a path this test made itself
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(unchangedContents) != "one\none\n" {
		t.Errorf("expected the file to be left alone, got %q", unchangedContents)
	}
}

func TestTextThatIsNotThereIsRefused(t *testing.T) {
	root, _ := testRoot(t, "one\n")

	if err := exec(t, root, `{"path":"a.txt","old_text":"nine","new_text":"9"}`); err == nil {
		t.Error("expected missing text to be refused")
	}
}

func TestTheFileKeepsItsMode(t *testing.T) {
	root, directory := testRoot(t, "one\n")

	if err := exec(t, root, `{"path":"a.txt","old_text":"one","new_text":"1"}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, err := os.Stat(filepath.Join(directory, "a.txt"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected the mode to survive, got %v", info.Mode().Perm())
	}
}

func TestEditingInsideAGitDirectoryIsRefused(t *testing.T) {
	root, directory := gitGuardedRoot(t)

	if err := os.Mkdir(filepath.Join(directory, ".git"), 0o750); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	config := filepath.Join(directory, ".git/config")
	if err := os.WriteFile(config, []byte("one\n"), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err := exec(t, root, `{"path":".git/config","old_text":"one","new_text":"1"}`)
	if !errors.Is(err, file.ErrGitDir) {
		t.Errorf("expected an edit inside .git to be refused, got %v", err)
	}
}

func allowAll(string) error { return nil }

func gitGuardedRoot(t *testing.T) (*file.Root, string) {
	t.Helper()

	directory := t.TempDir()

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = root.Close() })

	return file.New(root, file.RefuseGitDir), directory
}

func switchableRoot(t *testing.T, writable *bool) (*file.Root, string) {
	t.Helper()

	directory := t.TempDir()

	if err := os.WriteFile(filepath.Join(directory, "a.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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

func TestEditingIsRefusedWhileTheTreeIsReadOnly(t *testing.T) {
	isWritable := false
	root, directory := switchableRoot(t, &isWritable)

	arguments := `{"path":"a.txt","old_text":"one","new_text":"two"}`

	if err := exec(t, root, arguments); !errors.Is(err, file.ErrReadOnly) {
		t.Errorf("expected the edit to be refused, got %v", err)
	}

	data, err := os.ReadFile(filepath.Join(directory, "a.txt")) //nolint:gosec // a path this test made itself
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(data) != "one\n" {
		t.Errorf("expected the file to be untouched, got %q", data)
	}
}

func TestTheToolSaysItWritesEvenOverAReadOnlyTree(t *testing.T) {
	isWritable := false
	root, _ := switchableRoot(t, &isWritable)

	if edit.New(root, file.NewSnapshots()).ReadOnly() {
		t.Error("expected an edit tool to say it writes whatever the tree of the moment allows")
	}
}
