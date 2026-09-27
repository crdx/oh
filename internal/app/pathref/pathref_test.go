package pathref_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"crdx.org/oh/internal/app/dropdown"
	"crdx.org/oh/internal/app/pathref"
	"crdx.org/oh/internal/app/trigger"
)

func TestAListingHoldsEveryDirectoryOnce(t *testing.T) {
	listing := pathref.NewListing([]string{"a/b/c.go", "a/b/d.go", "e.go"})

	want := []string{"a/b/c.go", "a/b/", "a/", "a/b/d.go", "e.go"}
	if !reflect.DeepEqual(listing.Paths, want) {
		t.Errorf("got %q, want %q", listing.Paths, want)
	}
}

func TestScoreNeedsEveryCharacterInOrder(t *testing.T) {
	if _, isMatch := pathref.Score("internal/app/harness/app.go", "harapp"); !isMatch {
		t.Error("a subsequence did not match")
	}
	if _, isMatch := pathref.Score("internal/app/harness/app.go", "ppah"); isMatch {
		t.Error("characters out of order matched")
	}
	if _, isMatch := pathref.Score("README.md", "readme"); !isMatch {
		t.Error("a lowercase query did not match an uppercase path")
	}
	if _, isMatch := pathref.Score("readme.md", "README"); isMatch {
		t.Error("an uppercase query matched a lowercase path")
	}
}

func TestMatchRanksTheBestMatchFirst(t *testing.T) {
	listing := pathref.NewListing([]string{
		"internal/app/harness/main_test.go",
		"internal/app/harness/app.go",
		"internal/app/happy/pancakes.go",
		"cmd/weather/main.go",
	})

	for query, want := range map[string]string{
		"harapp": "internal/app/harness/app.go",
		"main":   "cmd/weather/main.go",
		"weat":   "cmd/weather/",
		"":       "cmd/",
	} {
		var matcher pathref.Matcher
		got, total := matcher.Match(listing, query, 10)
		if total == 0 || got[0] != want {
			t.Errorf("best match for %q is %q of %d, want %q", query, got, total, want)
		}
	}
}

