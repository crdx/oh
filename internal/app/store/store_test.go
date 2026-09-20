package store_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/req"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"

	"crdx.org/oh/internal/app/conditions"
	"crdx.org/oh/internal/app/hostcommand"
	"crdx.org/oh/internal/app/store"
)

func write(t *testing.T, directory string) string {
	t.Helper()

	log, err := store.Create(directory, store.Meta{
		Model:        "gpt-5.6-sol",
		WorkspaceDir: "/tmp/somewhere",
		Provider:     "codex",
		Effort:       "high",
		SystemPrompt: "You are a coding assistant.",
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, event := range conversation {
		if err := log.Event(event); err != nil {
			t.Fatal(err)
		}
	}

	if err := log.Item(json.RawMessage(`{"type":"reasoning"}`)); err != nil {
		t.Fatal(err)
	}

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	return log.Name()
}

func appendRaw(t *testing.T, directory string, name string, text string) {
	t.Helper()

	file, err := os.OpenFile(filepath.Join(directory, name, "session.jsonl"), os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // the path is the test's own
	if err != nil {
		t.Fatal(err)
	}

	if _, err := file.WriteString(text); err != nil {
		t.Fatal(err)
	}

	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

var conversation = []agent.Event{
	{Kind: agent.UserMessageEvent, Text: "what is the weather in London?"},
	{Kind: agent.ModelMessageEvent, Text: "Let me look."},
	{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "weather", Arguments: `{"city":"London"}`, FallbackRendering: agent.FallbackRendering{Subject: "London"}},
	{Kind: agent.ToolCallResultEvent, ID: "1", Name: "weather", Text: "raining"},
	{Kind: agent.ModelMessageEvent, Text: "It is raining.", Usage: &agent.Usage{InputTokens: 5000}},
}

func TestASessionReadsBackAsItWasWritten(t *testing.T) {
	directory := t.TempDir()
	id := write(t, directory)

	storedSession, err := store.Read(directory, id)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(storedSession.Events, conversation) {
		t.Errorf("expected %v, got %v", conversation, storedSession.Events)
	}

	if want := "gpt-5.6-sol"; storedSession.Meta.Model != want {
		t.Errorf("expected the model to be pinned to %q, got %q", want, storedSession.Meta.Model)
	}

	if storedSession.Meta.Effort != "high" {
		t.Errorf("expected the harness config to survive, got %+v", storedSession.Meta)
	}

	if storedSession.Meta.SystemPrompt != "You are a coding assistant." {
		t.Errorf("expected the context to survive, got %+v", storedSession.Meta)
	}

	if len(storedSession.Items) != 1 || string(storedSession.Items[0]) != `{"type":"reasoning"}` {
		t.Errorf("expected the provider's item to come back verbatim, got %v", storedSession.Items)
	}

	if want := "what is the weather in London?"; storedSession.FirstMessage() != want {
		t.Errorf("expected %q, got %q", want, storedSession.FirstMessage())
	}

	meta, err := session.ReadMeta(directory, id)
	if err != nil {
		t.Fatal(err)
	}
	wantData := `{"workspaceDir":"/tmp/somewhere","provider":"codex","model":"gpt-5.6-sol","effort":"high"}`
	if string(meta.Data) != wantData {
		t.Errorf("unexpected meta data: %s", meta.Data)
	}
	if meta.Title != "what is the weather in London?" || meta.Messages != 3 {
		t.Errorf("unexpected metadata: %+v", meta)
	}
}

func TestTheConditionsAtCreationSurviveWithTheSession(t *testing.T) {
	directory := t.TempDir()
	createdConditions := conditions.Conditions{UnixSockets: true, Interactive: true}
	log, err := store.Create(directory, store.Meta{Conditions: &createdConditions})
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "store it"}); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatal(err)
	}
	if storedSession.Meta.Conditions == nil {
		t.Fatal("the conditions at creation were not stored")
	}
	if *storedSession.Meta.Conditions != createdConditions {
		t.Errorf("got %+v, want %+v", *storedSession.Meta.Conditions, createdConditions)
	}
}

func TestASessionStoredBeforeConditionsWereRecordedHasNone(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{Model: "gpt-5.6-sol"})
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "store it"}); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatal(err)
	}
	if storedSession.Meta.Conditions != nil {
		t.Errorf("got conditions %+v, want none", *storedSession.Meta.Conditions)
	}
}

