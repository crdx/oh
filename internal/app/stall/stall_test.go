package stall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestWorkHoldingTheDrawingThreadIsWrittenDownWithEveryStack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stalls.txt")

	synctest.Test(t, func(t *testing.T) {
		watchdog := Watch(path)
		defer watchdog.Close()

		done := watchdog.Begin("keypress")
		sleepThroughAStall()
		done()
		synctest.Wait()
	})

	written, err := os.ReadFile(path) //nolint:gosec // the test's own stall log
	if err != nil {
		t.Fatalf("expected the stall written down: %v", err)
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
	path := filepath.Join(t.TempDir(), "stalls.txt")

	synctest.Test(t, func(t *testing.T) {
		watchdog := Watch(path)
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
	path := filepath.Join(t.TempDir(), "missing", "stalls.txt")

	synctest.Test(t, func(t *testing.T) {
		watchdog := Watch(path)
		defer watchdog.Close()

		done := watchdog.Begin("keypress")
		time.Sleep(time.Second)
		done()
		synctest.Wait()
	})

	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Errorf("expected no directory made for a stall, got %v", err)
	}
}

func firstLine(text []byte) string {
	line, _, _ := strings.Cut(string(text), "\n")
	return line
}
