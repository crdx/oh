package grep_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/io/internal/file"
	"crdx.org/io/internal/stop"
	"crdx.org/io/pkg/tool"
	"crdx.org/io/pkg/toolbox/grep"
)

func testRoot(t *testing.T, files map[string]string) *file.Root {
	t.Helper()

	directory := t.TempDir()

	for path, content := range files {
		full := filepath.Join(directory, path)

		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
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

	call, err := grep.New(root, file.NewSnapshots()).Parse(arguments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := call.Exec(t.Context())
	return result.Output, result.Metrics, err
}

func TestAMatchIsReportedWithItsPathAndLine(t *testing.T) {
	root := testRoot(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n"})

	output, err := exec(t, root, `{"pattern":"func main"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "main.go:3:func main() {}" {
		t.Errorf("expected the path, line and text, got %q", output)
	}
}

func TestTheNumberOfMatchingLinesIsReported(t *testing.T) {
	root := testRoot(t, map[string]string{"main.go": "hello\ngoodbye\nhello\n"})

	output, metrics, err := execWithMetrics(t, root, `{"pattern":"hello"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if want := tool.GetMetrics(output); metrics != want {
		t.Errorf("got metrics %+v, want %+v", metrics, want)
	}
}

func TestAGlobNarrowsWhatIsSearched(t *testing.T) {
	root := testRoot(t, map[string]string{
		"main.go":   "hello\n",
		"notes.txt": "hello\n",
	})

	output, err := exec(t, root, `{"pattern":"hello","glob":"**/*.go"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(output, "notes.txt") {
		t.Errorf("expected only the Go file, got %q", output)
	}
}

func TestIgnoredFilesAreNotSearched(t *testing.T) {
	root := testRoot(t, map[string]string{
		".gitignore":          "ignored/\n",
		"kept.txt":            "hello\n",
		"ignored/ignored.txt": "hello\n",
	})

	output, err := exec(t, root, `{"pattern":"hello"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "kept.txt:1:hello" {
		t.Errorf("expected ignored files to be skipped, got %q", output)
	}
}

func TestAPatternThatWillNotCompileIsRefused(t *testing.T) {
	root := testRoot(t, map[string]string{"main.go": "hello\n"})

	_, err := exec(t, root, `{"pattern":"("}`)
	if err == nil {
		t.Fatal("expected an invalid pattern to be refused")
	}

	if !strings.Contains(err.Error(), "invalid pattern") {
		t.Errorf("expected the refusal to name the pattern, got %q", err)
	}
}

func TestASearchThatFindsNothingSaysSo(t *testing.T) {
	root := testRoot(t, map[string]string{"main.go": "hello\n"})

	output, err := exec(t, root, `{"pattern":"goodbye"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "(no matches)" {
		t.Errorf("expected no matches to say so, got %q", output)
	}
}

func standInRipgrep(t *testing.T, script string) {
	t.Helper()

	directory := t.TempDir()
	path := filepath.Join(directory, "rg")

	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o700); err != nil { //nolint:gosec // a stand-in the test must be able to run
		t.Fatalf("unexpected error: %v", err)
	}

	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestASearchThatCouldNotRunIsNotCalledEmpty(t *testing.T) {
	root := testRoot(t, map[string]string{"main.go": "hello\n"})
	standInRipgrep(t, "echo 'shim: Permission denied' >&2\nexit 1\n")

	output, err := exec(t, root, `{"pattern":"hello"}`)
	if err == nil {
		t.Fatalf("got %q and no error, want a search that could not run to say so", output)
	}

	if !strings.Contains(err.Error(), "Permission denied") {
		t.Errorf("got %q, want what the search said for itself", err)
	}
}

func TestASearchThatFindsNothingQuietlySaysSo(t *testing.T) {
	root := testRoot(t, map[string]string{"main.go": "hello\n"})
	standInRipgrep(t, "exit 1\n")

	output, err := exec(t, root, `{"pattern":"hello"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "(no matches)" {
		t.Errorf("got %q, want no matches", output)
	}
}

func TestMatchCountDoesNotCapSmallResults(t *testing.T) {
	const matchCount = 150
	root := testRoot(t, map[string]string{
		"big.txt": strings.Repeat("hello\n", matchCount),
	})

	output, metrics, err := execWithMetrics(t, root, `{"pattern":"hello"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if metrics.Lines != matchCount || metrics.IsTruncated {
		t.Errorf("expected all %d small matches, got %+v", matchCount, metrics)
	}
	if strings.Contains(output, "narrow the search") {
		t.Errorf("expected the complete result, got %q", output)
	}
}

func TestHittingTheByteCapIsSaidOutLoud(t *testing.T) {
	root := testRoot(t, map[string]string{
		"big.txt": strings.Repeat(strings.Repeat("x", 100)+"\n", 500),
	})

	output, metrics, err := execWithMetrics(t, root, `{"pattern":"x"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(output, "matching output exceeded 16K") {
		t.Errorf("expected the byte cap to be reported, got the last of %q", output[len(output)-100:])
	}
	wantMetrics := tool.GetMetrics(output)
	wantMetrics.IsTruncated = true
	if metrics != wantMetrics {
		t.Errorf("got metrics %+v, want %+v", metrics, wantMetrics)
	}
}

func TestACancelledContextStopsTheSearch(t *testing.T) {
	root := testRoot(t, map[string]string{"main.go": "hello\n"})

	bin := t.TempDir()
	//nolint:gosec // an executable test fixture
	if err := os.WriteFile(
		filepath.Join(bin, "rg"),
		[]byte("#!/bin/sh\nexec /bin/sleep 60\n"),
		0o700,
	); err != nil {
		t.Fatalf("could not write fake rg: %v", err)
	}
	t.Setenv("PATH", bin)

	call, err := grep.New(root, file.NewSnapshots()).Parse(`{"pattern":"hello"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancelCause(t.Context())
	time.AfterFunc(100*time.Millisecond, func() { cancel(stop.Because("the user pressed escape")) })

	startedAt := time.Now()
	_, err = call.Exec(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context cancellation, got %v", err)
	}
	if want := "the search stopped because the user pressed escape"; err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
	if took := time.Since(startedAt); took > 2*time.Second {
		t.Errorf("expected the search to stop promptly, took %s", took)
	}
}

func TestCallHighlightsItsPatternAsRegexpSyntax(t *testing.T) {
	root := testRoot(t, nil)
	call, err := grep.New(root, file.NewSnapshots()).Parse(`{"pattern":"foo|bar","path":"internal/file.go"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := tool.Emphasis{Kind: tool.EmphasisSyntax, Value: "regexp"}
	if call.Emphasis() != want {
		t.Errorf("expected regexp emphasis, got %T", call)
	}
}

func TestRenderSaysNothingOfTheWorkingDirectory(t *testing.T) {
	subject, qualifier := grep.Describe(grep.Args{Pattern: "hello", Path: "."})
	if subject != "hello" || qualifier != "" {
		t.Errorf("expected the path to go without saying, got %q and %q", subject, qualifier)
	}

	subject, qualifier = grep.Describe(grep.Args{Pattern: "hello", Path: "internal", Glob: "*.go"})
	if subject != "hello" || qualifier != "internal *.go" {
		t.Errorf("expected a path and glob to be named, got %q and %q", subject, qualifier)
	}
}

func TestASymbolicLinkOutOfTheRootIsNotSearched(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	workspace := filepath.Join(base, "workspace")

	for _, directory := range []string{outside, workspace} {
		if err := os.MkdirAll(directory, 0o750); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	secret := filepath.Join(outside, "credentials")
	if err := os.WriteFile(secret, []byte("password\n"), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := os.Symlink(outside, filepath.Join(workspace, "directory")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := os.Symlink(secret, filepath.Join(workspace, "file")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	openedRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = openedRoot.Close() })

	root := file.New(openedRoot, allowAll)

	for _, path := range []string{"directory", "file", "directory/credentials"} {
		output, err := exec(t, root, `{"pattern":"password","path":"`+path+`"}`)
		if err == nil {
			t.Errorf("expected %s to be refused, got %q", path, output)
		}
		if strings.Contains(output, "password") {
			t.Errorf("expected nothing outside the root to be read, got %q", output)
		}
	}
}

func TestASymbolicLinkInsideTheRootIsStillSearched(t *testing.T) {
	root := testRoot(t, map[string]string{"inner/main.go": "hello\n"})

	if err := os.Symlink("inner", filepath.Join(root.Name(), "linked")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output, err := exec(t, root, `{"pattern":"hello","path":"linked"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(output, "hello") {
		t.Errorf("expected the linked directory to be searched, got %q", output)
	}
}

func allowAll(string) error { return nil }

func TestDeniedNamesAreNotReadBySearches(t *testing.T) {
	root := testRoot(t, map[string]string{
		"foo.txt":        "secret\n",
		"nested/foo.txt": "secret\n",
		"visible.txt":    "public\n",
	})
	root.SetExcludedNames([]string{"foo.txt"})

	output, err := exec(t, root, `{"pattern":"secret"}`)
	if err != nil {
		t.Fatal(err)
	}
	if output != "(no matches)" {
		t.Errorf("got %q, want no matches", output)
	}
}