func TestTheMetaCanIncludeTheGeneratedSessionID(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{SystemPrompt: "before"})
	if err != nil {
		t.Fatal(err)
	}

	systemPrompt := "scratch=" + log.Name()
	if err := log.SetMeta(store.Meta{SystemPrompt: systemPrompt}); err != nil {
		t.Fatal(err)
	}
	if err := log.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "store it"}); err != nil {
		t.Fatal(err)
	}
	if err := log.SetMeta(store.Meta{SystemPrompt: "too late"}); err == nil {
		t.Error("expected a stored meta to be immutable")
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatal(err)
	}
	if storedSession.Meta.SystemPrompt != systemPrompt {
		t.Errorf("got system prompt %q, want %q", storedSession.Meta.SystemPrompt, systemPrompt)
	}
}

func TestASessionIsNamedAfterAnAdjectiveAndAnAnimal(t *testing.T) {
	directory := t.TempDir()

	namePattern := regexp.MustCompile(`^[a-z]+-[a-z]+$`)

	first := write(t, directory)
	second := write(t, directory)

	for _, name := range []string{first, second} {
		if !namePattern.MatchString(name) {
			t.Errorf("expected %q to be two lowercase words joined by a hyphen", name)
		}
	}

	if first == second {
		t.Errorf("expected two sessions to be named differently, both got %q", first)
	}
}

func TestASessionNothingWasSaidInIsNeverWritten(t *testing.T) {
	directory := t.TempDir()

	log, err := store.Create(directory, store.Meta{Model: "gpt-5.6-sol"})
	if err != nil {
		t.Fatal(err)
	}

	if log.Name() == "" {
		t.Error("expected the session to be named before it is written")
	}

	if log.IsPersisted() {
		t.Error("expected nothing to carry on from before anything is said")
	}

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 0 {
		t.Errorf("expected nothing to have been written, got %v", entries)
	}
}

func TestASessionMayBePersistedBeforeItsFirstMessageForAFile(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{Model: "gpt-5.6-sol"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()

	if err := log.EnsurePersisted(); err != nil {
		t.Fatal(err)
	}
	if !log.IsPersisted() {
		t.Error("explicitly persisted session is not persisted")
	}
	if _, err := os.Stat(filepath.Join(directory, log.Name(), "session.jsonl")); err != nil {
		t.Errorf("persisted session has no journal: %v", err)
	}
}

func TestWhatHappensBeforeTheFirstMessageDoesNotWriteTheSession(t *testing.T) {
	directory := t.TempDir()

	log, err := store.Create(directory, store.Meta{Model: "gpt-5.6-sol"})
	if err != nil {
		t.Fatal(err)
	}

	if err := log.Event(agent.Event{Kind: agent.StartupEvent, Took: time.Millisecond}); err != nil {
		t.Fatal(err)
	}

	if log.IsPersisted() {
		t.Error("expected a startup notice on its own to leave nothing behind")
	}

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 0 {
		t.Errorf("expected nothing to have been written, got %v", entries)
	}
}

func TestWhatWasHeldBackIsWrittenInFrontOfTheFirstMessage(t *testing.T) {
	directory := t.TempDir()

	log, err := store.Create(directory, store.Meta{Model: "gpt-5.6-sol"})
	if err != nil {
		t.Fatal(err)
	}

	held := []agent.Event{
		{Kind: agent.StartupEvent, Took: time.Millisecond},
		{Kind: caps.ModeChange, Name: "w", Text: "rxwgs"},
	}

	for _, event := range held {
		if err := log.Event(event); err != nil {
			t.Fatal(err)
		}
	}

	if err := log.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
		t.Fatal(err)
	}

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatal(err)
	}

	want := []agent.Kind{agent.StartupEvent, caps.ModeChange, agent.UserMessageEvent}

	got := make([]agent.Kind, 0, len(storedSession.Events))
	for _, event := range storedSession.Events {
		got = append(got, event.Kind)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestAHostCommandCountsAsTheFirstThingSaid(t *testing.T) {
	directory := t.TempDir()

	log, err := store.Create(directory, store.Meta{Model: "gpt-5.6-sol"})
	if err != nil {
		t.Fatal(err)
	}

	if err := log.Event(agent.Event{Kind: agent.StartupEvent, Took: time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	if err := log.Event(hostcommand.RanEvent(hostcommand.Result{Command: "ls", Output: "readme\n"})); err != nil {
		t.Fatal(err)
	}

	if !log.IsPersisted() {
		t.Error("expected a command the person ran to begin the session")
	}

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatal(err)
	}

	want := []agent.Kind{agent.StartupEvent, hostcommand.Ran}

	got := make([]agent.Kind, 0, len(storedSession.Events))
	for _, event := range storedSession.Events {
		got = append(got, event.Kind)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestTheFirstThingSaidTakesTheHeadWithIt(t *testing.T) {
	directory := t.TempDir()

	log, err := store.Create(directory, store.Meta{Model: "gpt-5.6-sol", WorkspaceDir: "/tmp/somewhere"})
	if err != nil {
		t.Fatal(err)
	}

	if err := log.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
		t.Fatal(err)
	}

	if !log.IsPersisted() {
		t.Error("expected something to carry on from once something has been said")
	}

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatal(err)
	}

	if storedSession.Meta.Model != "gpt-5.6-sol" || storedSession.Meta.WorkspaceDir != "/tmp/somewhere" {
		t.Errorf("expected the meta to have gone in first, got %+v", storedSession.Meta)
	}

	if len(storedSession.Events) != 1 {
		t.Errorf("expected the one event, got %v", storedSession.Events)
	}
}

func TestMessagesCountsWhatWasSaidAndNotTheWorkingOut(t *testing.T) {
	directory := t.TempDir()

	storedSession, err := store.Read(directory, write(t, directory))
	if err != nil {
		t.Fatal(err)
	}

	if want := 3; storedSession.Messages() != want {
		t.Errorf("expected %d messages, got %d", want, storedSession.Messages())
	}
}

func TestAnOpenedSessionKeepsWhatWasThereBefore(t *testing.T) {
	directory := t.TempDir()
	id := write(t, directory)

	log, err := store.Open(directory, id)
	if err != nil {
		t.Fatal(err)
	}

	if err := log.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "and tomorrow?"}); err != nil {
		t.Fatal(err)
	}

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, id)
	if err != nil {
		t.Fatal(err)
	}

	if want := len(conversation) + 1; len(storedSession.Events) != want {
		t.Errorf("expected %d events, got %d", want, len(storedSession.Events))
	}
}

