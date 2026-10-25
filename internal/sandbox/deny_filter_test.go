package sandbox

import (
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func referenceOutermostPaths(paths []string) []string {
	slices.SortFunc(paths, func(left string, right string) int {
		if difference := len(left) - len(right); difference != 0 {
			return difference
		}
		return strings.Compare(left, right)
	})
	paths = slices.Compact(paths)
	outerPaths := make([]string, 0, len(paths))
	for _, name := range paths {
		isCovered := false
		for _, earlier := range outerPaths {
			relative, err := filepath.Rel(earlier, name)
			if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				isCovered = true
				break
			}
		}
		if !isCovered {
			outerPaths = append(outerPaths, name)
		}
	}
	return outerPaths
}

func TestOutermostPathsMatchesOriginal(t *testing.T) {
	cases := [][]string{
		{"/a", "/ab", "/a-b", "/a/b", "/a/bc", "/a/b/c", "/a"},
		{"/", "/a", "/b/c"},
		{"/a/./b", "/a/b", "/a/b/c", "/a/c/../b"},
		{".", "..", "../..", "../a", "a", "a/b", "/a", ""},
	}
	random := rand.New(rand.NewPCG(42, 17)) //nolint:gosec // These compatibility cases need a repeatable sequence.
	parts := []string{"a", "ab", "a-b", "secret", ".", ".."}
	for range 1000 {
		var paths []string
		for range random.IntN(80) {
			var name strings.Builder
			if random.IntN(3) != 0 {
				name.WriteString("/")
			}
			for i := range 1 + random.IntN(6) {
				if i > 0 {
					name.WriteString("/")
				}
				name.WriteString(parts[random.IntN(len(parts))])
			}
			paths = append(paths, name.String())
		}
		cases = append(cases, paths)
	}
	for _, paths := range cases {
		got := outermostPaths(slices.Clone(paths))
		want := referenceOutermostPaths(slices.Clone(paths))
		if !slices.Equal(got, want) {
			t.Fatalf("paths %q: got %q, want %q", paths, got, want)
		}
	}
}

func FuzzOutermostPaths(f *testing.F) {
	f.Add("/a\n/ab\n/a/b\n/a-b\n/a/b/c")
	f.Add(".\n..\n../..\n../a\na\na/b\n/a\n")
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 4096 {
			t.Skip()
		}
		paths := strings.Split(input, "\n")
		got := outermostPaths(slices.Clone(paths))
		want := referenceOutermostPaths(slices.Clone(paths))
		if !slices.Equal(got, want) {
			t.Fatalf("paths %q: got %q, want %q", paths, got, want)
		}
	})
}

func TestOutermostPathsBroadTreeAllocationBudget(t *testing.T) {
	const size = 1000
	paths := make([]string, size)
	for i := range paths {
		paths[i] = fmt.Sprintf("/workspace/project-%06d/deep/secret", i)
	}
	allocations := testing.AllocsPerRun(1, func() {
		if got := outermostPaths(paths); len(got) != size {
			t.Fatalf("retained %d paths, want %d", len(got), size)
		}
	})
	if allocations > size*10 {
		t.Fatalf("broad-tree filtering allocated %.0f times; budget %d", allocations, size*10)
	}
}

func BenchmarkOutermostPaths(b *testing.B) {
	for _, size := range []int{100, 1000, 10000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			paths := make([]string, size)
			for i := range paths {
				paths[i] = fmt.Sprintf("/workspace/project-%06d/deep/auth.json", i)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				outermostPaths(paths)
			}
		})
	}
}
