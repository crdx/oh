package read

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"

	"crdx.org/io/internal/file"
	"crdx.org/io/internal/util/strutil"
)

const fuzzedRangeMaximumBytes = 64 * 1024

func FuzzRangedReadMatchesInMemorySelection(fuzzer *testing.F) {
	for _, seed := range []struct {
		content string
		offset  int
		limit   int
	}{
		{"", 1, 0},
		{"\n", 1, 0},
		{"a", 1, 0},
		{"a\n", 1, 0},
		{"a\nb", 2, 0},
		{"a\nb\n", 2, 1},
		{"\n\n\n", 2, 1},
		{"one\r\ntwo\r\nthree\r\n", 1, 2},
		{"one\ntwo\nthree\n", 9, 0},
		{"one\ntwo\nthree\n", 0, 1},
		{"one\ntwo\nthree\n", -1, 2},
		{strings.Repeat("x", 8192) + "\n" + "tail\n", 1, 2},
		{strings.Repeat("no newline at all", 500), 1, 1},
	} {
		fuzzer.Add(seed.content, seed.offset, seed.limit)
	}

	fuzzer.Fuzz(func(t *testing.T, content string, offset int, limit int) {
		if len(content) > fuzzedRangeMaximumBytes {
			t.Skip("content too large for the fuzz campaign to stay fast")
		}
		if offset <= 0 && limit <= 0 {
			t.Skip("loadRange is only ever called with a real offset or limit")
		}

		wantOutput, wantSelectedLines, wantErr := referenceRangeSelect(content, offset, limit)

		root, name := fuzzRangeRoot(t, content)
		got, err := loadRange(t.Context(), root, name, Args{Offset: offset, Limit: limit})

		switch {
		case wantErr != nil:
			if err == nil || err.Error() != wantErr.Error() {
				t.Fatalf("content %q offset %d limit %d: got error %v, want %v", content, offset, limit, err, wantErr)
			}
			return
		case err != nil:
			t.Fatalf("content %q offset %d limit %d: unexpected error %v", content, offset, limit, err)
		}

		if got.output != wantOutput {
			t.Errorf("content %q offset %d limit %d: got output %q, want %q", content, offset, limit, got.output, wantOutput)
		}
		if got.selectedLines != wantSelectedLines {
			t.Errorf(
				"content %q offset %d limit %d: got %d selected lines, want %d",
				content, offset, limit, got.selectedLines, wantSelectedLines,
			)
		}

		wantHash := sha256.Sum256([]byte(content))
		if got.contentHash != hex.EncodeToString(wantHash[:]) {
			t.Errorf("content %q: the reported hash did not cover the whole file", content)
		}
	})
}

func referenceRangeSelect(content string, offset int, limit int) (string, int64, error) {
	lines := strutil.Lines(content)
	if len(lines) == 0 {
		return "", 0, nil
	}

	start := 0
	if offset > 0 {
		start = offset - 1
	}
	if start >= len(lines) {
		return "", 0, fmt.Errorf("offset %d exceeds the file's %d lines", offset, len(lines))
	}

	end := len(lines)
	if limit > 0 && start+limit < end {
		end = start + limit
	}

	return strings.Join(lines[start:end], "\n"), int64(end - start), nil
}

func fuzzRangeRoot(t *testing.T, content string) (*file.Root, string) {
	t.Helper()

	directory := t.TempDir()
	const name = "fuzzed.txt"
	if err := os.WriteFile(directory+string(os.PathSeparator)+name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	rootHandle, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rootHandle.Close() })

	return file.New(rootHandle, func(string) error { return nil }), name
}
