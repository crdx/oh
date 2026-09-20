package shell

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/internal/app/style"
)

var updateGoldens = flag.Bool("update", false, "write what was refused back to the golden files")

func TestGoldenTheRefusalOfAMachineThatCannotSandboxMatchesTheGolden(t *testing.T) {
	t.Cleanup(style.Init(&strings.Builder{}))

	drawn := style.Error(sandboxRefusal(errors.New("landlock is not available on this kernel"))) + "\n"

	goldenPath := filepath.Join("testdata", "refusal.txt")
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(drawn), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if drawn != string(want) {
		t.Errorf("refusal differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}

func TestAWaivedSandboxIsNeverRefused(t *testing.T) {
	if err := sandboxRefusal(nil); err != nil {
		t.Errorf("got %v, want a machine that can sandbox to be let through", err)
	}
}
