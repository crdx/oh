package picker

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/style"
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
	if got, want := self.Row(0, true, 80), style.Answer(row(self.at(0), true, 80)); got != want {
		t.Errorf("expected the chosen row to leave its selection colour to the menu, got %q", got)
	}
	if got, want := self.Row(2, false, 80), style.RunningSession(row(self.at(2), false, 80)); got != want {
		t.Errorf("expected a running session to stay running whatever it holds, got %q", got)
	}
	if got, want := self.Row(2, true, 80), style.RunningSession(row(self.at(2), true, 80)); got != want {
		t.Errorf("expected a running row to keep its semantic style before the menu applies selection, got %q", got)
	}
}

func TestAChosenSessionFromAnotherWorkspaceLeavesSelectionColourToTheMenu(t *testing.T) {
	storedSession := &Session{Name: "wiry-turtle", IsOtherWorkspace: true}
	self := &sessionList{
		store:               Store{Sessions: []*Session{storedSession}},
		isAllWorkspacesView: true,
	}

	got := self.Row(0, true, 120)
	want := style.OtherWorkspaceSession.Over(rowInView(storedSession, true, 120, true))
	if got != want {
		t.Errorf("expected the workspace style before the menu applies selection, got %q", got)
	}
}

func archiveKeypress() key.Key {
	return key.Key{Code: key.Rune, Value: 'a'}
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
		Archive: func(storedSession *Session) (int64, error) {
			archived = append(archived, storedSession.Name)
			return 0, nil
		},
	}}

	if _, isBound := self.Removal(0, archiveKeypress()); isBound {
		t.Error("expected a running session to be left alone")
	}
	if got, isBound := self.Removal(1, archiveKeypress()); !isBound || got.Prompt != "Press a again to archive thick-poodle" {
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
	record := func(storedSession *Session) (int64, error) {
		moved = append(moved, storedSession.Name)
		return 0, nil
	}

	return &sessionList{store: Store{
		Sessions:         []*Session{{Name: "thick-poodle"}},
		ArchivedSessions: []*Session{{Name: "tame-impala", IsArchived: true}},
		Archive:          record,
		Restore:          record,
	}}, &moved
}

func TestTheFirstPopulatedViewOpensWhenThisWorkspaceHasNoSessions(t *testing.T) {
	self := &sessionList{store: Store{Sessions: []*Session{{
		Name:             "wiry-turtle",
		IsOtherWorkspace: true,
	}}}}

	self.openFirstPopulatedView()
	if !self.isAllWorkspacesView || self.isArchivedView || self.Len() != 1 {
		t.Errorf("expected all stored workspaces, got all=%t archived=%t rows=%d", self.isAllWorkspacesView, self.isArchivedView, self.Len())
	}
}

func TestTheArchivedViewOpensWhenItIsAllThisWorkspaceHas(t *testing.T) {
	self := &sessionList{store: Store{ArchivedSessions: []*Session{{Name: "tame-impala"}}}}

	self.openFirstPopulatedView()
	if self.isAllWorkspacesView || !self.isArchivedView || self.Len() != 1 {
		t.Errorf("expected this workspace's archives, got all=%t archived=%t rows=%d", self.isAllWorkspacesView, self.isArchivedView, self.Len())
	}
}

func TestTabSwitchesBetweenThisWorkspaceAndAllWorkspaces(t *testing.T) {
	self := &sessionList{store: Store{Sessions: []*Session{
		{Name: "thick-poodle"},
		{Name: "wiry-turtle", WorkspaceDir: "/home/alice/another", IsOtherWorkspace: true},
	}}}

	if self.Len() != 1 || self.at(0).Name != "thick-poodle" {
		t.Fatalf("expected only this workspace first, got %+v", self.rows())
	}
	if !self.SwitchScope() {
		t.Fatal("expected the scope to switch")
	}
	if self.Len() != 2 || !strings.Contains(self.ColumnHeader(120), "Workspace") {
		t.Fatalf("expected every workspace and its column, got %+v", self.rows())
	}
	if self.IsChoosable(1) {
		t.Error("expected another workspace's session not to be openable")
	}

	self.SwitchScope()
	if self.Len() != 1 {
		t.Errorf("expected this workspace again, got %d rows", self.Len())
	}
}

