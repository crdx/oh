package util_test

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"crdx.org/oh/internal/util"
)

func TestASearchIsDescribedByItsPatternAndWhatQualifiesIt(t *testing.T) {
	pattern, qualifier := util.DescribeSearch("func New", "internal", "*.go")

	if pattern != "func New" {
		t.Errorf("got pattern %q, want the pattern searched for", pattern)
	}
	if want := "internal *.go"; qualifier != want {
		t.Errorf("got qualifier %q, want %q", qualifier, want)
	}
}

func TestASearchLeavesPathDisplayProcessingToThePainter(t *testing.T) {
	const path = "/home/alice/florp/io"

	if _, qualifier := util.DescribeSearch("func New", path, ""); qualifier != path {
		t.Errorf("got %q, want the unprocessed path %q", qualifier, path)
	}
}

func TestTheWorkingDirectoryGoesWithoutSaying(t *testing.T) {
	if _, qualifier := util.DescribeSearch("func New", ".", ""); qualifier != "" {
		t.Errorf("got qualifier %q, want nothing said about the working directory", qualifier)
	}
}

func TestACancelledWalkStopsBeforeTheNextEntry(t *testing.T) {
	filesystem := fstest.MapFS{
		"first.txt":  &fstest.MapFile{Data: []byte("first")},
		"second.txt": &fstest.MapFile{Data: []byte("second")},
	}
	ctx, cancel := context.WithCancel(t.Context())
	visitedFiles := 0

	err := util.Walk(ctx, filesystem, ".", func(_ string, entry fs.DirEntry) error {
		if entry.IsDir() {
			return nil
		}
		visitedFiles++
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want a cancellation", err)
	}
	if visitedFiles != 1 {
		t.Errorf("visited %d files, want only the file that cancelled the walk", visitedFiles)
	}
}
