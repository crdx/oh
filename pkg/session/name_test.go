package session

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/pkg/agent"
)

func TestTheWordListsAreSortedLowercaseAndFreeOfDuplicates(t *testing.T) {
	lists := map[string][]string{"adjectives": adjectives, "animals": animals}

	for name, words := range lists {
		if !slices.IsSorted(words) {
			t.Errorf("the %s are not sorted", name)
		}
		if len(slices.Compact(slices.Clone(words))) != len(words) {
			t.Errorf("the %s contain duplicates", name)
		}
		for _, word := range words {
			if strings.Trim(word, "abcdefghijklmnopqrstuvwxyz") != "" {
				t.Errorf("the %s contain %q, which is not plain lowercase", name, word)
			}
		}
	}
}

func TestANewSessionIsNamedAfterAnAdjectiveAndAnAnimal(t *testing.T) {
	writer := storeSession(t, t.TempDir())

	adjective, animal, found := strings.Cut(writer.Name(), "-")
	if !found {
		t.Fatalf("the name %q is not two words", writer.Name())
	}
	if _, ok := slices.BinarySearch(adjectives, adjective); !ok {
		t.Errorf("%q is not one of the adjectives", adjective)
	}
	if _, ok := slices.BinarySearch(animals, animal); !ok {
		t.Errorf("%q is not one of the animals", animal)
	}
}

func TestTheNameIsTheBundleDirectory(t *testing.T) {
	directory := t.TempDir()
	writer := storeSession(t, directory)

	if _, err := os.Stat(journalPath(directory, writer.Name())); err != nil {
		t.Fatalf("no journal under the name %q: %v", writer.Name(), err)
	}

	storedSession, err := Read(directory, writer.Name())
	if err != nil {
		t.Fatal(err)
	}
	if storedSession.Name != writer.Name() {
		t.Errorf("read back the name %q, want %q", storedSession.Name, writer.Name())
	}
	if len(storedSession.Events) != 1 {
		t.Errorf("read back %d events, want 1", len(storedSession.Events))
	}
}

func TestTheIdentifierIsKeptButNamesNothing(t *testing.T) {
	directory := t.TempDir()
	writer := storeSession(t, directory)

	if len(writer.ID()) != idLength {
		t.Errorf("the identifier %q is %d characters, want %d", writer.ID(), len(writer.ID()), idLength)
	}
	if _, err := os.Stat(Dir(directory, writer.ID())); err == nil {
		t.Error("the identifier names a directory, and should name nothing")
	}

	storedSession, err := Read(directory, writer.Name())
	if err != nil {
		t.Fatal(err)
	}
	if storedSession.ID != writer.ID() {
		t.Errorf("read back the identifier %q, want %q", storedSession.ID, writer.ID())
	}
}

func TestATakenNameIsPassedOverForTheNextFreeOne(t *testing.T) {
	directory := t.TempDir()

	restoreWordsAfterwards(t)
	adjectives = []string{"brave", "brisk"}
	animals = []string{"otter"}

	first := storeSession(t, directory)
	second := storeSession(t, directory)

	names := []string{first.Name(), second.Name()}
	slices.Sort(names)
	if want := []string{"brave-otter", "brisk-otter"}; !slices.Equal(names, want) {
		t.Fatalf("the two sessions are named %v, want %v", names, want)
	}
}

func TestADirectoryIsTakenEvenWhenItsJournalCannotBeRead(t *testing.T) {
	directory := t.TempDir()

	restoreWordsAfterwards(t)
	adjectives = []string{"brave", "brisk"}
	animals = []string{"otter"}

	if err := os.Mkdir(filepath.Join(directory, "brave-otter"), 0o700); err != nil {
		t.Fatal(err)
	}

	if got := storeSession(t, directory).Name(); got != "brisk-otter" {
		t.Errorf("the session is named %q, want %q", got, "brisk-otter")
	}
}

func TestRunningOutOfNamesIsReportedRatherThanFudged(t *testing.T) {
	directory := t.TempDir()

	restoreWordsAfterwards(t)
	adjectives = []string{"brave"}
	animals = []string{"otter"}

	storeSession(t, directory)

	if _, err := Create(directory, nil, nil); err == nil {
		t.Error("expected an exhausted word list to be reported")
	}
}

func TestOnlyATwoWordNameIsAccepted(t *testing.T) {
	rejected := []string{
		"", ".", "..", "../somewhere-else", "brave-otter/..", "brave_otter", "Brave-Otter",
		"brave", "brave-otter-2", "034AyfXg0X3KMZ2R8pH56S", "brave-otter ",
	}

	for _, name := range rejected {
		if err := validateName(name); err == nil {
			t.Errorf("expected %q to be rejected as a session name", name)
		}
	}

	if err := validateName("brave-otter"); err != nil {
		t.Errorf("expected %q to be accepted as a session name: %v", "brave-otter", err)
	}
}

func TestASessionKeepsItsNameAcrossReadAndOpen(t *testing.T) {
	directory := t.TempDir()
	writer := storeSession(t, directory)

	reopened, err := Open(directory, writer.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()

	if reopened.Name() != writer.Name() {
		t.Errorf("reopened with the name %q, want %q", reopened.Name(), writer.Name())
	}
	if reopened.ID() != writer.ID() {
		t.Errorf("reopened with the identifier %q, want %q", reopened.ID(), writer.ID())
	}
}

func TestEntriesIdentifyEverySessionWithoutLoadingIt(t *testing.T) {
	directory := t.TempDir()
	first := storeSession(t, directory)
	second := storeSession(t, directory)

	if err := os.WriteFile(filepath.Join(directory, "stray-file"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "034AyfXg0X3KMZ2R8pH56S"), 0o700); err != nil {
		t.Fatal(err)
	}

	entries, err := Entries(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("found %d sessions, want 2", len(entries))
	}

	wanted := map[string]string{first.Name(): first.ID(), second.Name(): second.ID()}
	for _, entry := range entries {
		if entry.ID != wanted[entry.Name] {
			t.Errorf("%q carries the identifier %q, want %q", entry.Name, entry.ID, wanted[entry.Name])
		}
		if entry.StartedAt.IsZero() {
			t.Errorf("%q has no start time", entry.Name)
		}
	}
}

func TestEntriesAreOldestFirst(t *testing.T) {
	directory := t.TempDir()
	var stored []string
	for range 4 {
		stored = append(stored, storeSession(t, directory).Name())
	}

	entries, err := Entries(directory)
	if err != nil {
		t.Fatal(err)
	}

	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name)
	}
	if !slices.Equal(got, stored) {
		t.Errorf("listed %v, want %v", got, stored)
	}
}

func TestAnAbsentDirectoryHoldsNoSessions(t *testing.T) {
	entries, err := Entries(filepath.Join(t.TempDir(), "not-here"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("found %d sessions in a directory that does not exist", len(entries))
	}
}

func storeSession(t *testing.T, directory string) *Writer {
	t.Helper()

	writer, err := Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return writer
}

func restoreWordsAfterwards(t *testing.T) {
	t.Helper()

	savedAdjectives, savedAnimals := slices.Clone(adjectives), slices.Clone(animals)
	t.Cleanup(func() { adjectives, animals = savedAdjectives, savedAnimals })
}
