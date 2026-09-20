package picker

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/internal/app/key"
	"crdx.org/io/internal/app/style"
)

func TestARunningSessionIsColouredWithoutAMarker(t *testing.T) {
	self := &sessionList{store: Store{Sessions: []*Session{{IsRunning: true, MessageCount: 40}, {}}}}
	got := self.Row(0, false, 80)
	want := style.RunningSession(row(self.at(0), false, 80))

	if got != want {
		t.Errorf("expected a running row, got %q", got)
	}
	if strings.Contains(got, "🟡") {
		t.Errorf("expected no running marker, got %q", got)
	}
}

func TestEveryConversationIsDrawnAlikeWhateverItHolds(t *testing.T) {
	self := &sessionList{store: Store{Sessions: []*Session{
		{Name: "able-dolphin", MessageCount: 1},
		{Name: "tame-impala", MessageCount: 40},
		{Name: "chewy-sardine", MessageCount: 1, IsRunning: true},
	}}}

	if got, want := self.Row(0, false, 80), style.Answer(row(self.at(0), false, 80)); got != want {
		t.Errorf("expected a short conversation to be drawn plainly, got %q", got)
	}
	if got, want := self.Row(1, false, 80), style.Answer(row(self.at(1), false, 80)); got != want {
		t.Errorf("expected a longer conversation to be drawn plainly, got %q", got)
	}
	if got, want := self.Row(0, true, 80), style.ChosenRow(row(self.at(0), true, 80)); got != want {
		t.Errorf("expected the chosen row to keep its own colour, got %q", got)
	}
	if got, want := self.Row(2, false, 80), style.RunningSession(row(self.at(2), false, 80)); got != want {
		t.Errorf("expected a running session to stay running whatever it holds, got %q", got)
	}
	if got, want := self.Row(2, true, 80), style.ChosenRunningSession(row(self.at(2), true, 80)); got != want {
		t.Errorf("expected the chosen row to take its colour even while running, got %q", got)
	}
}

func archiveKeypress() key.Key {
	return key.Key{Code: key.Rune, Value: 'a', Mod: key.Ctrl}
}

func deleteKeypress() key.Key {
	return key.Key{Code: key.Delete}
}

func openKeypress() key.Key {
	return key.Key{Code: key.Enter}
}

func TestASessionAnimalIncludesItsNameAndEmoji(t *testing.T) {
	got := sessionAnimal(&Session{Name: "chewy-sardine"})
	if got != "🐟 chewy-sardine" {
		t.Errorf("unexpected session animal: %q", got)
	}
}

func TestAnUntitledSessionDoesNotPutAnEscapeSequenceThroughTheClipper(t *testing.T) {
	got := sessionTitle(&Session{})

	if strings.ContainsRune(got, '\x1b') {
		t.Errorf("expected an unpainted title, got %q", got)
	}

	if style.Width(got) != len("(untitled)") {
		t.Errorf("expected the placeholder title, got %q", got)
	}
}

func TestOnlyASessionThatIsNotRunningCanBeArchived(t *testing.T) {
	var archived []string
	self := &sessionList{store: Store{
		Sessions: []*Session{{Name: "chewy-sardine", IsRunning: true}, {Name: "thick-poodle"}},
		Archive: func(storedSession *Session) error {
			archived = append(archived, storedSession.Name)
			return nil
		},
	}}

	if _, isBound := self.Removal(0, archiveKeypress()); isBound {
		t.Error("expected a running session to be left alone")
	}
	if got, isBound := self.Removal(1, archiveKeypress()); !isBound || got.Prompt != "Press ctrl+a again to archive thick-poodle" {
		t.Errorf("unexpected archival: %q and %t", got.Prompt, isBound)
	}

	removal, _ := self.Removal(1, archiveKeypress())
	if err := removal.Perform(); err != nil {
		t.Fatal(err)
	}
	removal.Apply()
	if len(archived) != 1 || archived[0] != "thick-poodle" {
		t.Errorf("got the sessions archived as %v", archived)
	}
	if self.Len() != 1 {
		t.Errorf("expected the archived session to leave the list, got %d rows", self.Len())
	}
}

