package diagnostics

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestAHeapProfileIsWrittenIntoTheSessionWhenItCloses(t *testing.T) {
	session := t.TempDir()

	ProfileHeap(session).Close()

	snapshots := heapSnapshots(t, session)
	if len(snapshots) != 1 {
		t.Fatalf("got %d snapshots, want 1", len(snapshots))
	}
	written, err := os.ReadFile(snapshots[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(written, gzipMagic) {
		t.Errorf("expected a gzipped pprof profile, got %q", written[:min(len(written), 8)])
	}
}

func TestEachHeapSnapshotIsWrittenOnItsOwn(t *testing.T) {
	session := t.TempDir()

	profiler := ProfileHeap(session)
	profiler.snapshot()
	profiler.Close()

	if snapshots := heapSnapshots(t, session); len(snapshots) != 2 {
		t.Errorf("got %d snapshots, want 2", len(snapshots))
	}
}

func TestAHeapProfileLeavesASessionNeverPersistedUncreated(t *testing.T) {
	session := filepath.Join(t.TempDir(), "unsent")

	ProfileHeap(session).Close()

	if _, err := os.Stat(session); !os.IsNotExist(err) {
		t.Errorf("expected no session directory, got %v", err)
	}
}

func heapSnapshots(t *testing.T, session string) []string {
	t.Helper()

	snapshots, err := filepath.Glob(filepath.Join(session, DirectoryName, HeapDirectoryName, "*.pprof"))
	if err != nil {
		t.Fatal(err)
	}

	return snapshots
}
