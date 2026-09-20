package sessions

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/session"

	"crdx.org/io/internal/app/model"
	"crdx.org/io/internal/app/store"
	"crdx.org/io/internal/app/work"
)

var updateGoldens = flag.Bool("update", false, "update golden files")

func TestLoadingSessionsIdentifiesThoseThatAreRunning(t *testing.T) {
	directory := t.TempDir()
	writer, err := store.Create(directory, store.Meta{WorkspaceDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "begin"}); err != nil {
		t.Fatal(err)
	}

	loadedSessions, err := Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedSessions) != 1 || !loadedSessions[0].IsRunning {
		t.Fatalf("expected one running session, got %+v", loadedSessions)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	loadedSessions, err = Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedSessions) != 1 || loadedSessions[0].IsRunning {
		t.Errorf("expected one stopped session, got %+v", loadedSessions)
	}
}

func TestFastModeIsListedBesideTheRestOfTheSelection(t *testing.T) {
	directory := t.TempDir()
	writeIdleSession(t, directory, store.Meta{
		Provider: model.CodexProvider,
		Model:    "gpt-5.6-sol",
		Effort:   "high",
		IsFast:   true,
	})

	loadedSessions, err := Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedSessions) != 1 || !loadedSessions[0].IsFast {
		t.Errorf("got %+v", loadedSessions)
	}
}

func TestAListingWrittenBeforeItHeldFastModeRecoversItFromTheJournal(t *testing.T) {
	directory := t.TempDir()
	writer, err := store.Create(directory, store.Meta{
		Provider: model.CodexProvider,
		Model:    "gpt-5.6-sol",
		Effort:   "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(model.FastModeEvent(true)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "begin"}); err != nil {
		t.Fatal(err)
	}
	name := writer.Name()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	loadedSessions, err := Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedSessions) != 1 || loadedSessions[0].IsFast {
		t.Fatalf("expected a listing without the flag to say nothing of fast mode, got %+v", loadedSessions)
	}

	if err := store.RebuildMeta(directory, name); err != nil {
		t.Fatal(err)
	}

	loadedSessions, err = Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedSessions) != 1 || !loadedSessions[0].IsFast {
		t.Errorf("expected the rebuilt listing to recover fast mode, got %+v", loadedSessions)
	}
}

func TestLoadingOnlyTheNewestSessionsDoesNotInspectOlderOnes(t *testing.T) {
	directory := t.TempDir()
	older, err := store.Create(directory, store.Meta{Provider: model.CodexProvider, Model: "gpt-5.6-sol"})
	if err != nil {
		t.Fatal(err)
	}
	if err := older.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "older"}); err != nil {
		t.Fatal(err)
	}
	if err := older.Close(); err != nil {
		t.Fatal(err)
	}

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	journal, err := root.OpenFile(filepath.Join(older.Name(), "session.jsonl"), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.WriteString("{broken}\n"); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	time.Sleep(2 * time.Millisecond)
	newer, err := store.Create(directory, store.Meta{Provider: model.AnthropicProvider, Model: "claude-opus-5"})
	if err != nil {
		t.Fatal(err)
	}
	if err := newer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "newer"}); err != nil {
		t.Fatal(err)
	}
	if err := newer.Close(); err != nil {
		t.Fatal(err)
	}

	loadedSessions, total, err := LoadNewest(directory, 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Errorf("loaded %d sessions in total, want 2", total)
	}
	if len(loadedSessions) != 1 || loadedSessions[0].Name != newer.Name() {
		t.Errorf("loaded %+v, want only %s", loadedSessions, newer.Name())
	}
}

func TestTheSessionAddedToLastIsOfferedFirstByThePicker(t *testing.T) {
	directory := t.TempDir()
	workspaceDir := t.TempDir()

	names := make([]string, 0, 3)
	for range cap(names) {
		writer, err := store.Create(directory, store.Meta{WorkspaceDir: workspaceDir})
		if err != nil {
			t.Fatal(err)
		}
		if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "begin"}); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}

		names = append(names, writer.Name())
		time.Sleep(2 * time.Millisecond)
	}

	loadedSessions, err := Load(directory)
	if err != nil {
		t.Fatal(err)
	}

	loaded := make([]string, 0, len(loadedSessions))
	for _, loadedSession := range loadedSessions {
		loaded = append(loaded, loadedSession.Name)
	}

	slices.Reverse(names)
	if !slices.Equal(loaded, names) {
		t.Errorf("sessions loaded as %v, want the most recently touched first: %v", loaded, names)
	}
}

