package read_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"crdx.org/oh/internal/file"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/read"
)

func testRoot(t *testing.T, name string, content string) *file.Root {
	t.Helper()

	directory := t.TempDir()

	if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
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

	call, err := read.New(root, file.NewSnapshots()).Parse(arguments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := call.Exec(t.Context())
	return result.Output, err
}

func TestAnImageIsAttachedForTheModel(t *testing.T) {
	content := "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 24)
	root := testRoot(t, "picture.png", content)
	call, err := read.New(root, file.NewSnapshots()).Parse(`{"path":"picture.png"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "image/png, 32B" {
		t.Errorf("expected an image description, got %q", result.Output)
	}

	if result.Image.MediaType != "image/png" || string(result.Image.Data) != content {
		t.Errorf("expected the PNG bytes, got %q and %d bytes", result.Image.MediaType, len(result.Image.Data))
	}
}

func TestAnImageReportsAnEstimateFromItsDimensions(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 64, 33))); err != nil {
		t.Fatalf("could not encode the test image: %v", err)
	}

	root := testRoot(t, "picture.png", encoded.String())
	call, err := read.New(root, file.NewSnapshots()).Parse(`{"path":"picture.png"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Metrics.Kind != tool.MetricImage || result.Metrics.EstimatedTokens != 4 {
		t.Errorf("expected a four-token image estimate, got %#v", result.Metrics)
	}
}

func TestAnOversizedImageIsEstimatedAtTheSizeThatWillBeSent(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 3200, 320))); err != nil {
		t.Fatalf("could not encode the test image: %v", err)
	}

	root := testRoot(t, "picture.png", encoded.String())
	call, err := read.New(root, file.NewSnapshots()).Parse(`{"path":"picture.png"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Metrics.EstimatedTokens != 49*5 {
		t.Errorf("expected the estimate to follow the bounded size, got %#v", result.Metrics)
	}
}

func TestAnImageCannotBeReadAsLines(t *testing.T) {
	content := "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 24)
	root := testRoot(t, "picture.png", content)

	_, err := exec(t, root, `{"path":"picture.png","limit":1}`)
	if err == nil || err.Error() != "images do not support line ranges" {
		t.Errorf("expected a line range to be refused, got %v", err)
	}
}

func TestAFileWithNoRangeComesBackWhole(t *testing.T) {
	root := testRoot(t, "notes.txt", "one\ntwo\nthree\n")

	output, err := exec(t, root, `{"path":"notes.txt"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "one\ntwo\nthree\n" {
		t.Errorf("expected the whole file, got %q", output)
	}
}

func TestALineRangeComesBackOnItsOwn(t *testing.T) {
	root := testRoot(t, "notes.txt", "one\ntwo\nthree\nfour\n")

	output, err := exec(t, root, `{"path":"notes.txt","offset":2,"limit":2}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "two\nthree" {
		t.Errorf("expected the second and third lines, got %q", output)
	}
}

func TestALineRangeMeasuresOnlyWhatComesBack(t *testing.T) {
	root := testRoot(t, "notes.txt", "one\ntwo\nthree\nfour\n")
	call, err := read.New(root, file.NewSnapshots()).Parse(`{"path":"notes.txt","offset":2,"limit":2}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Metrics.Lines != 2 || result.Metrics.Bytes != int64(len(result.Output)) {
		t.Errorf(
			"expected 2 lines and %d bytes, got %d lines and %d bytes",
			len(result.Output), result.Metrics.Lines, result.Metrics.Bytes,
		)
	}
}

func TestALineRangeOfAnEmptyFileIsEmpty(t *testing.T) {
	root := testRoot(t, "empty.txt", "")

	output, err := exec(t, root, `{"path":"empty.txt","offset":1,"limit":100}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output != "" {
		t.Errorf("expected empty output, got %q", output)
	}
}

func TestAnOffsetPastTheEndSaysHowLongTheFileIs(t *testing.T) {
	root := testRoot(t, "notes.txt", "one\ntwo\n")

	_, err := exec(t, root, `{"path":"notes.txt","offset":9}`)
	if err == nil {
		t.Fatal("expected an offset past the end to be refused")
	}

	if !strings.Contains(err.Error(), "2 lines") {
		t.Errorf("expected the length to be named, got %q", err)
	}
}

func TestAHugeFileIsNotCutShort(t *testing.T) {
	content := strings.Repeat("a line of text\n", 8000)
	root := testRoot(t, "big.txt", content)

	output, err := exec(t, root, `{"path":"big.txt"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != content {
		t.Errorf("expected %d bytes, got %d", len(content), len(output))
	}
}

func TestRangesOfFilesAboveTheReadLimit(t *testing.T) {
	root, name := largeTextRoot(t)
	execute := func(arguments string) (tool.ToolCallResult, error) {
		call, err := read.New(root, file.NewSnapshots()).Parse(arguments)
		if err != nil {
			t.Fatal(err)
		}
		return call.Exec(t.Context())
	}

	boundedResult, err := execute(fmt.Sprintf(`{"path":%q,"offset":2,"limit":1}`, name))
	if err != nil {
		t.Fatal(err)
	}
	if boundedResult.Output != "wanted" {
		t.Errorf("got %q, want %q", boundedResult.Output, "wanted")
	}
	if boundedResult.Metrics.Bytes != 6 || boundedResult.Metrics.Lines != 1 {
		t.Errorf("unexpected metrics: %+v", boundedResult.Metrics)
	}

	openResult, err := execute(fmt.Sprintf(`{"path":%q,"offset":2}`, name))
	if err != nil {
		t.Fatal(err)
	}
	if openResult.Output != "wanted" {
		t.Errorf("got %q, want %q", openResult.Output, "wanted")
	}

	_, err = execute(fmt.Sprintf(`{"path":%q,"limit":1}`, name))
	if err == nil || err.Error() != "range exceeds the 20M limit" {
		t.Errorf("unexpected oversized range failure: %v", err)
	}

	_, err = execute(fmt.Sprintf(`{"path":%q,"offset":3,"limit":1}`, name))
	if err == nil || err.Error() != "offset 3 exceeds the file's 2 lines" {
		t.Errorf("unexpected past-end failure: %v", err)
	}

	data, err := root.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var state file.ReadState
	if err := json.Unmarshal(boundedResult.State, &state); err != nil {
		t.Fatal(err)
	}
	wantSnapshot := file.NewReadSnapshot(name, data)
	if len(state.Files) != 1 || state.Files[0] != wantSnapshot {
		t.Errorf("got %+v, want %+v", state.Files, wantSnapshot)
	}
}

func TestARangeOfALargeFileCanBeCancelled(t *testing.T) {
	root, name := largeTextRoot(t)
	call, err := read.New(root, file.NewSnapshots()).Parse(fmt.Sprintf(`{"path":%q,"limit":1}`, name))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = call.Exec(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want a cancellation", err)
	}
}

func largeTextRoot(t *testing.T) (*file.Root, string) {
	t.Helper()

	const fileBytes = 20*1024*1024 + 1

	directory := t.TempDir()
	opened, err := os.CreateTemp(directory, "large")
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(opened.Name())
	chunk := strings.Repeat("a", 1024*1024)
	remainingBytes := fileBytes
	for remainingBytes > 0 {
		writtenBytes := min(remainingBytes, len(chunk))
		if _, err := opened.WriteString(chunk[:writtenBytes]); err != nil {
			_ = opened.Close()
			t.Fatal(err)
		}
		remainingBytes -= writtenBytes
	}
	if _, err := opened.WriteString("\nwanted\n"); err != nil {
		_ = opened.Close()
		t.Fatal(err)
	}
	if err := opened.Close(); err != nil {
		t.Fatal(err)
	}

	rootHandle, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rootHandle.Close() })
	return file.New(rootHandle, allowAll), name
}

func TestFilesAboveTheReadLimitAreRefusedBeforeTheirContentsAreLoaded(t *testing.T) {
	const fileBytes = 256 * 1024 * 1024

	tests := []struct {
		name      string
		header    string
		arguments string
		failure   string
	}{
		{
			name:      "image",
			header:    "\x89PNG\r\n\x1a\n",
			arguments: `{"path":"large"}`,
			failure:   "image exceeds the 20M limit",
		},
		{
			name:      "image range",
			header:    "\x89PNG\r\n\x1a\n",
			arguments: `{"path":"large","limit":1}`,
			failure:   "images do not support line ranges",
		},
		{
			name:      "text",
			header:    "plain text",
			arguments: `{"path":"large"}`,
			failure:   "file exceeds the 20M limit",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "large")
			opened, err := os.Create(path) //nolint:gosec // test-owned path
			if err != nil {
				t.Fatal(err)
			}
			if _, err := opened.WriteString(test.header); err != nil {
				_ = opened.Close()
				t.Fatal(err)
			}
			if err := opened.Truncate(fileBytes); err != nil {
				_ = opened.Close()
				t.Fatal(err)
			}
			if err := opened.Close(); err != nil {
				t.Fatal(err)
			}

			rootHandle, err := os.OpenRoot(directory)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = rootHandle.Close() }()
			root := file.New(rootHandle, allowAll)
			call, err := read.New(root, file.NewSnapshots()).Parse(test.arguments)
			if err != nil {
				t.Fatal(err)
			}

			runtime.GC()
			var before runtime.MemStats
			runtime.ReadMemStats(&before)
			result, err := call.Exec(t.Context())
			var after runtime.MemStats
			runtime.ReadMemStats(&after)

			if err == nil || err.Error() != test.failure {
				t.Fatalf("got %v, want %q", err, test.failure)
			}
			if result.Metrics.Bytes != fileBytes {
				t.Errorf("reported %d bytes, want %d", result.Metrics.Bytes, fileBytes)
			}
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 8*1024*1024 {
				t.Errorf("allocated %d bytes before refusing the file", allocated)
			}
		})
	}
}

