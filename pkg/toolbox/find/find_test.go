package find_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/internal/file"
	"crdx.org/io/pkg/tool"
	"crdx.org/io/pkg/toolbox/find"
)

func testRoot(t *testing.T, paths ...string) *file.Root {
	t.Helper()

	directory := t.TempDir()

	for _, path := range paths {
		full := filepath.Join(directory, path)

		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if err := os.WriteFile(full, []byte("content\n"), 0o600); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = root.Close() })

	return file.New(root, allowAll)
}

func exec(t *testing.T, root *file.Root, arguments string) (string, error) {
	t.Helper()

	output, _, err := execWithMetrics(t, root, arguments)
	return output, err
}

func execWithMetrics(
	t *testing.T,
	root *file.Root,
	arguments string,
) (string, tool.ToolCallMetrics, error) {
	t.Helper()

	call, err := find.New(root).Parse(arguments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := call.Exec(t.Context())
	return result.Output, result.Metrics, err
}

func TestAGlobMatchesAcrossDirectories(t *testing.T) {
	root := testRoot(t, "main.go", "inner/deep/thing.go", "inner/notes.txt")

	output, err := exec(t, root, `{"pattern":"**/*.go"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := strings.Split(output, "\n")

	if len(found) != 2 {
		t.Fatalf("expected both Go files, got %q", output)
	}

	if !strings.Contains(output, "main.go") || !strings.Contains(output, "thing.go") {
		t.Errorf("expected both Go files, got %q", output)
	}
}

func TestASearchStartsWhereItIsTold(t *testing.T) {
	root := testRoot(t, "main.go", "inner/thing.go")

	output, err := exec(t, root, `{"pattern":"*.go","path":"inner"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "inner/thing.go" {
		t.Errorf("expected only the file below inner, got %q", output)
	}
}

func TestTheGitDirectoryIsNotSearched(t *testing.T) {
	root := testRoot(t, ".git/objects/thing.go", "main.go")

	output, err := exec(t, root, `{"pattern":"**/*.go"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(output, ".git") {
		t.Errorf("expected .git to be skipped, got %q", output)
	}
}

func TestASearchThatFindsNothingSaysSo(t *testing.T) {
	root := testRoot(t, "main.go")

	output, err := exec(t, root, `{"pattern":"**/*.rb"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "(no matches)" {
		t.Errorf("expected no matches to say so, got %q", output)
	}
}

func TestMatchCountDoesNotCapSmallResults(t *testing.T) {
	const matchCount = 150
	paths := make([]string, matchCount)
	for i := range matchCount {
		paths[i] = fmt.Sprintf("file-%03d.txt", i)
	}
	root := testRoot(t, paths...)

	output, err := exec(t, root, `{"pattern":"*.txt"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if found := strings.Split(output, "\n"); len(found) != matchCount {
		t.Errorf("expected all %d small results, got %d", matchCount, len(found))
	}
	if strings.Contains(output, "narrow the search") {
		t.Errorf("expected the complete result, got %q", output)
	}
}

func TestHittingTheByteCapIsSaidOutLoud(t *testing.T) {
	const pathCount = 200
	paths := make([]string, pathCount)
	for i := range pathCount {
		paths[i] = fmt.Sprintf("directory-%03d-%s/file.txt", i, strings.Repeat("x", 80))
	}
	root := testRoot(t, paths...)

	output, metrics, err := execWithMetrics(t, root, `{"pattern":"**/*.txt"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(output, "matching output exceeded 16K") {
		t.Errorf("expected the byte cap to be reported, got the last of %q", output[len(output)-100:])
	}
	if strings.Contains(output, paths[pathCount-1]) {
		t.Errorf("expected later results to be omitted, got %q", output)
	}
	wantMetrics := tool.GetMetrics(output)
	wantMetrics.IsTruncated = true
	if metrics != wantMetrics {
		t.Errorf("got metrics %+v, want %+v", metrics, wantMetrics)
	}
}

func TestASearchWithNoPatternIsRefused(t *testing.T) {
	root := testRoot(t, "main.go")

	if _, err := exec(t, root, `{}`); err == nil {
		t.Error("expected a search with no pattern to be refused")
	}
}

func allowAll(string) error { return nil }

func TestDeniedNamesAreAbsentFromSearches(t *testing.T) {
	root := testRoot(t, "foo.txt", "nested/foo.txt", "visible.txt")
	root.SetAccessRefusal(func(path string) error {
		if filepath.Base(path) == "foo.txt" {
			return os.ErrPermission
		}
		return nil
	})

	output, err := exec(t, root, `{"pattern":"**"}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output, "foo.txt") || !strings.Contains(output, "visible.txt") {
		t.Errorf("got %q, want only visible matches", output)
	}
}