func TestASessionPickerWithNowhereToArchiveToRemovesNothing(t *testing.T) {
	self := &sessionList{store: Store{Sessions: []*Session{{Name: "thick-poodle"}}}}

	if _, isBound := self.Removal(0, archiveKeypress()); isBound {
		t.Error("expected no session to be archivable without an archiver")
	}
}

func archivingStore() (*sessionList, *[]string) {
	var moved []string
	record := func(storedSession *Session) error {
		moved = append(moved, storedSession.Name)
		return nil
	}

	return &sessionList{store: Store{
		Sessions:         []*Session{{Name: "thick-poodle"}},
		ArchivedSessions: []*Session{{Name: "tame-impala", IsArchived: true}},
		Archive:          record,
		Restore:          record,
	}}, &moved
}

func TestTheViewSwitchesBetweenTheStoredSessionsAndTheArchivedOnes(t *testing.T) {
	self, _ := archivingStore()

	if got := self.at(0).Name; got != "thick-poodle" {
		t.Errorf("expected the stored sessions first, got %q", got)
	}
	if got := self.ColumnHeader(120); !strings.Contains(got, "Agent ") || strings.Contains(got, "archived") {
		t.Errorf("expected the stored header, got %q", got)
	}

	if !self.Switch(1) {
		t.Fatal("expected the view to switch")
	}
	if got := self.at(0).Name; got != "tame-impala" {
		t.Errorf("expected the archived sessions, got %q", got)
	}
	if got := self.ColumnHeader(120); !strings.Contains(got, "Agent (archived)") {
		t.Errorf("expected the archived header, got %q", got)
	}
	if got, _ := self.Removal(0, archiveKeypress()); got.Prompt != "Press ctrl+a again to restore tame-impala" {
		t.Errorf("unexpected prompt: %q", got.Prompt)
	}

	self.Switch(-1)
	if self.isArchivedView {
		t.Error("expected the stored sessions back")
	}
}

func TestARestoredSessionMovesBackIntoTheStoredView(t *testing.T) {
	self, moved := archivingStore()
	self.Switch(1)

	removal, _ := self.Removal(0, archiveKeypress())
	if err := removal.Perform(); err != nil {
		t.Fatal(err)
	}
	removal.Apply()
	if len(*moved) != 1 || (*moved)[0] != "tame-impala" {
		t.Fatalf("got the sessions moved as %v", *moved)
	}
	if self.Len() != 0 {
		t.Errorf("expected the archived view to be empty, got %d rows", self.Len())
	}

	self.Switch(-1)
	if self.Len() != 2 {
		t.Fatalf("expected the restored session among the stored ones, got %d", self.Len())
	}
	for index := range self.Len() {
		if self.at(index).IsArchived {
			t.Errorf("expected %s to be stored rather than archived", self.at(index).Name)
		}
	}
}

func TestAnArchivedSessionIsRestoredWhenItIsChosen(t *testing.T) {
	self, moved := archivingStore()
	self.Switch(1)

	chosenSession, err := self.chosen(0)
	if err != nil {
		t.Fatal(err)
	}
	if chosenSession.Name != "tame-impala" || chosenSession.IsArchived {
		t.Errorf("expected a restored session, got %+v", chosenSession)
	}
	if len(*moved) != 1 {
		t.Errorf("expected the session to have been restored once, got %v", *moved)
	}

	self.Switch(-1)
	if _, err := self.chosen(0); err != nil {
		t.Fatal(err)
	}
	if len(*moved) != 1 {
		t.Errorf("expected a stored session to be chosen without restoring, got %v", *moved)
	}
}

func TestAnArchivedSessionThatCannotBeRestoredIsNotChosen(t *testing.T) {
	self := &sessionList{store: Store{
		ArchivedSessions: []*Session{{Name: "tame-impala", IsArchived: true}},
		Restore:          func(*Session) error { return errors.New("the archive is unreadable") },
	}}
	self.Switch(1)

	if _, err := self.chosen(0); err == nil {
		t.Error("expected the failure to be reported")
	}
}

