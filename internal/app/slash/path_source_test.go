package slash_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/app/trigger"
)

func pathSourceFixture(t *testing.T, directory string) *slash.PathSource {
	t.Helper()

	registry := mustRegistry(t, mustSet(t, "/",
		slash.Command{Name: "grant", Run: commandHandler}.
			WithArguments("r", "rx", "rw", "rxw").
			WithPathArgumentAfter("r", "rx", "rw", "rxw"),
		slash.Command{Name: "open", Run: commandHandler}.WithArguments("workspace"),
	))
	return slash.NewPathSource(directory, func() slash.Registry { return registry })
}

func TestThePathSourceFindsAPathAfterAConfiguredArgument(t *testing.T) {
	source := pathSourceFixture(t, t.TempDir())

	for name, test := range map[string]struct {
		text      string
		cursor    int
		want      trigger.Word
		wantFound bool
	}{
		"an empty path": {
			text:      "/grant rx ",
			cursor:    10,
			want:      trigger.Word{Start: 10, End: 10},
			wantFound: true,
		},
		"a relative path": {
			text:      "/grant rx notes/one",
			cursor:    18,
			want:      trigger.Word{Start: 10, End: 19, Query: "notes/on"},
			wantFound: true,
		},
		"a path with spaces": {
			text:      "/grant rx meeting notes",
			cursor:    22,
			want:      trigger.Word{Start: 10, End: 23, Query: "meeting note"},
			wantFound: true,
		},
		"the cursor before the path": {text: "/grant rx notes", cursor: 7},
		"no space before the path":   {text: "/grant rx", cursor: 9},
		"an invalid first argument":  {text: "/grant z ", cursor: 9},
		"another command":            {text: "/open workspace", cursor: 15},
		"a second line":              {text: "/grant rx \nnotes", cursor: 16},
	} {
		t.Run(name, func(t *testing.T) {
			got, found := source.Find([]rune(test.text), test.cursor)
			if found != test.wantFound {
				t.Fatalf("found %t, want %t", found, test.wantFound)
			}
			if got != test.want {
				t.Errorf("got %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestThePathSourceListsRelativePathsAndContinuesThroughDirectories(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "alpha dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "apple.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "other.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "alpha dir", "inside.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	source := pathSourceFixture(t, directory)
	changes := make(chan struct{}, 2)
	source.Open(func() { changes <- struct{}{} })

	word := trigger.Word{Query: "a"}
	if got := source.Results(word, 10); got.Placeholder != "listing paths…" {
		t.Fatalf("got initial results %+v", got)
	}
	awaitPathSourceChange(t, changes)

	want := trigger.Results{
		Items: []trigger.Result{
			{Label: "alpha dir/", Text: "alpha dir/", IsOpenEnded: true},
			{Label: "apple.txt", Text: "apple.txt"},
		},
		Total:       2,
		Placeholder: "no matching paths",
	}
	if got := source.Results(word, 10); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	fuzzyWord := trigger.Word{Query: "adir"}
	wantFuzzy := []trigger.Result{{Label: "alpha dir/", Text: "alpha dir/", IsOpenEnded: true}}
	if got := source.Results(fuzzyWord, 10).Items; !reflect.DeepEqual(got, wantFuzzy) {
		t.Errorf("fuzzy match got %+v, want %+v", got, wantFuzzy)
	}

	word = trigger.Word{Query: "alpha dir/"}
	if got := source.Results(word, 10); got.Placeholder != "listing paths…" {
		t.Fatalf("got initial directory results %+v", got)
	}
	awaitPathSourceChange(t, changes)

	want = trigger.Results{
		Items:       []trigger.Result{{Label: "alpha dir/inside.txt", Text: "alpha dir/inside.txt"}},
		Total:       1,
		Placeholder: "no matching paths",
	}
	if got := source.Results(word, 10); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestThePathSourceCompletesAnAbsolutePath(t *testing.T) {
	parent := t.TempDir()
	directory := filepath.Join(parent, "reference")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}

	source := pathSourceFixture(t, t.TempDir())
	changes := make(chan struct{}, 1)
	source.Open(func() { changes <- struct{}{} })
	word := trigger.Word{Query: filepath.Join(parent, "ref")}
	source.Results(word, 10)
	awaitPathSourceChange(t, changes)

	path := directory + string(os.PathSeparator)
	want := []trigger.Result{{Label: path, Text: path, IsOpenEnded: true}}
	if got := source.Results(word, 10).Items; !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestThePathSourceCompletesAPathUnderTheUserHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Mkdir(filepath.Join(home, "Documents"), 0o700); err != nil {
		t.Fatal(err)
	}

	source := pathSourceFixture(t, t.TempDir())
	changes := make(chan struct{}, 1)
	source.Open(func() { changes <- struct{}{} })
	word := trigger.Word{Query: "~/Do"}
	source.Results(word, 10)
	awaitPathSourceChange(t, changes)

	want := []trigger.Result{{Label: "~/Documents/", Text: "~/Documents/", IsOpenEnded: true}}
	if got := source.Results(word, 10).Items; !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestThePathSourceLimitsItsPageButCountsEveryMatch(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(directory, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	source := pathSourceFixture(t, directory)
	changes := make(chan struct{}, 1)
	source.Open(func() { changes <- struct{}{} })
	source.Results(trigger.Word{}, 2)
	awaitPathSourceChange(t, changes)

	got := source.Results(trigger.Word{}, 2)
	if len(got.Items) != 2 || got.Total != 3 {
		t.Errorf("held %d of %d paths", len(got.Items), got.Total)
	}
}

func awaitPathSourceChange(t *testing.T, changes <-chan struct{}) {
	t.Helper()

	select {
	case <-changes:
	case <-time.After(5 * time.Second):
		t.Fatal("the path listing never arrived")
	}
}
