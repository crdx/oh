package link

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func FuzzPathMatchesAgreeWithTheWholeText(f *testing.F) {
	for _, seed := range []string{
		"",
		"see internal/app/link.go:12:4 and ~/notes.md then <s>/x",
		"a.b c/d\te\n./f ../g <s> x<s>/y foo=bar.go",
		"word.\u00a0next/one .hidden \v\fv.w",
		"no paths at all here",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, text string) {
		want := pathPattern.FindAllStringSubmatchIndex(text, -1)
		got := pathMatches(text)

		if len(want) != len(got) || !slices.EqualFunc(want, got, slices.Equal[[]int]) {
			t.Fatalf("pathMatches(%q) = %v, want %v", text, got, want)
		}
	})
}

func TestRememberingKeepsAnAnswerUntilItEnds(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "later.go")
	roots := Roots{Workspace: workspace}

	Remembering(func() {
		if got := Render("see later.go", roots); got != "see later.go" {
			t.Fatalf("expected no link before the file exists, got %q", got)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if got := Render("see later.go", roots); got != "see later.go" {
			t.Errorf("expected the remembered absence while remembering, got %q", got)
		}
	})

	if got := Render("see later.go", roots); !strings.Contains(got, openPrefix) {
		t.Errorf("expected a link once remembering ends, got %q", got)
	}
}

func TestRememberingNestsWithoutForgettingEarly(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "nested.go")
	roots := Roots{Workspace: workspace}

	Remembering(func() {
		Remembering(func() {
			Render("see nested.go", roots)
		})
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if got := Render("see nested.go", roots); got != "see nested.go" {
			t.Errorf("expected the outer remembering to outlive the inner one, got %q", got)
		}
	})
}

func BenchmarkRenderPathsInLongProse(benchmark *testing.B) {
	text := strings.Repeat("the change touches internal/app/link/link.go and some ordinary words that run on ", 30)
	roots := Roots{Workspace: benchmark.TempDir()}

	benchmark.ReportAllocs()
	for benchmark.Loop() {
		Render(text, roots)
	}
}

func BenchmarkRenderPathsInLongProseRemembering(benchmark *testing.B) {
	text := strings.Repeat("the change touches internal/app/link/link.go and some ordinary words that run on ", 30)
	roots := Roots{Workspace: benchmark.TempDir()}

	benchmark.ReportAllocs()
	Remembering(func() {
		for benchmark.Loop() {
			Render(text, roots)
		}
	})
}