func TestAHalfWrittenLineEndsTheSession(t *testing.T) {
	directory := t.TempDir()
	id := write(t, directory)

	appendRaw(t, directory, id, `{"kind":"event","event":{"kind":"te`)

	storedSession, err := store.Read(directory, id)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(storedSession.Events, conversation) {
		t.Errorf("expected %v, got %v", conversation, storedSession.Events)
	}
}

func TestListReadsTheNewestFirst(t *testing.T) {
	directory := t.TempDir()

	first := write(t, directory)

	time.Sleep(2 * time.Millisecond)

	second := write(t, directory)

	sessions, err := store.List(directory)
	if err != nil {
		t.Fatal(err)
	}

	if len(sessions) != 2 {
		t.Fatalf("expected two sessions, got %d", len(sessions))
	}

	if sessions[0].Name != second || sessions[1].Name != first {
		t.Errorf("expected the newest first, got %s then %s", sessions[0].Name, sessions[1].Name)
	}
}

func TestListPutsTheSessionTouchedLastAtTheTop(t *testing.T) {
	directory := t.TempDir()

	older := write(t, directory)

	time.Sleep(2 * time.Millisecond)
	write(t, directory)
	time.Sleep(2 * time.Millisecond)

	log, err := store.Open(directory, older)
	if err != nil {
		t.Fatal(err)
	}

	if err := log.Event(agent.Event{Kind: agent.ModelMessageEvent, Text: "one more thing"}); err != nil {
		t.Fatal(err)
	}

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	sessions, err := store.List(directory)
	if err != nil {
		t.Fatal(err)
	}

	if sessions[0].Name != older {
		t.Errorf("expected the session added to last at the top, got %s", sessions[0].Name)
	}
}

