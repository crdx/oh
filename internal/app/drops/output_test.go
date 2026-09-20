package drops_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/drops"
)

func TestSavedOutputIsWrittenIntoTheDropsDirectory(t *testing.T) {
	sessionDirectory := filepath.Join(t.TempDir(), "tame-impala")
	whole := strings.Repeat("a line of text\n", 4000)

	path, err := drops.SaveOutput(sessionDirectory, persisted(sessionDirectory), whole)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := filepath.Dir(path), drops.GetDirectory(sessionDirectory); got != want {
		t.Errorf("output was written to %q, want it under %q", got, want)
	}
	if got := filepath.Base(path); !strings.HasPrefix(got, "output-") || !strings.HasSuffix(got, ".txt") {
		t.Errorf("output is named %q, want an output-*.txt", got)
	}

	written, err := os.ReadFile(path) //nolint:gosec // a path this test just created
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != whole {
		t.Errorf("output holds %d bytes, want the whole %d", len(written), len(whole))
	}
}

func TestTheSameOutputSavedTwiceIsKeptOnce(t *testing.T) {
	sessionDirectory := filepath.Join(t.TempDir(), "tame-impala")

	first, err := drops.SaveOutput(sessionDirectory, persisted(sessionDirectory), "the whole of it\n")
	if err != nil {
		t.Fatal(err)
	}
	second, err := drops.SaveOutput(sessionDirectory, persisted(sessionDirectory), "the whole of it\n")
	if err != nil {
		t.Fatal(err)
	}

	if first != second {
		t.Errorf("saved to %q and %q, want identical output kept once", first, second)
	}

	entries, err := os.ReadDir(drops.GetDirectory(sessionDirectory))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("the drops directory holds %d files, want one", len(entries))
	}
}

func TestOutputThatDiffersTakesItsOwnFile(t *testing.T) {
	sessionDirectory := filepath.Join(t.TempDir(), "tame-impala")

	first, err := drops.SaveOutput(sessionDirectory, persisted(sessionDirectory), "one\n")
	if err != nil {
		t.Fatal(err)
	}
	second, err := drops.SaveOutput(sessionDirectory, persisted(sessionDirectory), "another\n")
	if err != nil {
		t.Fatal(err)
	}

	if first == second {
		t.Errorf("both were saved to %q, want a file each", first)
	}
}