func TestMatchListsADirectorysChildrenBeforeItsDescendants(t *testing.T) {
	listing := pathref.NewListing([]string{"internal/app/harness/app.go", "internal/app/edit.go"})

	var matcher pathref.Matcher
	got, _ := matcher.Match(listing, "internal/app/", 10)

	want := []string{"internal/app/edit.go", "internal/app/harness/", "internal/app/harness/app.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMatchNarrowsTheSameWayItSearchesAfresh(t *testing.T) {
	listing := syntheticListing(2000)

	var narrowing pathref.Matcher
	for _, query := range []string{"", "p", "pk", "pkg", "pkg/", "pkg/7", "pkg/", "Pkg"} {
		got, gotTotal := narrowing.Match(listing, query, 50)

		var fresh pathref.Matcher
		want, wantTotal := fresh.Match(listing, query, 50)
		if gotTotal != wantTotal || !reflect.DeepEqual(got, want) {
			t.Errorf("narrowing to %q found %d, fresh search found %d", query, gotTotal, wantTotal)
		}
	}
}

func TestMatchReportsEveryMatchBeyondItsLimit(t *testing.T) {
	listing := syntheticListing(300)

	var matcher pathref.Matcher
	got, total := matcher.Match(listing, "", 5)
	if len(got) != 5 || total != len(listing.Paths) {
		t.Errorf("got %d of %d, want 5 of %d", len(got), total, len(listing.Paths))
	}

	var unlimited pathref.Matcher
	all, _ := unlimited.Match(listing, "", len(listing.Paths))
	if !slices.Equal(got, all[:5]) {
		t.Errorf("limited best %q differ from the head of the full ranking %q", got, all[:5])
	}
}

func TestListFilesHonoursIgnoresAndExclusions(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{".gitignore", "kept.go", "ignored.log", "secrets.yml", "nested/deep.go", ".hidden"} {
		full := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		content := ""
		if name == ".gitignore" {
			content = "*.log\n"
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	files, isTruncated, err := pathref.ListFiles(context.Background(), directory, []string{"secrets.yml"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if isTruncated {
		t.Error("a small listing was truncated")
	}

	slices.Sort(files)
	want := []string{".gitignore", ".hidden", "kept.go", "nested/deep.go"}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("got %q, want %q", files, want)
	}
}

func TestAnIndexListsOnceUntilItGoesStale(t *testing.T) {
	now := time.Unix(0, 0)
	lists := 0
	index := pathref.NewIndexWith("/nowhere", nil, func(context.Context, string, []string) ([]string, bool, error) {
		lists++
		return []string{"a.go"}, false, nil
	}, func() time.Time { return now })

	changes := make(chan struct{}, 2)
	changed := func() { changes <- struct{}{} }

	index.Refresh(changed)
	<-changes
	index.Refresh(changed)
	if lists != 1 {
		t.Errorf("listed %d times before going stale", lists)
	}
	if got := index.Listing().Paths; !reflect.DeepEqual(got, []string{"a.go"}) {
		t.Errorf("got %q", got)
	}

	now = now.Add(time.Minute)
	index.Refresh(changed)
	<-changes
	if lists != 2 {
		t.Errorf("listed %d times after going stale", lists)
	}
}

func TestASourceSaysItIsListingUntilTheListingArrives(t *testing.T) {
	release := make(chan struct{})
	source := pathref.NewSource(pathref.NewIndexWith("/nowhere", nil, func(context.Context, string, []string) ([]string, bool, error) {
		<-release
		return []string{"cmd/main.go"}, false, nil
	}, time.Now))

	changes := make(chan struct{}, 1)
	source.Open(func() { changes <- struct{}{} })
	if got := source.Results(trigger.Word{}, 10); got.Placeholder != "listing files…" || len(got.Items) != 0 {
		t.Errorf("got %+v before the listing arrived", got)
	}

	close(release)
	<-changes
	want := trigger.Results{
		Items: []trigger.Result{
			{Label: "cmd/", Text: "@cmd/", IsOpenEnded: true},
			{Label: "cmd/main.go", Text: "@cmd/main.go"},
		},
		Total:       2,
		Placeholder: "no matching paths",
	}
	if got := source.Results(trigger.Word{}, 10); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if source.Symbol() != '@' || source.Elision() != dropdown.ElideStart {
		t.Errorf("the source answers to %q and elides with %v", source.Symbol(), source.Elision())
	}
}

func TestASourceNamesAFailedListing(t *testing.T) {
	source := pathref.NewSource(pathref.NewIndexWith("/nowhere", nil, func(context.Context, string, []string) ([]string, bool, error) {
		return nil, false, errors.New("rg: not found")
	}, time.Now))

	changes := make(chan struct{}, 1)
	source.Open(func() { changes <- struct{}{} })
	<-changes

	if got := source.Results(trigger.Word{Query: "x"}, 10).Placeholder; got != "could not list files: rg: not found" {
		t.Errorf("got placeholder %q", got)
	}
}

func syntheticListing(count int) *pathref.Listing {
	files := make([]string, count)
	for i := range files {
		files[i] = fmt.Sprintf("pkg/%d/section%d/file_%d.go", i%37, i%11, i)
	}

	return pathref.NewListing(files)
}

func BenchmarkMatchAFreshQuery(b *testing.B) {
	listing := syntheticListing(pathref.MaxPaths)

	for b.Loop() {
		var matcher pathref.Matcher
		matcher.Match(listing, "sec3fil", 200)
	}
}

func BenchmarkMatchAnEmptyQuery(b *testing.B) {
	listing := syntheticListing(pathref.MaxPaths)

	for b.Loop() {
		var matcher pathref.Matcher
		matcher.Match(listing, "", 200)
	}
}

func TestASourceQuotesAPathWithASpace(t *testing.T) {
	source := pathref.NewSource(pathref.NewIndexWith("/nowhere", nil, func(context.Context, string, []string) ([]string, bool, error) {
		return []string{"my notes/a b.md"}, false, nil
	}, time.Now))

	changes := make(chan struct{}, 1)
	source.Open(func() { changes <- struct{}{} })
	<-changes

	want := []trigger.Result{
		{Label: "my notes/", Text: `@"my notes/`, IsOpenEnded: true},
		{Label: "my notes/a b.md", Text: `@"my notes/a b.md"`},
	}
	if got := source.Results(trigger.Word{Query: "my"}, 10).Items; !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestASourceFindsAQuotedPath(t *testing.T) {
	source := pathref.NewSource(pathref.NewIndex("/nowhere", nil))

	want := trigger.Word{Start: 4, End: 14, Query: "my no", IsQuoted: true}
	if got, isFound := source.Find([]rune(`see @"my notes`), 11); !isFound || got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
