package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/util/pathutil"

	"golang.org/x/sys/unix"
)

func FuzzAPathIsInsideARootOrItIsNot(fuzzer *testing.F) {
	for _, seed := range [][2]string{
		{"/work", "/work"},
		{"/work", "/work/held"},
		{"/work", "/workspace"},
		{"/work/", "/work/held"},
		{"/", "/anywhere"},
		{"/work", "/"},
		{"", "/work"},
		{"work", "work/held"},
		{"/work", "work/held"},
		{"/work", "/work/../elsewhere"},
		{"/tmp", "/tmp"},
	} {
		fuzzer.Add(seed[0], seed[1])
	}

	fuzzer.Fuzz(func(t *testing.T, root string, path string) {
		if len(root) > fuzzedPathLength || len(path) > fuzzedPathLength {
			t.Skip("a path longer than a policy is ever built with")
		}

		if !filepath.IsAbs(root) || !filepath.IsAbs(path) {
			policy := Policy{Read: []string{path}, Write: []string{root}}
			if policy.sane() == nil {
				t.Fatalf("granting %q within %q was accepted, and neither names one place", path, root)
			}
			return
		}

		_, isRelativeTo := pathutil.RelativeTo(root, path)
		isBeneath := isBeneathAny(path, []string{root})

		if isRelativeTo != isBeneath {
			t.Fatalf(
				"holding %q back within %q says %v and opening it says %v",
				path, root, isRelativeTo, isBeneath,
			)
		}
	})
}

const (
	fuzzedEntryCount     = 12
	fuzzedEntryWidth     = 4
	fuzzedComponentCount = 6
	honestStepBudget     = 64
)

var fuzzedNames = []string{"a", "b", "c", "d"}

var fuzzedEscapes = []string{"/etc", "/", "missing", "..", "../a", "a/../b", "./a"}

type builtTree struct {
	base        string
	directories []string
	entries     []string
}

func buildFuzzedTree(t *testing.T, layout []byte) builtTree {
	t.Helper()

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("could not settle where the tree is: %v", err)
	}

	tree := builtTree{base: base}
	tree.directories = append(tree.directories, tree.base)

	for len(layout) >= fuzzedEntryWidth {
		kind, name, parent, choice := layout[0], layout[1], layout[2], layout[3]
		layout = layout[fuzzedEntryWidth:]

		if len(tree.entries) >= fuzzedEntryCount {
			break
		}

		within := tree.directories[int(parent)%len(tree.directories)]
		path := filepath.Join(within, fuzzedNames[int(name)%len(fuzzedNames)])

		if pathutil.Exists(path) {
			continue
		}

		switch kind % 4 {
		case 0, 1:
			if err := os.Mkdir(path, 0o700); err != nil {
				continue
			}
			tree.directories = append(tree.directories, path)
		case 2:
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				continue
			}
		default:
			if err := os.Symlink(tree.target(within, choice), path); err != nil {
				continue
			}
		}

		tree.entries = append(tree.entries, path)
	}

	return tree
}

func (self builtTree) target(within string, choice byte) string {
	targets := append(append([]string{}, self.entries...), fuzzedEscapes...)
	target := targets[int(choice>>1)%len(targets)]

	if choice&1 == 0 || !filepath.IsAbs(target) {
		return target
	}

	relative, err := filepath.Rel(within, target)
	if err != nil {
		return target
	}

	return relative
}

func (self builtTree) roots(mask uint16) []string {
	var chosen []string

	for i, directory := range self.directories {
		if mask&(1<<i) != 0 {
			chosen = append(chosen, directory)
		}
	}

	return chosen
}

func (self builtTree) path(seed []byte) string {
	path := self.base

	for i, choice := range seed {
		if i >= fuzzedComponentCount {
			break
		}

		path = filepath.Join(path, fuzzedNames[int(choice)%len(fuzzedNames)])
	}

	return path
}

type honestWalk struct {
	realPath   string
	isPresent  bool
	isTainted  bool
	hasGivenUp bool
}

func honestComponents(path string) []string {
	var parts []string

	for part := range strings.SplitSeq(path, string(os.PathSeparator)) {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}

	return parts
}