func TestAnotherWorkspacesSessionIsReadButNeverOpened(t *testing.T) {
	self := &sessionList{
		store: Store{
			Sessions: []*Session{{
				Name:             "wiry-turtle",
				WorkspaceDir:     "/home/alice/another",
				IsOtherWorkspace: true,
			}},
			Archive: func(*Session) (int64, error) { return 0, nil },
			Delete:  func(*Session) error { return nil },
			Read:    func(*Session, int) ([]string, error) { return []string{"a line"}, nil },
		},
		isAllWorkspacesView: true,
	}

	if !self.IsReachable(0) {
		t.Fatal("expected another workspace's session to be reachable for reading")
	}
	if self.IsChoosable(0) {
		t.Error("expected another workspace's session not to be openable")
	}
	if _, err := self.chosen(0); err == nil {
		t.Error("expected another workspace's session to refuse direct opening")
	}
	preview, isBound := self.Preview(0, key.Key{Code: key.Enter})
	if !isBound || preview.UnavailableLabel != "other workspace" ||
		preview.UnavailableMessage != "Session belongs to /home/alice/another" {
		t.Errorf("unexpected preview: %+v, bound %t", preview, isBound)
	}
}

func TestAnotherWorkspacesSessionIsArchivedRestoredAndDeleted(t *testing.T) {
	var moved []string
	record := func(storedSession *Session) (int64, error) {
		moved = append(moved, storedSession.Name)
		return 0, nil
	}
	self := &sessionList{
		store: Store{
			Sessions: []*Session{{Name: "wiry-turtle", IsOtherWorkspace: true}},
			Archive:  record,
			Restore:  record,
			Delete: func(storedSession *Session) error {
				moved = append(moved, storedSession.Name)
				return nil
			},
		},
		isAllWorkspacesView: true,
	}

	perform := func(keypress key.Key, wantPrompt string) {
		t.Helper()
		removal, isBound := self.Removal(0, keypress)
		if !isBound || removal.Prompt != wantPrompt {
			t.Fatalf("got %q and %t, want %q", removal.Prompt, isBound, wantPrompt)
		}
		if err := removal.Perform(); err != nil {
			t.Fatal(err)
		}
		removal.Apply()
	}

	perform(archiveKeypress(), "Press a again to archive wiry-turtle")
	self.Switch(1)
	perform(archiveKeypress(), "Press a again to restore wiry-turtle")
	self.Switch(-1)
	perform(deleteKeypress(), "Press delete again to delete wiry-turtle")

	if !slices.Equal(moved, []string{"wiry-turtle", "wiry-turtle", "wiry-turtle"}) {
		t.Errorf("got the sessions moved as %v", moved)
	}
	if self.Len() != 0 || len(self.store.ArchivedSessions) != 0 {
		t.Errorf("expected the session to be gone, got %d stored and %d archived", self.Len(), len(self.store.ArchivedSessions))
	}
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
	if got, _ := self.Removal(0, archiveKeypress()); got.Prompt != "Press a again to restore tame-impala" {
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
		Restore:          func(*Session) (int64, error) { return 0, errors.New("the archive is unreadable") },
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
	if removal.Prompt != "Press delete again to delete thick-poodle" {
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
		Archive:  func(*Session) (int64, error) { return 0, nil },
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
		Archive:  func(*Session) (int64, error) { return 0, nil },
		Delete:   func(*Session) error { return nil },
	}}

	if _, isBound := self.Removal(0, key.Key{Code: key.Rune, Value: 'x'}); isBound {
		t.Error("expected an unbound letter to be left alone")
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

func TestAnArchivedSessionIsReadBeforeItIsRestored(t *testing.T) {
	self := &sessionList{
		store: Store{
			ArchivedSessions: []*Session{{Name: "tame-impala", IsArchived: true}},
			Read:             func(*Session, int) ([]string, error) { return []string{"a line"}, nil },
		},
		isArchivedView: true,
	}

	preview, isBound := self.Preview(0, key.Key{Code: key.Enter})
	if !isBound {
		t.Fatal("expected an archived session to be read")
	}
	if !preview.RequiresRestoration {
		t.Error("expected an archived session preview to require restoration")
	}
	rows, err := preview.Read(80)
	if err != nil || !slices.Equal(rows, []string{"a line"}) {
		t.Errorf("expected the archived conversation, got %q and %v", rows, err)
	}
	if !self.store.ArchivedSessions[0].IsArchived {
		t.Error("expected reading an archived session to leave it archived")
	}
	if !self.IsReachable(0) {
		t.Error("expected an archived session to stay reachable")
	}
}

func TestASessionMovedBetweenViewsTakesTheSizeItNowOccupies(t *testing.T) {
	self := &sessionList{store: Store{
		Sessions:         []*Session{{Name: "thick-poodle", Bytes: 4 << 20}},
		ArchivedSessions: []*Session{},
		Archive:          func(*Session) (int64, error) { return 1 << 20, nil },
		Restore:          func(*Session) (int64, error) { return 4 << 20, nil },
	}}

	removal, _ := self.Removal(0, archiveKeypress())
	if err := removal.Perform(); err != nil {
		t.Fatal(err)
	}
	removal.Apply()
	self.Switch(1)

	if got := self.at(0).Bytes; got != 1<<20 {
		t.Errorf("expected the archive's size, got %d bytes", got)
	}
	if got := self.Row(0, false, 120); !strings.Contains(got, "1M") {
		t.Errorf("expected the archive's size to be drawn, got %q", got)
	}

	removal, _ = self.Removal(0, archiveKeypress())
	if err := removal.Perform(); err != nil {
		t.Fatal(err)
	}
	removal.Apply()
	self.Switch(-1)

	if got := self.at(0).Bytes; got != 4<<20 {
		t.Errorf("expected the restored directory's size, got %d bytes", got)
	}
}

func TestASessionWhoseSizeIsUnknownIsDrawnWithADash(t *testing.T) {
	if got := FormatSize(0); got != unknownSize {
		t.Errorf("got %q", got)
	}
}

func TestOnlyASizeThatIsDrawnIsMeasured(t *testing.T) {
	measured := 0
	first := &Session{Name: "thick-poodle"}
	second := &Session{Name: "tame-impala"}
	self := &sessionList{store: Store{
		Sessions: []*Session{first, second},
		Measure: func(*Session) int64 {
			measured++
			return 2 << 20
		},
	}}

	self.Row(0, false, roomForSize-1)
	self.Row(0, false, roomForSize)
	self.Row(0, false, roomForSize)

	if measured != 1 {
		t.Errorf("measured %d sessions, want only the one drawn with its size", measured)
	}
	if first.Bytes != 2<<20 {
		t.Errorf("first session has %d bytes after measurement", first.Bytes)
	}
	if second.Bytes != 0 {
		t.Errorf("second session was measured before it was drawn: %d bytes", second.Bytes)
	}
}

func TestAnUnavailableSizeIsMeasuredOnlyOnce(t *testing.T) {
	measured := 0
	self := &sessionList{store: Store{
		Sessions: []*Session{{Name: "thick-poodle"}},
		Measure: func(*Session) int64 {
			measured++
			return 0
		},
	}}

	self.Row(0, false, roomForSize)
	self.Row(0, false, roomForSize)

	if measured != 1 {
		t.Errorf("attempted the measurement %d times", measured)
	}
}

func TestWorkspaceRowsAreReusedUntilTheListingChanges(t *testing.T) {
	first := &Session{Name: "first"}
	other := &Session{Name: "other", IsOtherWorkspace: true}
	second := &Session{Name: "second"}
	self := &sessionList{store: Store{Sessions: []*Session{first, other, second}}}

	initial := self.rows()
	if len(initial) != 2 || initial[0] != first || initial[1] != second {
		t.Fatalf("wrong workspace sessions: %v", initial)
	}
	if &self.rows()[0] != &initial[0] {
		t.Fatal("the unchanged workspace was filtered again")
	}

	self.forget(0)
	if rows := self.rows(); len(rows) != 1 || rows[0] != second {
		t.Fatalf("deleting a session kept the old workspace rows: %v", rows)
	}
	self.SwitchScope()
	if rows := self.rows(); len(rows) != 2 || rows[0] != other {
		t.Fatalf("switching scope hid a session: %v", rows)
	}
}
