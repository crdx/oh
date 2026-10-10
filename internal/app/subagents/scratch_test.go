package subagents

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAManagerKeepsTheScratchOfEarlierChildren(t *testing.T) {
	family := newTestFamily(t)
	earlier := filepath.Join(family.scratch, ScratchName, "frugal-otter")
	if err := os.MkdirAll(earlier, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(earlier, "result"), []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	family.manager(t, waiting()).Close()
	written, err := os.ReadFile(filepath.Join(earlier, "result")) //nolint:gosec // the test's own path
	if err != nil || string(written) != "kept" {
		t.Errorf("an earlier child's scratch was not kept: %q, %v", written, err)
	}
}