func isWithinAny(path string, roots []string) bool {
	for _, root := range roots {
		if target, err := filepath.EvalSymlinks(root); err == nil {
			root = target
		}

		if path == root || strings.HasPrefix(path, root+string(os.PathSeparator)) {
			return true
		}
	}

	return false
}

func resolveHonestly(path string, roots []string) honestWalk {
	var walk honestWalk

	remaining := honestComponents(filepath.Clean(path))
	current := string(os.PathSeparator)

	for steps := 0; len(remaining) > 0; steps++ {
		if steps > honestStepBudget {
			walk.hasGivenUp = true
			return walk
		}

		part := remaining[0]
		remaining = remaining[1:]
		candidate := filepath.Join(current, part)

		info, err := os.Lstat(candidate)
		if err != nil {
			return walk
		}

		if info.Mode()&os.ModeSymlink == 0 {
			if len(remaining) > 0 && !info.IsDir() {
				return walk
			}

			current = candidate
			continue
		}

		if isWithinAny(current, roots) {
			walk.isTainted = true
		}

		target, err := os.Readlink(candidate)
		if err != nil {
			return walk
		}

		if filepath.IsAbs(target) {
			current = string(os.PathSeparator)
		}

		remaining = append(honestComponents(target), remaining...)
	}

	walk.isPresent = true
	walk.realPath = current

	return walk
}

func openedPath(t *testing.T, fd int) string {
	t.Helper()

	name, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
	if err != nil {
		t.Fatalf("could not say what was opened: %v", err)
	}

	return name
}

func FuzzOpeningAGrantNeverFollowsASymlinkACommandCouldPlant(fuzzer *testing.F) {
	for _, seed := range []struct {
		layout []byte
		path   []byte
		mask   uint16
	}{
		{layout: []byte{0, 0, 0, 0}, path: []byte{0}, mask: 0},
		{layout: []byte{0, 0, 0, 0, 3, 1, 1, 0}, path: []byte{0, 1}, mask: 2},
		{layout: []byte{0, 0, 0, 0, 3, 1, 1, 4}, path: []byte{0, 1}, mask: 3},
		{layout: []byte{0, 0, 0, 0, 0, 1, 1, 0, 3, 2, 2, 0}, path: []byte{0, 1, 2}, mask: 4},
		{layout: []byte{3, 0, 0, 2}, path: []byte{0}, mask: 1},
		{layout: []byte{0, 0, 0, 0, 3, 1, 0, 0, 0, 2, 2, 0}, path: []byte{1, 2}, mask: 0},
	} {
		fuzzer.Add(seed.layout, seed.path, seed.mask)
	}

	fuzzer.Fuzz(func(t *testing.T, layout []byte, pathSeed []byte, mask uint16) {
		if len(layout) > fuzzedEntryCount*fuzzedEntryWidth || len(pathSeed) > fuzzedComponentCount {
			t.Skip("a tree larger than the campaign means to explore")
		}

		tree := buildFuzzedTree(t, layout)
		roots := tree.roots(mask)
		path := tree.path(pathSeed)

		walk := resolveHonestly(path, roots)
		if walk.hasGivenUp {
			t.Skip("the path loops through its own symbolic links")
		}

		fd, err := openGrantPath(path, roots)
		if err == nil {
			defer func() { _ = unix.Close(fd) }()
		}

		link, redirects := FirstSymlinkBeneath(path, roots)

		if walk.isTainted {
			if err == nil {
				t.Fatalf(
					"granting %s under %v opened %s through a symbolic link a command could plant",
					path, roots, openedPath(t, fd),
				)
			}
			if !redirects {
				t.Fatalf("a policy granting %s under %v was accepted, and %q was not named", path, roots, link)
			}
			return
		}

		if redirects {
			t.Fatalf("a policy granting %s under %v was refused over %s, which no command can plant", path, roots, link)
		}

		if !walk.isPresent {
			if err == nil {
				t.Fatalf("granting %s under %v opened %s, which is not there", path, roots, openedPath(t, fd))
			}
			return
		}

		if err != nil {
			t.Fatalf("granting %s under %v was refused: %v", path, roots, err)
		}

		if opened := openedPath(t, fd); opened != walk.realPath {
			t.Fatalf("granting %s under %v opened %s, want %s", path, roots, opened, walk.realPath)
		}
	})
}
