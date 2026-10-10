package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func settledDir(t *testing.T) string {
	t.Helper()

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	return base
}

func makeDir(t *testing.T, path string) string {
	t.Helper()

	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	return path
}

func makeLink(t *testing.T, path string, target string) string {
	t.Helper()

	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}

	return path
}

func grantedPath(t *testing.T, path string, writableRoots []string) (string, error) {
	t.Helper()

	fd, err := openGrantPath(path, writableRoots)
	if err != nil {
		return "", err
	}

	defer func() { _ = unix.Close(fd) }()

	opened, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
	if err != nil {
		t.Fatal(err)
	}

	return opened, nil
}

func requireGrantRefused(t *testing.T, path string, roots []string, wanted string) {
	t.Helper()

	if opened, err := grantedPath(t, path, roots); err == nil {
		t.Errorf("granting %s opened %s, want %s", path, opened, wanted)
	}

	if _, redirects := FirstSymlinkBeneath(path, roots); !redirects {
		t.Errorf("a policy granting %s was accepted, want %s", path, wanted)
	}
}

type plantedTree struct {
	base     string
	writable string
	planted  string
	secret   string
}

func plantALinkInAWritablePath(t *testing.T) plantedTree {
	t.Helper()

	base := settledDir(t)
	writable := makeDir(t, filepath.Join(base, "writable"))
	secret := makeDir(t, filepath.Join(base, "secret"))

	return plantedTree{
		base:     base,
		writable: writable,
		secret:   secret,
		planted:  makeLink(t, filepath.Join(writable, "planted"), secret),
	}
}

func TestAChildScratchCannotRedirectItsMountIntoTheHost(t *testing.T) {
	base := settledDir(t)
	parent := makeDir(t, filepath.Join(base, "parent"))
	outside := makeDir(t, filepath.Join(base, "outside"))
	makeDir(t, filepath.Join(outside, "agent-1"))
	makeLink(t, filepath.Join(parent, "subagents"), outside)
	child := filepath.Join(parent, "subagents", "agent-1")
	if err := attachProtected(child, "/tmp", nil, []string{parent}); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("child scratch symlink was not refused: %v", err)
	}
}

func TestAGrantMayPassThroughASymlinkNoCommandCanReach(t *testing.T) {
	base := settledDir(t)
	target := makeDir(t, filepath.Join(base, "target"))
	alias := makeLink(t, filepath.Join(base, "alias"), target)

	opened, err := grantedPath(t, alias, nil)
	if err != nil {
		t.Fatalf("got %v, want a link outside every writable path followed", err)
	}
	if opened != target {
		t.Errorf("got %s, want %s", opened, target)
	}

	if link, redirects := FirstSymlinkBeneath(alias, nil); redirects {
		t.Errorf("got %s named, want a link no command can plant left alone", link)
	}
}

func TestAGrantMayNotPassThroughASymlinkACommandCanPlant(t *testing.T) {
	tree := plantALinkInAWritablePath(t)

	requireGrantRefused(t, tree.planted, []string{tree.writable}, "a link a command planted refused")
}

func TestAGrantMayNotEnterAWritablePathAndFollowWhatItFinds(t *testing.T) {
	tree := plantALinkInAWritablePath(t)
	entrance := makeLink(t, filepath.Join(tree.base, "entrance"), tree.planted)

	requireGrantRefused(
		t, entrance, []string{tree.writable},
		"a command's own link refused however it was reached",
	)
}

func TestAWritablePathReachedThroughASymlinkStillHoldsItsOwnBack(t *testing.T) {
	tree := plantALinkInAWritablePath(t)
	alias := makeLink(t, filepath.Join(tree.base, "alias"), tree.writable)

	requireGrantRefused(
		t, tree.planted, []string{alias},
		"a writable path named by its alias to bound the walk",
	)
}

func TestAGrantThatLoopsThroughItsOwnLinksIsRefused(t *testing.T) {
	base := settledDir(t)
	first := filepath.Join(base, "first")
	second := filepath.Join(base, "second")

	makeLink(t, first, second)
	makeLink(t, second, first)

	if opened, err := grantedPath(t, first, nil); err == nil {
		t.Errorf("got %s opened, want a loop refused", opened)
	}

	if _, redirects := FirstSymlinkBeneath(first, nil); redirects {
		t.Error("a loop through no writable path was named as one a command planted")
	}
}

func TestAPathThatNamesNoOnePlaceIsRefusedBeforeTheChildIsAskedToEnforceIt(t *testing.T) {
	for _, test := range []struct {
		name   string
		policy Policy
		want   string
	}{
		{
			name:   "a relative read grant",
			policy: Policy{Read: []string{"relative/path"}},
			want:   "not an absolute path",
		},
		{
			name:   "a relative scratch",
			policy: Policy{TmpDir: "scratch"},
			want:   "not an absolute path",
		},
		{
			name:   "a write grant carrying a null byte",
			policy: Policy{Write: []string{"/work\x00held"}},
			want:   "null byte",
		},
		{
			name:   "an exec grant that is not valid UTF-8",
			policy: Policy{Exec: []string{"/work/\xffheld"}},
			want:   "valid UTF-8",
		},
	} {
		err := test.policy.sane()
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: got %v, want a complaint mentioning %q", test.name, err, test.want)
		}
	}
}

func TestAGrantBeneathAPathAnotherSessionCanWriteNeverFollowsALinkPlantedThere(t *testing.T) {
	tree := plantALinkInAWritablePath(t)
	policy := Policy{Read: []string{tree.planted}, ParentWritable: []string{tree.writable}}

	if err := policy.grantPathsSafe(); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Errorf("got %v, want a link the parent planted refused", err)
	}
	if err := (Policy{Read: []string{tree.planted}}).grantPathsSafe(); err != nil {
		t.Errorf("got %v, want a link nobody else can write left alone", err)
	}
	if roots := (Policy{ParentWritable: []string{tree.writable}, ScratchParent: tree.base}).untrustedRoots(); !slices.Equal(roots, []string{tree.writable, tree.base}) {
		t.Errorf("got untrusted roots %v", roots)
	}
	if err := attachProtected(tree.planted, tree.planted, nil, []string{tree.writable}); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Errorf("got %v, want a mount through a planted link refused", err)
	}
}