func TestCompletableNamesComeFromTheListingRatherThanTheJournal(t *testing.T) {
	directory := t.TempDir()
	workspaceDir := t.TempDir()

	names := make([]string, 0, 2)
	for range cap(names) {
		names = append(names, writeIdleSession(t, directory, store.Meta{WorkspaceDir: workspaceDir}))
		time.Sleep(2 * time.Millisecond)
	}
	writeIdleSession(t, directory, store.Meta{WorkspaceDir: t.TempDir()})

	emptyJournal(t, directory, names[0])

	got, err := NamesInWorkspace(directory, work.At(workspaceDir))
	if err != nil {
		t.Fatal(err)
	}

	slices.Reverse(names)
	if !slices.Equal(got, names) {
		t.Errorf("completed %v, want the workspace's own sessions newest first: %v", got, names)
	}
}

func TestASessionWithAnOlderListingDoesNotStopTheOthersCompleting(t *testing.T) {
	directory := t.TempDir()
	workspaceDir := t.TempDir()

	behind := writeIdleSession(t, directory, store.Meta{WorkspaceDir: workspaceDir})
	time.Sleep(2 * time.Millisecond)
	current := writeIdleSession(t, directory, store.Meta{WorkspaceDir: workspaceDir})

	putListingBehind(t, directory, behind)

	got, err := NamesInWorkspace(directory, work.At(workspaceDir))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{current}) {
		t.Errorf("completed %v, want only %s", got, current)
	}
}

