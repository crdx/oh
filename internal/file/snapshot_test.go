package file_test

import (
	"errors"
	"path/filepath"
	"testing"

	"crdx.org/oh/internal/file"
)

func TestRestoringReadStateSkipsPathsThatAreNoLongerMounted(t *testing.T) {
	isWritable := false
	root, directory := testRoot(t, &isWritable)
	snapshots := file.NewSnapshots()
	content := []byte("current")

	state := file.EncodeReadState(
		file.NewReadSnapshot("current.txt", content),
		file.NewReadSnapshot(filepath.Join(directory, "..", "removed", "old.txt"), []byte("old")),
	)
	if err := snapshots.RestoreReadState(root, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := snapshots.Check(root, "current.txt", content); err != nil {
		t.Errorf("reachable snapshot was not restored: %v", err)
	}
	if err := snapshots.Check(root, "old.txt", []byte("old")); !errors.Is(err, file.ErrNotRead) {
		t.Errorf("unreachable snapshot was restored with %v", err)
	}
}
