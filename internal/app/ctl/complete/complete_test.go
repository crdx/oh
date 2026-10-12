package complete

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestOnlyACompletionRequestIsAnsweredAtAll(t *testing.T) {
	var out strings.Builder

	if Write(&out, []string{"sessions"}) {
		t.Error("expected a command to be left to the command")
	}
	if Write(&out, []string{Flag}) {
		t.Error("expected a bare flag to ask for nothing")
	}
	if !Write(&out, []string{Flag, kindCommand}) {
		t.Error("expected a completion request to be answered")
	}
	if !strings.Contains(out.String(), "sessions\n") {
		t.Errorf("expected every command to be offered, got %q", out.String())
	}
}

func TestEveryCommandIsOffered(t *testing.T) {
	if got := completions(kindCommand, "mod", ""); !slices.Equal(got, []string{"models"}) {
		t.Errorf("got the commands %v", got)
	}
	if got := completions(kindCommand, "s", ""); !slices.Equal(got, []string{"sessions"}) {
		t.Errorf("got the commands %v", got)
	}
	if got := completions("nonsense", "", ""); got != nil {
		t.Errorf("expected nothing for an unknown kind, got %v", got)
	}
}

func TestEveryCommandOffersItsOwnOptions(t *testing.T) {
	got := completions(kindOption, "-", "sessions")
	if !slices.Contains(got, "-d") || !slices.Contains(got, "--archived") || !slices.Contains(got, "--delete") {
		t.Errorf("expected the sessions flags, got %v", got)
	}
	if got := completions(kindOption, "--del", "sessions"); !slices.Equal(got, []string{"--delete"}) {
		t.Errorf("expected the --delete flag, got %v", got)
	}
	if got := completions(kindOption, "-", "check"); !slices.Equal(got, []string{"--help", "-h"}) {
		t.Errorf("expected the check flags, got %v", got)
	}
	if got := completions(kindOption, "-", "nonsense"); got != nil {
		t.Errorf("expected nothing for an unknown command, got %v", got)
	}
}

func writeStoredJournal(t *testing.T, directory string, name string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(directory, name), 0o700); err != nil {
		t.Fatal(err)
	}

	head := fmt.Sprintf(`{"kind":"head","time":%q,"id":%q,"name":%q}`+"\n", "2026-01-01T00:00:00Z", name, name)
	if err := os.WriteFile(filepath.Join(directory, name, "session.jsonl"), []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestGoldenWhatEachKindOffersMatchesTheGolden(t *testing.T) {
	directory := t.TempDir()
	t.Setenv(location.StateDirVariable, directory)

	sessionsDir := location.GetSessionsDir()
	for _, name := range []string{"able-dolphin", "chewy-sardine", "tame-impala"} {
		writeStoredJournal(t, sessionsDir, name)
	}
	if err := session.Archive(sessionsDir, "tame-impala"); err != nil {
		t.Fatal(err)
	}

	requests := []struct {
		name string
		args []string
	}{
		{name: "commands", args: []string{Flag, kindCommand, ""}},
		{name: "commands beginning with a word", args: []string{Flag, kindCommand, "se"}},
		{name: "sessions", args: []string{Flag, kindSession, ""}},
		{name: "sessions beginning with a word", args: []string{Flag, kindSession, "able"}},
		{name: "deletable sessions", args: []string{Flag, kindDeletable, ""}},
		{name: "deletable sessions beginning with a word", args: []string{Flag, kindDeletable, "tame"}},
		{name: "sessions options beginning with a dash", args: []string{Flag, kindOption, "-", "sessions"}},
		{name: "sessions options beginning with a word", args: []string{Flag, kindOption, "--del", "sessions"}},
		{name: "check options beginning with a dash", args: []string{Flag, kindOption, "-", "check"}},
	}

	var drawn strings.Builder
	for _, request := range requests {
		fmt.Fprintf(&drawn, "=== %s ===\n", request.name)
		if !Write(&drawn, request.args) {
			t.Fatalf("%s completion request was not recognised", request.name)
		}
	}

	assertGolden(t, "completion.txt", drawn.String())
}

func assertGolden(t *testing.T, name string, drawn string) {
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
		t.Errorf("completions differ from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}

func TestAnArchivedSessionIsStillOfferedForDeletion(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv(location.StateDirVariable, stateDir)

	directory := location.GetSessionsDir()
	writeStoredJournal(t, directory, "tame-impala")
	if err := session.Archive(directory, "tame-impala"); err != nil {
		t.Fatal(err)
	}

	if got := completions(kindSession, "", ""); slices.Contains(got, "tame-impala") {
		t.Errorf("expected the archived session to be gone from the stored ones, got %v", got)
	}
	if got := completions(kindDeletable, "", ""); !slices.Contains(got, "tame-impala") {
		t.Errorf("expected the archived session to be offered for deletion, got %v", got)
	}
	if got := completions(kindDeletable, "tam", ""); !slices.Equal(got, []string{"tame-impala"}) {
		t.Errorf("expected the archived session to be matched by a prefix, got %v", got)
	}
}

func TestAStoredSessionCompletesUntilItIsArchived(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv(location.StateDirVariable, stateDir)

	directory := location.GetSessionsDir()

	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	name := writer.Name()
	if got := completions(kindSession, "", ""); !slices.Contains(got, name) {
		t.Errorf("expected the stored session to be offered, got %v", got)
	}

	if err := session.Archive(directory, name); err != nil {
		t.Fatal(err)
	}

	if got := completions(kindSession, "", ""); slices.Contains(got, name) {
		t.Errorf("expected the archived session to be gone from the stored ones, got %v", got)
	}
}