func TestHTTPObservationDoesNotCreateTheBundleBeforeTheFirstEvent(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{
		Model:        "model",
		Effort:       "high",
		Provider:     "codex",
		WorkspaceDir: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}

	request := req.Request{
		StartedAt: time.Now(),
		Method:    "POST",
		URL:       "https://example.com",
		Protocol:  "HTTP/1.1",
	}

	if exchange := log.Observer().Start(request); exchange != nil {
		t.Error("expected an exchange before the first event to go unrecorded")
	}
	if log.IsPersisted() {
		t.Error("expected an exchange before the first event to leave the session unwritten")
	}
	if warnings := log.TakeWarnings(); len(warnings) != 0 {
		t.Fatalf("expected no recorder warnings, got %v", warnings)
	}

	bundle := filepath.Join(directory, log.Name())
	if _, err := os.Stat(bundle); !os.IsNotExist(err) {
		t.Errorf("expected no bundle at %s: %v", bundle, err)
	}

	if err := log.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
		t.Fatal(err)
	}

	if exchange := log.Observer().Start(request); exchange == nil {
		t.Error("expected an exchange after the first event to be recorded")
	}
	if warnings := log.TakeWarnings(); len(warnings) != 0 {
		t.Fatalf("expected no recorder warnings, got %v", warnings)
	}

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"session.jsonl", "meta.json", "chat.md", "wire.http"} {
		if _, err := os.Stat(filepath.Join(bundle, name)); err != nil {
			t.Errorf("expected %s: %v", name, err)
		}
	}
}

func TestTheFirstRecordCreatesACompleteBundle(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{
		Model:        "model",
		Effort:       "high",
		Provider:     "codex",
		WorkspaceDir: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Item(json.RawMessage(`{"type":"first"}`)); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	bundle := filepath.Join(directory, log.Name())
	for _, name := range []string{"session.jsonl", "meta.json", "chat.md", "wire.http"} {
		info, err := os.Stat(filepath.Join(bundle, name))
		if err != nil {
			t.Errorf("expected %s: %v", name, err)
			continue
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("expected %s mode 0600, got %o", name, info.Mode().Perm())
		}
	}
}

func TestOpeningABundleAppendsToTheMarkdownTranscript(t *testing.T) {
	directory := t.TempDir()
	id := write(t, directory)
	path := filepath.Join(directory, id, "chat.md")
	before, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	log, err := store.Open(directory, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "resumed text"}); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(after, before) || !strings.Contains(string(after), "resumed text") {
		t.Errorf("expected the transcript to append on resume, got:\n%s", after)
	}
	if strings.Count(string(after), "# Conversation") != 1 {
		t.Errorf("expected one metadata header, got:\n%s", after)
	}
}

func TestAnOpenSessionIsRefusedToASecondWriter(t *testing.T) {
	directory := t.TempDir()
	id := write(t, directory)

	first, err := store.Open(directory, id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()

	second, err := store.Open(directory, id)
	if !errors.Is(err, session.ErrInUse) {
		_ = second.Close()
		t.Fatalf("expected the second writer to be refused, got %v", err)
	}
}

func TestRebuildWritesTheSameTranscriptAgain(t *testing.T) {
	directory := t.TempDir()
	name := write(t, directory)
	path := filepath.Join(directory, name, "chat.md")

	first, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Rebuild(directory, name); err != nil {
		t.Fatal(err)
	}

	second, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	if string(first) != string(second) {
		t.Errorf("the transcript was written differently the second time:\n%s\n---\n%s", first, second)
	}
}

func TestRebuildReplacesATranscriptRatherThanAppendingToIt(t *testing.T) {
	directory := t.TempDir()
	name := write(t, directory)
	path := filepath.Join(directory, name, "chat.md")

	if err := os.WriteFile(path, []byte("whatever an older renderer wrote\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := store.Rebuild(directory, name); err != nil {
		t.Fatal(err)
	}

	rebuilt, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(rebuilt), "older renderer") {
		t.Error("the old transcript survived the rebuild")
	}
	if !strings.HasPrefix(string(rebuilt), "# Conversation\n") {
		t.Errorf("the rebuilt transcript does not start with a header:\n%s", rebuilt)
	}
}

func TestRebuildCreatesAMissingTranscriptInsideAnArchive(t *testing.T) {
	directory := t.TempDir()
	name := write(t, directory)
	path := filepath.Join(directory, name, "chat.md")

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := session.Archive(directory, name); err != nil {
		t.Fatal(err)
	}

	if err := store.Rebuild(directory, name); err != nil {
		t.Fatal(err)
	}

	if !session.IsArchived(directory, name) {
		t.Fatal("the rebuilt session was not archived again")
	}
	if err := session.Restore(directory, name); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(rebuilt), "# Conversation\n") {
		t.Errorf("the rebuilt transcript does not start with a header:\n%s", rebuilt)
	}
}

