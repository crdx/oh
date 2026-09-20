package util_test

import (
	"testing"

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