func TestAMissingFileDoesNotExposeHowItWasOpened(t *testing.T) {
	root := testRoot(t, "notes.txt", "one\n")

	_, err := exec(t, root, `{"path":"missing.txt"}`)
	if err == nil {
		t.Fatal("expected a missing file to fail")
	}

	const expected = "missing.txt: no such file or directory"

	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err)
	}
}

func TestAPathOutsideTheRootIsRefused(t *testing.T) {
	root := testRoot(t, "notes.txt", "one\n")

	if _, err := exec(t, root, `{"path":"../../etc/passwd"}`); err == nil {
		t.Error("expected a path outside the root to be refused")
	}
}

func TestAFileInAMountedRootCanBeRead(t *testing.T) {
	root := testRoot(t, "notes.txt", "one\n")
	scratch := testRoot(t, "result.txt", "answer\n")
	root.Mount("/tmp", scratch)

	output, err := exec(t, root, `{"path":"/tmp/result.txt"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output != "answer\n" {
		t.Errorf("got %q, want %q", output, "answer\n")
	}
}

func TestAReadWithNoPathIsRefused(t *testing.T) {
	root := testRoot(t, "notes.txt", "one\n")

	if _, err := exec(t, root, `{}`); err == nil {
		t.Error("expected a read with no path to be refused")
	}
}

func TestAReadFocusesTheFileName(t *testing.T) {
	root := testRoot(t, "notes.txt", "one\n")
	call, err := read.New(root, file.NewSnapshots()).Parse(`{"path":"somewhere/notes.txt"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := tool.Emphasis{Kind: tool.EmphasisFocus, Value: "notes.txt"}
	if call.Emphasis() != want {
		t.Errorf("expected the file name to be focused, got %T", call)
	}
}

func TestRenderSaysWhichLinesAreBeingRead(t *testing.T) {
	subject, qualifier := read.Describe(read.Args{Path: "notes.txt", Offset: 10, Limit: 5})

	if subject != "notes.txt" {
		t.Errorf("expected the path, got %q", subject)
	}

	if qualifier != "10-14" {
		t.Errorf("expected the range, got %q", qualifier)
	}
}

func TestRenderLeavesPathDisplayProcessingToThePainter(t *testing.T) {
	const path = "/home/alice/.agents/skills/golang/SKILL.md"

	subject, _ := read.Describe(read.Args{Path: path})

	if subject != path {
		t.Errorf("got %q, want the unprocessed path %q", subject, path)
	}
}

func TestRenderLeavesAnOpenRangeOpen(t *testing.T) {
	_, qualifier := read.Describe(read.Args{Path: "notes.txt", Offset: 10})

	if qualifier != "10+" {
		t.Errorf("expected an open range, got %q", qualifier)
	}
}

func TestRenderSaysNothingAboutAWholeFile(t *testing.T) {
	_, qualifier := read.Describe(read.Args{Path: "notes.txt"})

	if qualifier != "" {
		t.Errorf("expected no range, got %q", qualifier)
	}
}

func allowAll(string) error { return nil }
