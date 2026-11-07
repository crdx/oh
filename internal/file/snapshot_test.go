package file_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestHashingAFileMatchesHashingItsContent(t *testing.T) {
	isWritable := true
	root, directory := testRoot(t, &isWritable)
	content := []byte(strings.Repeat("a line of text\n", 100000))
	if err := os.WriteFile(filepath.Join(directory, "notes.txt"), content, 0o600); err != nil {
		t.Fatal(err)
	}

	hash, err := file.HashFile(t.Context(), root, "notes.txt")
	if err != nil || hash != file.Hash(content) {
		t.Errorf("got %q and %v, want %q", hash, err, file.Hash(content))
	}
}

func TestHashingAFileNeverHoldsItWhole(t *testing.T) {
	isWritable := true
	root, directory := testRoot(t, &isWritable)
	const fileBytes = 64 << 20
	if err := os.WriteFile(filepath.Join(directory, "large.log"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(filepath.Join(directory, "large.log"), fileBytes); err != nil {
		t.Fatal(err)
	}

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if _, err := file.HashFile(t.Context(), root, "large.log"); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)

	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > fileBytes/8 {
		t.Errorf("allocated %d bytes to hash a %d byte file", allocated, fileBytes)
	}
}

func TestHashingAFileStopsOnceCancelled(t *testing.T) {
	isWritable := true
	root, directory := testRoot(t, &isWritable)
	if err := os.WriteFile(filepath.Join(directory, "notes.txt"), []byte("text"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := file.HashFile(ctx, root, "notes.txt"); !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want the cancellation", err)
	}
}
