package gitStatus

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"crdx.org/oh/internal/app/gitrepo"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/style"
	"github.com/BurntSushi/toml"
)

const settleLimit = 10 * time.Second

type tomlOptions string

func (self tomlOptions) Read(into any) error {
	_, err := toml.Decode(string(self), into)

	return err
}

func isolateGit(t *testing.T) {
	t.Helper()

	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Kevin")
	t.Setenv("GIT_AUTHOR_EMAIL", "kevin@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Kevin")
	t.Setenv("GIT_COMMITTER_EMAIL", "kevin@example.com")
}

func repository(t *testing.T, script string) string {
	t.Helper()

	workspaceDir := t.TempDir()

	command := exec.CommandContext(t.Context(), "bash", "-euo", "pipefail", "-c", script) //nolint:gosec // the fixture's own setup
	command.Dir = workspaceDir

	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("failed to set up a repository: %s\n%s", err, output)
	}

	return workspaceDir
}

func build(t *testing.T, workspaceDir string, options string) *state {
	t.Helper()

	built, err := New(workspaceDir)(tomlOptions(options))
	if err != nil {
		t.Fatal(err)
	}

	instance, isState := built.(*state)
	if !isState {
		t.Fatalf("expected the segment's own state, got %T", built)
	}

	return instance
}

func settle(t *testing.T, instance *state) string {
	t.Helper()

	deadline := time.Now().Add(settleLimit)
	for time.Now().Before(deadline) {
		if drawn := style.Plain(instance.Render(segment.Context{})); drawn != "" {
			return drawn
		}
		time.Sleep(readingInterval)
	}

	t.Fatal("expected the status to be read in time")

	return ""
}

func TestParsingFindsNothingInACleanTree(t *testing.T) {
	got := parseStatus([]byte("# branch.oid abc\n# branch.head main\n"))

	if got != (tree{}) {
		t.Errorf("expected a clean tree, got %+v", got)
	}
}

func TestParsingCountsEveryKindOfChangeAsDirty(t *testing.T) {
	for _, line := range []string{
		"1 .M N... 100644 100644 100644 abc abc file",
		"2 R. N... 100644 100644 100644 abc abc R100 new\told",
		"? untracked",
	} {
		got := parseStatus([]byte(line + "\n"))

		if !got.isDirty || got.isConflicted {
			t.Errorf("expected %q to make the tree dirty and nothing more, got %+v", line, got)
		}
	}
}

func TestParsingMarksAnUnmergedPathAsConflicted(t *testing.T) {
	got := parseStatus([]byte("u UU N... 100644 100644 100644 100644 a b c file\n"))

	if !got.isConflicted || !got.isDirty {
		t.Errorf("expected an unmerged path to conflict, got %+v", got)
	}
}

func TestParsingIgnoresAnIgnoredPath(t *testing.T) {
	if got := parseStatus([]byte("! build/\n")); got.isDirty {
		t.Errorf("expected an ignored path to leave the tree clean, got %+v", got)
	}
}

func TestParsingReadsTheDivergence(t *testing.T) {
	got := parseStatus([]byte("# branch.upstream origin/main\n# branch.ab +3 -12\n"))

	if got.ahead != 3 || got.behind != 12 {
		t.Errorf("expected 3 ahead and 12 behind, got %+v", got)
	}
}

func TestDrawingNamesTheOperationOverAConflict(t *testing.T) {
	got := style.Plain(tree{isConflicted: true, operation: "merging", ahead: 1}.draw())

	if got != "● merging ↑1" {
		t.Errorf("expected the operation and the divergence, got %q", got)
	}
}

func TestDrawingSaysAConflictWithNoOperationIsConflicted(t *testing.T) {
	got := style.Plain(tree{isConflicted: true, isDirty: true}.draw())

	if got != "● conflicted" {
		t.Errorf("expected a bare conflict to be named, got %q", got)
	}
}

func TestDrawingShowsOnlyTheMarkForAQuietTree(t *testing.T) {
	if got := style.Plain(tree{isDirty: true}.draw()); got != "●" {
		t.Errorf("expected only the mark, got %q", got)
	}
}

func TestDrawingColoursTheMarkByTheTreeItDescribes(t *testing.T) {
	cases := map[string]struct {
		tree tree
		want string
	}{
		"clean":     {tree: tree{}, want: style.CleanTree(treeMark)},
		"dirty":     {tree: tree{isDirty: true}, want: style.DirtyTree(treeMark)},
		"conflict":  {tree: tree{isConflicted: true}, want: style.TroubledTree(treeMark)},
		"operation": {tree: tree{operation: "rebasing"}, want: style.TroubledTree(treeMark)},
	}

	for name, each := range cases {
		if got := each.tree.drawMark(); got != each.want {
			t.Errorf("%s: expected %q, got %q", name, each.want, got)
		}
	}
}

