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
	"crdx.org/oh/internal/util/strutil"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func storedSessions() []*Session {
	now := time.Now()

	return []*Session{
		{
			Name:         "chewy-sardine",
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
			Title:        "rename the harness to oh",
			Model:        "Codex 5.3",
			ModelID:      "gpt-5.3-codex",
			MessageCount: 26,
			StartedAt:    now.Add(-330 * time.Hour),
			TouchedAt:    now.Add(-300 * time.Hour),
		},
	}
}

func archivedSessions() []*Session {
	now := time.Now()

	return []*Session{
		{
			Name:         "tame-impala",
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

	for i, room := range []int{150, 120, 80, 46} {
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

func TestGoldenWhatTheSessionPickerPaintsMatchesTheGolden(t *testing.T) {
	frames := []struct {
		name     string
		room     int
		height   int
		cursor   int
		query    string
		keypress *key.Key
		read     func(*Session, int) ([]string, error)

		isArchivedView     bool
		hasNothingArchived bool
	}{
		{name: "a wide terminal, with room for the title", room: 150, height: 24, cursor: 1},
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
		{name: "the confirmation asked before an archived session is restored", room: 120, height: 24, cursor: 1, isArchivedView: true, keypress: new(archiveKeypress())},
		{name: "the archived view with nothing archived", room: 120, height: 24, cursor: 0, isArchivedView: true, hasNothingArchived: true},
		{name: "the confirmation asked before a session is deleted for good", room: 120, height: 24, cursor: 1, keypress: new(deleteKeypress())},
		{name: "the deletion confirmation clipped by a narrow terminal", room: 46, height: 24, cursor: 1, keypress: new(deleteKeypress())},
		{name: "deleting an archived session for good", room: 120, height: 24, cursor: 0, isArchivedView: true, keypress: new(deleteKeypress())},
		{name: "the whole conversation read", room: 120, height: 24, cursor: 1, keypress: new(openKeypress()), read: reading()},
		{name: "the conversation read with the terminal too short for it", room: 120, height: 8, cursor: 1, keypress: new(openKeypress()), read: reading()},
		{name: "a conversation read in a narrow terminal", room: 46, height: 12, cursor: 1, keypress: new(openKeypress()), read: reading()},
		{name: "a conversation that could not be read", room: 120, height: 12, cursor: 1, keypress: new(openKeypress()), read: unreadable()},
		{name: "an archived session, which is opened rather than read", room: 120, height: 24, cursor: 1, isArchivedView: true, keypress: new(openKeypress()), read: reading()},
		{name: "a session picker with nothing to read from", room: 120, height: 24, cursor: 1, keypress: new(openKeypress())},
		{name: "the conversation of a running session, which cannot be opened", room: 120, height: 12, cursor: 0, keypress: new(openKeypress()), read: reading()},
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

		fmt.Fprintf(&output, "=== %s ===\n%s\n", frame.name, strutil.VisibleEscapes(
			paint(
				&sessionList{
					store: Store{
						Sessions:         storedSessions(),
						ArchivedSessions: archived,
						Archive:          archiving(),
						Restore:          archiving(),
						Delete:           archiving(),
						Read:             frame.read,
					},
					isArchivedView: frame.isArchivedView,
				},
				frame.room,
				frame.height,
				frame.cursor,
				frame.query,
			),
		))
	}

	compareWithGolden(t, "painted.ansi", output.String())
}

func archiving() func(*Session) error {
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
