package picker

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/menu"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/util/strutil"
)

const (
	currentWorkspaceDir = "/home/alice/project"
	otherWorkspaceDir   = "/home/alice/another"
	archivedBytes       = 6_291_456
	restoredBytes       = 7_340_032
	sizesRoom           = 150
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func storedSessions() []*Session {
	now := time.Now()

	sessions := []*Session{
		{
			Name:         "chewy-sardine",
			Bytes:        1_258_291,
			Title:        "why does the spinner stutter when a tool runs",
			Model:        "Codex 5.3",
			ModelID:      "gpt-5.3-codex",
			Effort:       "high",
			MessageCount: 12,
			StartedAt:    now.Add(-8 * time.Minute),
			TouchedAt:    now,
			IsRunning:    true,
			IsFast:       true,
		},
		{
			Name:         "thick-poodle",
			Bytes:        188_416,
			Title:        "add support for reasoning traces",
			Model:        "Sonnet 5",
			ModelID:      "claude-sonnet-5",
			Effort:       "medium",
			MessageCount: 4,
			StartedAt:    now.Add(-127 * time.Minute),
			TouchedAt:    now.Add(-37 * time.Minute),
			IsFast:       true,
		},
		{
			Name:         "funny-badger",
			Bytes:        24_536_678,
			Model:        "Qwen Coder 3 30B Instruct",
			ModelID:      "qwen3-coder:30b-a3b-instruct",
			Effort:       "medium",
			IsFast:       true,
			Title:        "the cancelled turn leaves a tool call unanswered\nand the next request fails",
			MessageCount: 148,
			StartedAt:    now.Add(-8 * time.Hour),
			TouchedAt:    now.Add(-5 * time.Hour),
		},
		{
			Name:         "able-dolphin",
			MessageCount: 1,
			StartedAt:    now.Add(-77 * time.Hour),
			TouchedAt:    now.Add(-73 * time.Hour),
		},
		{
			Name:         "tame-impala",
			Bytes:        4_300_800,
			Title:        "rename the harness to oh",
			Model:        "Codex 5.3",
			ModelID:      "gpt-5.3-codex",
			MessageCount: 26,
			StartedAt:    now.Add(-330 * time.Hour),
			TouchedAt:    now.Add(-300 * time.Hour),
		},
	}
	for _, storedSession := range sessions {
		storedSession.WorkspaceDir = currentWorkspaceDir
	}

	return sessions
}

func archivedSessions() []*Session {
	now := time.Now()

	sessions := []*Session{
		{
			Name:         "tame-impala",
			Bytes:        831_488,
			Title:        "rename the harness to oh",
			Model:        "Codex 5.3",
			ModelID:      "gpt-5.3-codex",
			MessageCount: 26,
			StartedAt:    now.Add(-330 * time.Hour),
			TouchedAt:    now.Add(-300 * time.Hour),
			IsArchived:   true,
		},
		{
			Name:         "wiry-turtle",
			Bytes:        2_411_724,
			Title:        "add an archive view to the picker",
			Model:        "Sonnet 5",
			ModelID:      "claude-sonnet-5",
			Effort:       "high",
			MessageCount: 61,
			StartedAt:    now.Add(-52 * time.Hour),
			TouchedAt:    now.Add(-49 * time.Hour),
			IsArchived:   true,
		},
	}
	for _, storedSession := range sessions {
		storedSession.WorkspaceDir = currentWorkspaceDir
	}

	return sessions
}

func otherWorkspaceSession() *Session {
	storedSession := storedSessions()[1]
	storedSession.Name = "wiry-turtle"
	storedSession.WorkspaceDir = otherWorkspaceDir
	storedSession.IsOtherWorkspace = true
	storedSession.Title = "work in another project"
	return storedSession
}

func compareWithGolden(t *testing.T, name string, drawn string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)

	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(drawn), 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}

	if drawn != string(want) {
		t.Errorf("rows differ from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}

func TestGoldenTheRowsOfTheSessionPickerMatchTheGolden(t *testing.T) {
	sessions := &sessionList{store: Store{Sessions: storedSessions()}}

	var output strings.Builder

	for i, room := range []int{150, 120, roomForSize, roomForSize - 1, 46} {
		if i > 0 {
			_, _ = fmt.Fprintln(&output)
		}
		_, _ = fmt.Fprintf(&output, "--- %d columns ---\n", room)
		_, _ = fmt.Fprintln(&output, sessions.ColumnHeader(room))
		for rowIndex, storedSession := range sessions.rows() {
			_, _ = fmt.Fprintln(&output, row(storedSession, rowIndex == 1, room))
		}
	}

	compareWithGolden(t, "rows.golden", output.String())
}

func TestGoldenEverySizeASessionCanOccupyIsDrawnWithinItsColumn(t *testing.T) {
	now := time.Now()
	sizes := []int64{0, 1, megabyte - 1, megabyte, 1_572_863, 1_572_864, 11_010_048, 1_073_217_535, 1_073_217_536, gigabyte, 1_610_612_735, 1_610_612_736, 1 << 40}

	var output strings.Builder
	_, _ = fmt.Fprintln(&output, sessionTable("Agent", false).Header(sizesRoom))
	for _, bytes := range sizes {
		_, _ = fmt.Fprintln(&output, row(&Session{
			Name:         "thick-poodle",
			Title:        strconv.FormatInt(bytes, 10) + " bytes",
			MessageCount: 4,
			Bytes:        bytes,
			StartedAt:    now.Add(-time.Hour),
			TouchedAt:    now,
		}, false, sizesRoom))
	}

	compareWithGolden(t, "sizes.golden", output.String())
}

func TestGoldenWhatTheSessionPickerPaintsMatchesTheGolden(t *testing.T) {
	configuredTheme := style.DefaultTheme()
	configuredTheme.Accent = "#010203"

	frames := []struct {
		name     string
		room     int
		height   int
		cursor   int
		query    string
		keypress *key.Key
		read     func(*Session, int) ([]string, error)
		theme    *style.Theme

		isArchivedView      bool
		isAllWorkspacesView bool
		hasNothingArchived  bool
		hasOtherWorkspace   bool
		hasOtherArchive     bool
		movedIndex          *int
	}{
		{name: "a wide terminal, with room for the title", room: 150, height: 24, cursor: 1},
		{name: "a picker following a configured theme", room: 120, height: 24, cursor: 1, theme: &configuredTheme},
		{name: "a terminal wide enough for the model that answered", room: 120, height: 24, cursor: 1},
		{name: "no room for the model, so the room goes to the title", room: 80, height: 24, cursor: 3},
		{name: "a narrow terminal, with the columns clipped", room: 46, height: 24, cursor: 1},
		{name: "a filter narrowing the list to the model that answered", room: 120, height: 24, cursor: 0, query: "codex"},
		{name: "a filter matching the mode a session ran in", room: 120, height: 24, cursor: 0, query: "fast"},
		{name: "a filter no session answers to", room: 120, height: 24, cursor: 0, query: "kimi"},
		{name: "the confirmation asked before a session is archived", room: 120, height: 24, cursor: 1, keypress: new(archiveKeypress())},
		{name: "the confirmation in a narrow terminal", room: 46, height: 24, cursor: 1, keypress: new(archiveKeypress())},
		{name: "the confirmation taking the place of a filter being typed", room: 120, height: 24, cursor: 1, query: "codex", keypress: new(archiveKeypress())},
		{name: "a running session under the cursor, which is never offered for archiving", room: 120, height: 24, cursor: 0, query: "codex", keypress: new(archiveKeypress())},
		{name: "the archived view, switched to with left or right", room: 120, height: 24, cursor: 0, isArchivedView: true},
		{name: "all workspaces, switched to with tab", room: 150, height: 24, cursor: 1, isAllWorkspacesView: true, hasOtherWorkspace: true},
		{name: "another workspace selected without running italics", room: 150, height: 24, cursor: 5, isAllWorkspacesView: true, hasOtherWorkspace: true},
		{name: "archived sessions from all workspaces", room: 150, height: 24, cursor: 1, isArchivedView: true, isAllWorkspacesView: true, hasOtherArchive: true},
		{name: "another workspace's conversation read but not opened", room: 120, height: 12, cursor: 5, isAllWorkspacesView: true, hasOtherWorkspace: true, keypress: new(openKeypress()), read: reading()},
		{name: "the confirmation asked before an archived session is restored", room: 120, height: 24, cursor: 1, isArchivedView: true, keypress: new(archiveKeypress())},
		{name: "the archived view with nothing archived", room: 120, height: 24, cursor: 0, isArchivedView: true, hasNothingArchived: true},
		{name: "the confirmation asked before a session is deleted for good", room: 120, height: 24, cursor: 1, keypress: new(deleteKeypress())},
		{name: "the deletion confirmation clipped by a narrow terminal", room: 40, height: 24, cursor: 1, keypress: new(deleteKeypress())},
		{name: "deleting an archived session for good", room: 120, height: 24, cursor: 0, isArchivedView: true, keypress: new(deleteKeypress())},
		{name: "the whole conversation read", room: 120, height: 24, cursor: 1, keypress: new(openKeypress()), read: reading()},
		{name: "the conversation read with the terminal too short for it", room: 120, height: 8, cursor: 1, keypress: new(openKeypress()), read: reading()},
		{name: "a conversation read in a narrow terminal", room: 46, height: 12, cursor: 1, keypress: new(openKeypress()), read: reading()},
		{name: "a conversation that could not be read", room: 120, height: 12, cursor: 1, keypress: new(openKeypress()), read: unreadable()},
		{name: "an archived session, which is opened rather than read", room: 120, height: 24, cursor: 1, isArchivedView: true, keypress: new(openKeypress()), read: reading()},
		{name: "a session picker with nothing to read from", room: 120, height: 24, cursor: 1, keypress: new(openKeypress())},
		{name: "the conversation of a running session, which cannot be opened", room: 120, height: 12, cursor: 0, keypress: new(openKeypress()), read: reading()},
		{name: "a session archived here, drawn at the size of its archive", room: 120, height: 24, cursor: 0, isArchivedView: true, movedIndex: new(2)},
		{name: "a session restored here, drawn at the size of its directory", room: 120, height: 24, cursor: 0, movedIndex: new(1)},
	}

	var output strings.Builder

	for _, frame := range frames {
		paint := func(rows menu.List, room int, height int, cursor int, query string) string {
			if frame.keypress == nil {
				return menu.Paint(rows, room, height, cursor, query)
			}

			return menu.PaintAfterKey(rows, room, height, cursor, query, *frame.keypress)
		}

		archived := archivedSessions()
		if frame.hasNothingArchived {
			archived = nil
		}
		if frame.hasOtherArchive {
			otherArchive := archivedSessions()[0]
			otherArchive.Name = "calm-lemur"
			otherArchive.WorkspaceDir = otherWorkspaceDir
			otherArchive.IsOtherWorkspace = true
			archived = append(archived, otherArchive)
		}

		stored := storedSessions()
		if frame.hasOtherWorkspace {
			stored = append(stored, otherWorkspaceSession())
		}
		sessions := &sessionList{
			store: Store{
				Sessions:         stored,
				ArchivedSessions: archived,
				Archive:          archiving(),
				Restore:          restoring(),
				Delete:           deleting(),
				Read:             frame.read,
				Measure:          measurementsFor(stored, archived),
			},
		}
		if frame.movedIndex != nil {
			sessions.isArchivedView = !frame.isArchivedView
			move(t, sessions, *frame.movedIndex)
		}
		sessions.isArchivedView = frame.isArchivedView
		sessions.isAllWorkspacesView = frame.isAllWorkspacesView

		restoreTheme := func() {}
		if frame.theme != nil {
			restoreTheme = style.ApplyTheme(*frame.theme)
		}
		drawn := paint(
			sessions,
			frame.room,
			frame.height,
			frame.cursor,
			frame.query,
		)
		restoreTheme()

		fmt.Fprintf(&output, "=== %s ===\n%s\n", frame.name, strutil.VisibleEscapes(drawn))
	}

	compareWithGolden(t, "painted.ansi", output.String())
}

func measurementsFor(groups ...[]*Session) func(*Session) int64 {
	measurements := make(map[*Session]int64)
	for _, sessions := range groups {
		for _, storedSession := range sessions {
			measurements[storedSession] = storedSession.Bytes
			storedSession.Bytes = 0
		}
	}

	return func(storedSession *Session) int64 {
		return measurements[storedSession]
	}
}

func archiving() func(*Session) (int64, error) {
	return func(*Session) (int64, error) { return archivedBytes, nil }
}

func restoring() func(*Session) (int64, error) {
	return func(*Session) (int64, error) { return restoredBytes, nil }
}

func move(t *testing.T, sessions *sessionList, index int) {
	t.Helper()

	removal, isBound := sessions.Removal(index, archiveKeypress())
	if !isBound {
		t.Fatalf("expected row %d to be movable", index)
	}
	if err := removal.Perform(); err != nil {
		t.Fatal(err)
	}
	removal.Apply()
}

func deleting() func(*Session) error {
	return func(*Session) error { return nil }
}

func reading() func(*Session, int) ([]string, error) {
	return func(readSession *Session, room int) ([]string, error) {
		rows := []string{
			"reasoning about " + readSession.Name + " in a terminal " + strconv.Itoa(room) + " columns wide",
			"",
			"The prompt is drawn by layout, which wraps the buffer against the terminal",
			"width and reports where the cursor landed.",
			"",
			"read cmd/oh/line/render.go",
			"",
			"Nothing there needs changing to make it sticky: the work belongs in the",
			"output layer instead.",
		}

		return rows, nil
	}
}

func unreadable() func(*Session, int) ([]string, error) {
	return func(*Session, int) ([]string, error) {
		return nil, errors.New("the journal could not be read: no such file or directory")
	}
}