func TestRebuildLeavesTheJournalAlone(t *testing.T) {
	directory := t.TempDir()
	name := write(t, directory)
	path := filepath.Join(directory, name, "session.jsonl")

	before, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Rebuild(directory, name); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	if string(before) != string(after) {
		t.Error("the journal changed during a rebuild")
	}
}

func TestRebuildRefusesASessionThatIsNotThere(t *testing.T) {
	if err := store.Rebuild(t.TempDir(), "tame-impala"); err == nil {
		t.Error("expected a rebuild of an unstored session to be refused")
	}
}

func TestTheTranscriptCountsTimeFromWhenTheSessionStarted(t *testing.T) {
	directory := t.TempDir()
	name := write(t, directory)

	var started string
	var events []string
	err := session.Records(directory, name, func(line session.Line) error {
		switch line.Kind {
		case session.Head:
			started = line.Time.UTC().Format(time.RFC3339Nano)
		case session.Event:
			events = append(events, line.Time.UTC().Format(time.RFC3339Nano))
		case session.Item, session.TurnCompletion:
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	written, err := os.ReadFile(filepath.Join(directory, name, "chat.md")) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	transcript := string(written)

	if !strings.Contains(transcript, started) {
		t.Errorf("the transcript does not say when the session started, %q", started)
	}
	for _, at := range events {
		if strings.Contains(transcript, at) {
			t.Errorf("the transcript quotes the absolute time %q rather than an offset from the start", at)
		}
	}
	if !strings.Contains(transcript, " · +") {
		t.Errorf("the transcript counts no event from the start:\n%s", transcript)
	}
}

func TestAnArchivedListingInAnOlderFormatIsRebuiltInPlace(t *testing.T) {
	directory := t.TempDir()
	name := write(t, directory)

	putListingBehind(t, directory, name)

	if err := session.Archive(directory, name); err != nil {
		t.Fatal(err)
	}
	if _, err := session.ArchivedMeta(directory, name); !errors.Is(err, session.ErrMetaOutOfDate) {
		t.Fatalf("expected the archived listing to be out of date, got %v", err)
	}

	journalBefore := archivedJournal(t, directory, name)

	rebuilt, err := store.RebuildStaleMeta(directory)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt != 1 {
		t.Errorf("rebuilt %d listings, want 1", rebuilt)
	}

	if !session.IsArchived(directory, name) {
		t.Fatal("expected the session to be archived again")
	}
	meta, err := session.ArchivedMeta(directory, name)
	if err != nil {
		t.Fatalf("expected the rebuilt archived listing to be readable, got %v", err)
	}

	want := `{"workspaceDir":"/tmp/somewhere","provider":"codex","model":"gpt-5.6-sol","effort":"high"}`
	if string(meta.Data) != want {
		t.Errorf("expected the listing to come back from the journal, got %s", meta.Data)
	}
	if got := archivedJournal(t, directory, name); got != journalBefore {
		t.Error("the journal inside the archive was not left alone")
	}

	stale, err := store.StaleMeta(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Errorf("expected nothing left stale, got %v", stale)
	}
}

func archivedJournal(t *testing.T, directory string, name string) string {
	t.Helper()

	if err := session.Restore(directory, name); err != nil {
		t.Fatal(err)
	}
	journal, err := os.ReadFile(filepath.Join(directory, name, "session.jsonl")) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Archive(directory, name); err != nil {
		t.Fatal(err)
	}

	return string(journal)
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

func TestAListingWrittenBeforeTheModelWasInItIsRebuiltWithIt(t *testing.T) {
	directory := t.TempDir()
	name := write(t, directory)

	putListingBehind(t, directory, name)

	rebuilt, err := store.RebuildStaleMeta(directory)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt != 1 {
		t.Errorf("expected the one stale listing to be rebuilt, got %d", rebuilt)
	}

	meta, err := session.ReadMeta(directory, name)
	if err != nil {
		t.Fatal(err)
	}

	want := `{"workspaceDir":"/tmp/somewhere","provider":"codex","model":"gpt-5.6-sol","effort":"high"}`
	if string(meta.Data) != want {
		t.Errorf("expected the model to come back from the journal, got %s", meta.Data)
	}
}
