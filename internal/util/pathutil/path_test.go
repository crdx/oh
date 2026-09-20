package pathutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"crdx.org/oh/internal/util/pathutil"
)

func TestShortenWritesAPathTheWayTheUserWouldSayIt(t *testing.T) {
	t.Setenv("HOME", "/home/alice")

	tests := map[string]string{
		"/home/alice/florp/io":      "~/florp/io",
		"/home/alice":               "~",
		"/home/alice-other/project": "/home/alice-other/project",
		"/etc/hosts":                "/etc/hosts",
		"florp/io":                  "florp/io",
		"":                          "",
	}

	for path, want := range tests {
		if got := pathutil.Shorten(path); got != want {
			t.Errorf("got %q for %q, want %q", got, path, want)
		}
	}
}

func TestShortenLeavesAPathAloneWithoutAHome(t *testing.T) {
	t.Setenv("HOME", "")

	if got := pathutil.Shorten("/home/alice/florp"); got != "/home/alice/florp" {
		t.Errorf("got %q, want the path as it went in", got)
	}
}

func TestExpandReadsAPathTheWayTheUserWroteIt(t *testing.T) {
	t.Setenv("HOME", "/home/alice")

	tests := map[string]string{
		"~/florp/io": "/home/alice/florp/io",
		"~":          "/home/alice",
		"~other/dir": "~other/dir",
		"/etc/hosts": "/etc/hosts",
		"florp/io":   "florp/io",
		"":           "",
	}

	for path, want := range tests {
		got, err := pathutil.Expand(path)
		if err != nil || got != want {
			t.Errorf("got %q and %v for %q, want %q", got, err, path, want)
		}
	}
}

func TestExpandFailsWithoutAHome(t *testing.T) {
	t.Setenv("HOME", "")

	if _, err := pathutil.Expand("~/florp"); err == nil {
		t.Error("expected a home path without a home to fail")
	}
	if got, err := pathutil.Expand("/etc/hosts"); err != nil || got != "/etc/hosts" {
		t.Errorf("got %q and %v, want the path as it went in", got, err)
	}
}

func TestExistsReportsWhatCanBeStatted(t *testing.T) {
	directory := t.TempDir()
	if !pathutil.Exists(directory) {
		t.Errorf("expected %q to exist", directory)
	}
	if pathutil.Exists(filepath.Join(directory, "absent")) {
		t.Error("expected an absent path not to exist")
	}
}

func TestRelativeToNamesAPathInsideTheRoot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "somewhere", "file.go")

	got, ok := pathutil.RelativeTo(root, path)
	if !ok {
		t.Fatal("expected the path to be inside the root")
	}
	if want := filepath.Join("somewhere", "file.go"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRelativeToRejectsAPathOutsideTheRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "outside")

	if name, ok := pathutil.RelativeTo(root, outside); ok {
		t.Errorf("expected an outside path to be rejected, got %q", name)
	}
}

func TestCanonicaliseFollowsASymlinkedSpellingToTheRealPath(t *testing.T) {
	realDirectory := t.TempDir()
	elsewhere := t.TempDir()
	link := filepath.Join(elsewhere, "link")

	if err := os.Symlink(realDirectory, link); err != nil {
		t.Fatal(err)
	}

	resolved, err := filepath.EvalSymlinks(realDirectory)
	if err != nil {
		t.Fatal(err)
	}

	for name, test := range map[string]struct {
		path string
		want string
	}{
		"the link itself": {path: link, want: resolved},
		"a name below it": {
			path: filepath.Join(link, "note.md"),
			want: filepath.Join(resolved, "note.md"),
		},
		"a name that is not there yet": {
			path: filepath.Join(link, "one", "two"),
			want: filepath.Join(resolved, "one", "two"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := pathutil.Canonicalise(test.path); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestCanonicaliseLeavesAPathWithNothingToFollowAlone(t *testing.T) {
	nowhere := "/no/such/place"

	if got := pathutil.Canonicalise(nowhere); got != nowhere {
		t.Errorf("got %q, want the path untouched", got)
	}
}
