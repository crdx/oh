package session_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"crdx.org/oh/pkg/session"
)

func listingFixture(t *testing.T) string {
	t.Helper()

	directory := t.TempDir()
	for _, name := range []string{"zany-zebra", "able-ant", "Not-A-Name", "odd-owl.tgz"} {
		if err := os.Mkdir(filepath.Join(directory, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"tidy-tiger.tgz", "bold-bee.tgz", "Bad-Name.tgz", "loose-file", "calm-cat.tar"} {
		if err := os.WriteFile(filepath.Join(directory, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return directory
}

func TestSessionsAreListedByHowTheyAreKept(t *testing.T) {
	directory := listingFixture(t)

	stored, err := session.StoredNames(directory)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"able-ant", "zany-zebra"}; !slices.Equal(stored, want) {
		t.Errorf("got stored %v, want %v", stored, want)
	}

	archived, err := session.ArchivedNames(directory)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"bold-bee", "tidy-tiger"}; !slices.Equal(archived, want) {
		t.Errorf("got archived %v, want %v", archived, want)
	}

	all, err := session.AllNames(directory)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"able-ant", "zany-zebra", "bold-bee", "tidy-tiger"}; !slices.Equal(all, want) {
		t.Errorf("got every name %v, want the stored then the archived %v", all, want)
	}
}

func TestAMissingSessionsDirectoryListsNothing(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "absent")

	for name, list := range map[string]func(string) ([]string, error){
		"stored":   session.StoredNames,
		"archived": session.ArchivedNames,
		"all":      session.AllNames,
	} {
		names, err := list(directory)
		if err != nil || len(names) != 0 {
			t.Errorf("%s: got %v and %v, want nothing", name, names, err)
		}
	}

	entries, err := session.Entries(directory)
	if err != nil || len(entries) != 0 {
		t.Errorf("entries: got %v and %v, want nothing", entries, err)
	}
}

func TestASessionsPathThatIsAFileIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	for name, list := range map[string]func(string) ([]string, error){
		"stored":   session.StoredNames,
		"archived": session.ArchivedNames,
		"all":      session.AllNames,
	} {
		if _, err := list(path); err == nil {
			t.Errorf("%s: expected an error listing a file", name)
		}
	}
	if _, err := session.Entries(path); err == nil {
		t.Error("entries: expected an error listing a file")
	}
}

func TestListingNamesBothKindsAtOnce(t *testing.T) {
	directory := listingFixture(t)

	stored, archived, err := session.ListNames(directory)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"able-ant", "zany-zebra"}; !slices.Equal(stored, want) {
		t.Errorf("got stored %v, want %v", stored, want)
	}
	if want := []string{"bold-bee", "tidy-tiger"}; !slices.Equal(archived, want) {
		t.Errorf("got archived %v, want %v", archived, want)
	}
}
