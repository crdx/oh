package diagnostics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestWorkHoldingTheDrawingThreadIsWrittenDownWithEveryStack(t *testing.T) {
	session := t.TempDir()

	synctest.Test(t, func(t *testing.T) {
		watchdog := Watch(session)
		defer watchdog.Close()

		done := watchdog.Begin("keypress")
		sleepThroughAStall()
		done()
		synctest.Wait()
	})

	stalls := stallEntries(t, session)
	if len(stalls) != 1 {
		t.Fatalf("got %d stalls written down, want 1", len(stalls))
	}
	written, err := os.ReadFile(stalls[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "keypress held the drawing thread for 400ms") {
		t.Errorf("expected the work and how long it held on, got %q", firstLine(written))
	}
	if !strings.Contains(string(written), "sleepThroughAStall") {
		t.Error("expected the stacks to show what the drawing thread was doing")
	}
}

func sleepThroughAStall() {
	time.Sleep(400 * time.Millisecond)
}

func TestQuickWorkIsNotWrittenDown(t *testing.T) {
	session := t.TempDir()
	path := filepath.Join(session, DirectoryName)

	synctest.Test(t, func(t *testing.T) {
		watchdog := Watch(session)
		defer watchdog.Close()

		for range 10 {
			done := watchdog.Begin("draw")
			time.Sleep(100 * time.Millisecond)
			done()
		}
		synctest.Wait()
	})

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected nothing written for work under the threshold, got %v", err)
	}
}

func TestAStallWithNowhereToGoIsDroppedQuietly(t *testing.T) {
	session := filepath.Join(t.TempDir(), "unsent")

	synctest.Test(t, func(t *testing.T) {
		watchdog := Watch(session)
		defer watchdog.Close()

		done := watchdog.Begin("keypress")
		time.Sleep(time.Second)
		done()
		synctest.Wait()
	})

	if _, err := os.Stat(session); !os.IsNotExist(err) {
		t.Errorf("expected no session directory made for a stall, got %v", err)
	}
}

func TestEachStallIsWrittenToAFileOfItsOwn(t *testing.T) {
	session := t.TempDir()

	synctest.Test(t, func(t *testing.T) {
		watchdog := Watch(session)
		defer watchdog.Close()

		for range 2 {
			done := watchdog.Begin("resize")
			sleepThroughAStall()
			done()
			synctest.Wait()
		}
	})

	if stalls := stallEntries(t, session); len(stalls) != 2 {
		t.Errorf("got %d stall files, want 2", len(stalls))
	}
}

func stallEntries(t *testing.T, session string) []string {
	t.Helper()

	entries, err := filepath.Glob(filepath.Join(session, DirectoryName, StallDirectoryName, "*.txt"))
	if err != nil {
		t.Fatal(err)
	}

	return entries
}

func firstLine(text []byte) string {
	line, _, _ := strings.Cut(string(text), "\n")
	return line
}
