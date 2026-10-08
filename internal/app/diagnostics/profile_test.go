package diagnostics

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

var gzipMagic = []byte{0x1f, 0x8b}

func TestAProfileIsWrittenIntoTheSessionWhenItCloses(t *testing.T) {
	session := t.TempDir()

	profiler, err := Profile(session)
	if err != nil {
		t.Fatal(err)
	}
	profiler.Close()

	segments := profileSegments(t, session)
	if len(segments) != 1 {
		t.Fatalf("got %d segments, want 1", len(segments))
	}
	written, err := os.ReadFile(segments[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(written, gzipMagic) {
		t.Errorf("expected a gzipped pprof profile, got %q", written[:min(len(written), 8)])
	}
}

func TestEachSegmentIsWrittenOnItsOwn(t *testing.T) {
	session := t.TempDir()

	profiler, err := Profile(session)
	if err != nil {
		t.Fatal(err)
	}
	if !profiler.rotate() {
		t.Fatal("expected the next segment to start")
	}
	profiler.Close()

	if segments := profileSegments(t, session); len(segments) != 2 {
		t.Errorf("got %d segments, want 2", len(segments))
	}
}

func TestASessionNeverPersistedIsLeftUncreated(t *testing.T) {
	session := filepath.Join(t.TempDir(), "unsent")

	profiler, err := Profile(session)
	if err != nil {
		t.Fatal(err)
	}
	profiler.Close()

	if _, err := os.Stat(session); !os.IsNotExist(err) {
		t.Errorf("expected no session directory, got %v", err)
	}
}

func TestOnlyOneProfileRunsAtOnce(t *testing.T) {
	profiler, err := Profile(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer profiler.Close()

	if _, err := Profile(t.TempDir()); err == nil {
		t.Error("expected a second profile refused while the first runs")
	}
}

func profileSegments(t *testing.T, session string) []string {
	t.Helper()

	segments, err := filepath.Glob(filepath.Join(session, DirectoryName, CPUDirectoryName, "*.pprof"))
	if err != nil {
		t.Fatal(err)
	}

	return segments
}