func TestDeletingTakesTheSessionOutOfWhicheverViewItIsIn(t *testing.T) {
	var deleted []string
	self := &sessionList{store: Store{
		Sessions:         []*Session{{Name: "thick-poodle"}, {Name: "funny-badger"}},
		ArchivedSessions: []*Session{{Name: "tame-impala", IsArchived: true}},
		Delete: func(storedSession *Session) error {
			deleted = append(deleted, storedSession.Name)
			return nil
		},
	}}

	removal, isBound := self.Removal(0, deleteKeypress())
	if !isBound {
		t.Fatal("expected a stored session to be deletable")
	}
	if removal.Prompt != "Press delete again to delete thick-poodle for good" {
		t.Errorf("unexpected prompt: %q", removal.Prompt)
	}
	if err := removal.Perform(); err != nil {
		t.Fatal(err)
	}
	removal.Apply()

	if self.Len() != 1 || self.at(0).Name != "funny-badger" {
		t.Errorf("expected the session to be gone, got %d rows", self.Len())
	}

	self.Switch(1)
	removal, isBound = self.Removal(0, deleteKeypress())
	if !isBound {
		t.Fatal("expected an archived session to be deletable")
	}
	if err := removal.Perform(); err != nil {
		t.Fatal(err)
	}
	removal.Apply()

	if self.Len() != 0 {
		t.Errorf("expected the archive to be empty, got %d rows", self.Len())
	}
	if !slices.Equal(deleted, []string{"thick-poodle", "tame-impala"}) {
		t.Errorf("got the sessions deleted as %v", deleted)
	}

	self.Switch(-1)
	if self.Len() != 1 {
		t.Errorf("expected the other view to be untouched, got %d rows", self.Len())
	}
}

func TestARunningSessionIsNeitherArchivedNorDeleted(t *testing.T) {
	self := &sessionList{store: Store{
		Sessions: []*Session{{Name: "chewy-sardine", IsRunning: true}},
		Archive:  func(*Session) error { return nil },
		Delete:   func(*Session) error { return nil },
	}}

	for name, keypress := range map[string]key.Key{
		"archive": archiveKeypress(),
		"delete":  deleteKeypress(),
	} {
		if _, isBound := self.Removal(0, keypress); isBound {
			t.Errorf("expected a running session not to be offered for %s", name)
		}
	}
}

func TestAnUnboundKeyAsksForNothing(t *testing.T) {
	self := &sessionList{store: Store{
		Sessions: []*Session{{Name: "thick-poodle"}},
		Archive:  func(*Session) error { return nil },
		Delete:   func(*Session) error { return nil },
	}}

	if _, isBound := self.Removal(0, key.Key{Code: key.Rune, Value: 'a'}); isBound {
		t.Error("expected a bare letter to be left to the filter")
	}
	if _, isBound := self.Removal(0, key.Key{Code: key.Enter}); isBound {
		t.Error("expected enter to be left alone")
	}
}

func TestARunningSessionIsReadButNeverOpened(t *testing.T) {
	self := &sessionList{store: Store{
		Sessions: []*Session{{Name: "chewy-sardine", IsRunning: true}, {Name: "thick-poodle"}},
		Read:     func(*Session, int) ([]string, error) { return []string{"a line"}, nil },
	}}

	if self.IsChoosable(0) {
		t.Error("expected a running session to stay closed to opening")
	}
	if !self.IsReachable(0) {
		t.Error("expected a running session to be reachable so it can be read")
	}
	if _, isBound := self.Preview(0, key.Key{Code: key.Enter}); !isBound {
		t.Error("expected a running session to be readable")
	}
}

func TestASessionCannotBeReadWithoutSomethingToReadItWith(t *testing.T) {
	self := &sessionList{store: Store{Sessions: []*Session{{Name: "chewy-sardine", IsRunning: true}}}}

	if self.IsReachable(0) {
		t.Error("expected a running session to be left alone when nothing can read it")
	}
	if _, isBound := self.Preview(0, key.Key{Code: key.Enter}); isBound {
		t.Error("expected no preview when nothing can read it")
	}
}

func TestAnArchivedSessionIsOpenedRatherThanRead(t *testing.T) {
	self := &sessionList{
		store: Store{
			ArchivedSessions: []*Session{{Name: "tame-impala", IsArchived: true}},
			Read:             func(*Session, int) ([]string, error) { return []string{"a line"}, nil },
		},
		isArchivedView: true,
	}

	if _, isBound := self.Preview(0, key.Key{Code: key.Enter}); isBound {
		t.Error("expected an archived session to be opened rather than read")
	}
	if !self.IsReachable(0) {
		t.Error("expected an archived session to stay reachable")
	}
}