func writeIdleSession(t *testing.T, directory string, meta store.Meta) string {
	t.Helper()

	writer, err := store.Create(directory, meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "begin"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return writer.Name()
}

func emptyJournal(t *testing.T, directory string, name string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(directory, name, "session.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestASessionRecordedThroughALinkBelongsToTheWorkspaceItNames(t *testing.T) {
	directory := t.TempDir()
	workspaceDir := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(workspaceDir, alias); err != nil {
		t.Fatal(err)
	}

	writer, err := store.Create(directory, store.Meta{WorkspaceDir: alias})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "begin"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	loadedSessions, err := Load(directory)
	if err != nil {
		t.Fatal(err)
	}

	for _, named := range []string{workspaceDir, alias} {
		if chosen := InWorkspace(loadedSessions, work.At(named)); len(chosen) != 1 {
			t.Errorf("the session was not offered in %q, got %+v", named, chosen)
		}
	}

	if chosen := InWorkspace(loadedSessions, work.At(t.TempDir())); len(chosen) != 0 {
		t.Errorf("the session was offered in another workspace, got %+v", chosen)
	}
}

func TestAWorkspaceWithNothingStoredSaysSo(t *testing.T) {
	directory := t.TempDir()
	workspaceDir := t.TempDir()

	writer, err := store.Create(directory, store.Meta{WorkspaceDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	var screen strings.Builder

	_, err = Choose(directory, work.At(workspaceDir), nil, &screen)
	if err == nil {
		t.Fatal("expected the empty workspace to be reported")
	}
	if err.Error() != "there are no stored conversations for this workspace" {
		t.Errorf("expected the workspace to be named as the empty one, got %v", err)
	}
	if screen.String() != "" {
		t.Errorf("expected nothing to be drawn, got %q", screen.String())
	}
}

func TestAListingInAnOlderFormatIsWrittenAgainRatherThanRefused(t *testing.T) {
	directory := t.TempDir()
	writer, err := store.Create(directory, store.Meta{WorkspaceDir: t.TempDir(), Model: "gpt-5.6-sol"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "begin"}); err != nil {
		t.Fatal(err)
	}
	name := writer.Name()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	putListingBehind(t, directory, name)

	if _, err := session.Open(directory, name); !errors.Is(err, session.ErrMetaOutOfDate) {
		t.Fatalf("expected an older listing to stop the session being opened, got %v", err)
	}

	var said strings.Builder
	if err := RefreshListings(directory, &said); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(said.String(), "writing the session listing again") {
		t.Errorf("expected the rebuild to be reported, got %q", said.String())
	}

	reopened, err := session.Open(directory, name)
	if err != nil {
		t.Fatalf("expected the session to open once its listing was written again, got %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}

	loadedSessions, err := Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedSessions) != 1 || loadedSessions[0].ModelID != "gpt-5.6-sol" {
		t.Errorf("expected the model to come back from the journal, got %+v", loadedSessions)
	}
	if loadedSessions[0].Model != "GPT Sol 5.6" {
		t.Errorf("expected the model to be named for a person, got %q", loadedSessions[0].Model)
	}
}

func TestGoldenAnOlderRunningSessionDoesNotKeepTriggeringListingRebuilds(t *testing.T) {
	directory := t.TempDir()
	workspaceDir := t.TempDir()
	writer, err := store.Create(directory, store.Meta{WorkspaceDir: workspaceDir, Model: "gpt-5.6-sol"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "begin"}); err != nil {
		t.Fatal(err)
	}
	name := writer.Name()
	putListingBehind(t, directory, name)

	var runningOutput strings.Builder
	for range 2 {
		if err := RefreshListings(directory, &runningOutput); err != nil {
			t.Fatal(err)
		}
	}
	if runningOutput.String() != "" {
		t.Errorf("expected no rebuild of a running session, got %q", runningOutput.String())
	}
	if _, err := session.ReadMeta(directory, name); !errors.Is(err, session.ErrMetaOutOfDate) {
		t.Fatalf("expected the old listing to remain in place, got %v", err)
	}

	loadedSessions, err := Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedSessions) != 1 || !loadedSessions[0].IsRunning {
		t.Fatalf("expected one running session, got %+v", loadedSessions)
	}
	if loadedSessions[0].WorkspaceDir != workspaceDir || loadedSessions[0].ModelID != "gpt-5.6-sol" {
		t.Errorf("expected the journal to supply the current listing, got %+v", loadedSessions[0])
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	var stoppedOutput strings.Builder
	if err := RefreshListings(directory, &stoppedOutput); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stoppedOutput.String(), "writing the session listing again") {
		t.Errorf("expected the stopped session to be rebuilt, got %q", stoppedOutput.String())
	}
	if _, err := session.ReadMeta(directory, name); err != nil {
		t.Fatalf("expected the stopped session to have a current listing, got %v", err)
	}

	comparePickerGolden(t, "listing-refresh.txt", strings.Join([]string{
		"=== running old session, twice ===\n",
		runningOutput.String(),
		"=== stopped old session ===\n",
		stoppedOutput.String(),
	}, ""))
}

func TestGoldenOnlyTheListingOfTheResumedSessionIsWrittenAgainAtStartup(t *testing.T) {
	directory := t.TempDir()
	workspaceDir := t.TempDir()
	resumed := writeIdleSession(t, directory, store.Meta{WorkspaceDir: workspaceDir})
	untouched := writeIdleSession(t, directory, store.Meta{WorkspaceDir: workspaceDir})
	putListingBehind(t, directory, resumed)
	putListingBehind(t, directory, untouched)

	var startupOutput strings.Builder
	if err := RefreshListing(directory, &startupOutput, resumed); err != nil {
		t.Fatal(err)
	}
	if _, err := session.ReadMeta(directory, resumed); err != nil {
		t.Errorf("expected the resumed listing to be written again, got %v", err)
	}
	if _, err := session.ReadMeta(directory, untouched); err == nil {
		t.Error("expected the listing of another session to be left as it was")
	}

	var pickerOutput strings.Builder
	if err := RefreshListings(directory, &pickerOutput); err != nil {
		t.Fatal(err)
	}
	if _, err := session.ReadMeta(directory, untouched); err != nil {
		t.Errorf("expected the picker to write the remaining listing again, got %v", err)
	}

	comparePickerGolden(t, "resumed-listing-refresh.txt", strings.Join([]string{
		"=== startup, resuming one of two behind ===\n",
		startupOutput.String(),
		"=== picker ===\n",
		pickerOutput.String(),
	}, ""))
}

func TestStartupSaysNothingOfAListingItWasNotAskedFor(t *testing.T) {
	directory := t.TempDir()
	name := writeIdleSession(t, directory, store.Meta{WorkspaceDir: t.TempDir()})
	putListingBehind(t, directory, name)

	for _, asked := range []string{"", "brave-unicorn"} {
		var drawn strings.Builder
		if err := RefreshListing(directory, &drawn, asked); err != nil {
			t.Errorf("expected %q to be left to the rest of startup, got %v", asked, err)
		}
		if drawn.String() != "" {
			t.Errorf("expected nothing to be drawn for %q, got %q", asked, drawn.String())
		}
	}

	if _, err := session.ReadMeta(directory, name); err == nil {
		t.Error("expected the listing to be left as it was")
	}
}

func comparePickerGolden(t *testing.T, name string, drawn string) {
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
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}

func putListingBehind(t *testing.T, directory string, name string) {
	t.Helper()

	path := filepath.Join(directory, name, "meta.json")
	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(stored, &fields); err != nil {
		t.Fatal(err)
	}
	fields["version"] = json.RawMessage("1")
	fields["data"] = json.RawMessage(`{"workspaceDir":"/tmp/somewhere"}`)

	behind, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, behind, 0o600); err != nil {
		t.Fatal(err)
	}
}
