package drops_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/internal/app/drops"
)

func TestSavedHTMLIsWrittenUnchangedIntoTheDropsDirectory(t *testing.T) {
	sessionDirectory := filepath.Join(t.TempDir(), "tame-impala")
	contents := []byte("<!DOCTYPE html>\n<title>A &amp; B</title>\n")

	path, err := drops.SaveHTML(sessionDirectory, persisted(sessionDirectory), contents)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := filepath.Dir(path), drops.GetDirectory(sessionDirectory); got != want {
		t.Errorf("HTML was written to %q, want it under %q", got, want)
	}
	if got := filepath.Base(path); !strings.HasPrefix(got, "fetch-") || !strings.HasSuffix(got, ".html") {
		t.Errorf("HTML is named %q, want fetch-*.html", got)
	}

	written, err := os.ReadFile(path) //nolint:gosec // a path this test just created
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != string(contents) {
		t.Errorf("HTML holds %q, want %q", written, contents)
	}
}