func TestEveryOperationIsFoundByItsMarker(t *testing.T) {
	cases := map[string]string{
		"rebase-merge": "rebasing",
		"rebase-apply": "rebasing",
		filepath.Join("rebase-apply", "applying"): "applying",
		"MERGE_HEAD":       "merging",
		"CHERRY_PICK_HEAD": "cherry-picking",
		"REVERT_HEAD":      "reverting",
	}

	for marker, want := range cases {
		gitDir := t.TempDir()
		path := filepath.Join(gitDir, marker)

		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}

		if got := operationIn(gitDir); got != want {
			t.Errorf("%s: expected %q, got %q", marker, want, got)
		}
	}

	if got := operationIn(t.TempDir()); got != "" {
		t.Errorf("expected no operation in an idle repository, got %q", got)
	}
}

func TestTheSegmentSaysNothingOutsideARepository(t *testing.T) {
	instance := build(t, t.TempDir(), "")

	if got := instance.Render(segment.Context{}); got != "" {
		t.Errorf("expected nothing, got %q", got)
	}

	if instance.isReading {
		t.Error("expected no git to run outside a repository")
	}
}

func TestTheSegmentReadsARealRepositoryOffTheDrawingThread(t *testing.T) {
	isolateGit(t)

	workspaceDir := repository(t, "git init -q -b main\ngit commit -q --allow-empty -m base\necho x > file\n")
	instance := build(t, workspaceDir, "")

	if got := instance.Render(segment.Context{}); got != "" {
		t.Errorf("expected nothing before the first reading lands, got %q", got)
	}

	if got := settle(t, instance); got != "●" {
		t.Errorf("expected the dirty mark, got %q", got)
	}

	if got := instance.Render(segment.Context{}); got != style.DirtyTree(treeMark) {
		t.Errorf("expected the mark in the dirty colour, got %q", got)
	}
}

func TestTheSegmentFindsARebaseInARealRepository(t *testing.T) {
	isolateGit(t)

	workspaceDir := repository(t, `
git init -q -b main
echo base > file
git add file
git commit -q -m base
git checkout -q -b other
echo other > file
git commit -q -am other
git checkout -q main
echo main > file
git commit -q -am main
! git rebase -q other > /dev/null 2>&1
`)

	if got := settle(t, build(t, workspaceDir, "")); got != "● rebasing" {
		t.Errorf("expected the rebase to be named, got %q", got)
	}
}

func TestTheSegmentSaysWhenGitFails(t *testing.T) {
	workspaceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspaceDir, ".git"), []byte("gitdir: /nowhere\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := settle(t, build(t, workspaceDir, "")); got != failureLabel {
		t.Errorf("expected the failure to be drawn, got %q", got)
	}
}

func TestTheSegmentAsksForARedrawOnceItHasRead(t *testing.T) {
	isolateGit(t)

	instance := build(t, repository(t, "git init -q -b main\n"), "rate = \"1h\"\n")
	at := time.Now()

	if got := instance.NextRefresh(segment.Phase{At: at}); !got.Equal(at) {
		t.Errorf("expected a status never read to be read at once, got %s", got.Sub(at))
	}

	instance.mutex.Lock()
	instance.isReading = true
	instance.mutex.Unlock()

	if got := instance.NextRefresh(segment.Phase{At: at}); !got.Equal(at.Add(readingInterval)) {
		t.Errorf("expected a reading in flight to be looked in on shortly, got %s", got.Sub(at))
	}

	instance.read(gitrepo.Dir(instance.workspaceDir))

	if got := instance.NextRefresh(segment.Phase{At: at}); !got.Equal(at) {
		t.Errorf("expected a fresh reading to be drawn at once, got %s", got.Sub(at))
	}

	instance.Render(segment.Context{})

	if got := instance.NextRefresh(segment.Phase{At: at}); got.Sub(at) < 59*time.Minute {
		t.Errorf("expected the rate to pace the next reading, got %s", got.Sub(at))
	}
}

func TestTheSegmentRefusesARateThatRunsBackwards(t *testing.T) {
	if _, err := New(t.TempDir())(tomlOptions("rate = \"-1s\"\n")); err == nil {
		t.Fatal("expected a negative rate to be refused")
	}
}
