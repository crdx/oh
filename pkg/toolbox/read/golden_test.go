package read_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/file"
	"crdx.org/oh/pkg/tool/middleware/truncate"
	"crdx.org/oh/pkg/toolbox/read"
)

var updateGoldens = flag.Bool("update", false, "write what was read back to the golden files")

func TestGoldenATruncatedReadNamesWhereToContinue(t *testing.T) {
	const outputLimit = 14

	tests := []struct {
		name      string
		content   string
		arguments string
		isSaved   bool
	}{
		{
			name:      "a whole multiline file",
			content:   "alpha\nbravo\ncharlie\ndelta\necho\n",
			arguments: `{"path":"notes.txt"}`,
		},
		{
			name:      "a range beginning after the first line",
			content:   "alpha\nbravo\ncharlie\ndelta\necho\n",
			arguments: `{"path":"notes.txt","offset":3}`,
		},
		{
			name:      "a line too long to return whole",
			content:   strings.Repeat("x", 20) + "\nshort\n",
			arguments: `{"path":"notes.txt"}`,
			isSaved:   true,
		},
	}

	var drawn strings.Builder
	for _, test := range tests {
		root := testRoot(t, "notes.txt", test.content)
		limit := truncate.NewLimit(outputLimit)
		savedOutputs := 0
		limit.SaveOverflowWith(func(string) (string, error) {
			savedOutputs++
			return "/state/sessions/tame-impala/drops/output.txt", nil
		})

		subject := truncate.Tool(read.New(root, file.NewSnapshots()), limit)
		call, err := subject.Parse(test.arguments)
		if err != nil {
			t.Fatal(err)
		}
		result, err := call.Exec(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if wasSaved := savedOutputs > 0; wasSaved != test.isSaved {
			t.Errorf("%s: saved output is %t, want %t", test.name, wasSaved, test.isSaved)
		}

		fmt.Fprintf(&drawn, "=== %s ===\n%s\n\n", test.name, result.Output)
	}

	compareReadGolden(t, "truncated.txt", strings.TrimRight(drawn.String(), "\n")+"\n")
}

func compareReadGolden(t *testing.T, name string, drawn string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)
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
		t.Errorf("read differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}
