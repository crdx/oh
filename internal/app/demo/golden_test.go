package demo

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateGoldens = flag.Bool("update", false, "update golden files")

func TestGoldenRepliesMatchGolden(t *testing.T) {
	got := "abilities\n\n" + abilities +
		"\n\nmissing path\n\n" + readReply("read").Say + "\n"
	goldenPath := filepath.Join("testdata", "replies.txt")

	if *updateGoldens {
		if err := os.WriteFile(goldenPath, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, got, want)
	}
}
