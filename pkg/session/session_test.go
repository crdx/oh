package session_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
)

func TestTheHeadCarriesTheNameAndTheIdentifier(t *testing.T) {
	directory := t.TempDir()

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

	head, err := os.ReadFile(filepath.Join(directory, writer.Name(), "session.jsonl")) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	var line struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
		ID   string `json:"id"`
	}

	if err := json.Unmarshal([]byte(strings.SplitN(string(head), "\n", 2)[0]), &line); err != nil {
		t.Fatal(err)
	}

	if line.Kind != "head" {
		t.Fatalf("expected the first line to be the head, got %q", line.Kind)
	}

	if line.Name != writer.Name() {
		t.Errorf("got the name %q in the head, want %q", line.Name, writer.Name())
	}

	if line.ID != writer.ID() {
		t.Errorf("got the identifier %q in the head, want %q", line.ID, writer.ID())
	}
}

func TestMetaCarriesOnlyListingData(t *testing.T) {
	directory := t.TempDir()
	canonicalMeta := json.RawMessage(`{"workspaceDir":"/workspace","systemPrompt":"very large"}`)
	listingData := json.RawMessage(`{"workspaceDir":"/workspace"}`)
	writer, err := session.Create(directory, canonicalMeta, listingData)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "first question"}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{Kind: agent.StateChangeEvent, Name: "file_read"}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{Kind: agent.ModelMessageEvent, Text: "first answer"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Item(json.RawMessage(`{"large":"provider state"}`)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	meta, err := session.ReadMeta(directory, writer.Name())
	if err != nil {
		t.Fatal(err)
	}
	if string(meta.Data) != string(listingData) {
		t.Errorf("got meta data %s, want %s", meta.Data, listingData)
	}
	if meta.Title != "first question" || meta.Messages != 2 {
		t.Errorf("unexpected metadata: %+v", meta)
	}
	if meta.StartedAt.IsZero() || meta.TouchedAt.Before(meta.StartedAt) {
		t.Errorf("unexpected metadata times: %+v", meta)
	}

	encoded, err := os.ReadFile(filepath.Join(directory, writer.Name(), "meta.json")) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "systemPrompt") || strings.Contains(string(encoded), "provider state") {
		t.Errorf("meta contains journal-only data: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"data":{"workspaceDir":"/workspace"}`) || strings.Contains(string(encoded), `"meta":`) {
		t.Errorf("meta does not use the caller-owned data field: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"title":"first question"`) || strings.Contains(string(encoded), "firstMessage") {
		t.Errorf("meta does not use the durable title field: %s", encoded)
	}
}

func TestMalformedJournalRecordsAreReportedUnlessTheLastIsUnterminated(t *testing.T) {
	tests := []struct {
		name        string
		suffix      string
		wantFailure bool
	}{
		{name: "interior record", suffix: "not-json\n{}\n", wantFailure: true},
		{name: "terminated final record", suffix: "not-json\n", wantFailure: true},
		{name: "record split across lines", suffix: "{\"kind\":\"event\",\n\"event\":{}}\n", wantFailure: true},
		{name: "records joined on one line", suffix: "{\"kind\":\"event\"}{\"kind\":\"event\"}\n", wantFailure: true},
		{name: "incomplete unterminated final record", suffix: `{"kind":"event"`},
		{name: "complete unterminated final record", suffix: `{"kind":"event"}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			writer, err := session.Create(directory, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "kept"}); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}

			path := filepath.Join(directory, writer.Name(), "session.jsonl")
			journal, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // test-owned journal path
			if err != nil {
				t.Fatal(err)
			}
			if _, err := journal.WriteString(test.suffix); err != nil {
				_ = journal.Close()
				t.Fatal(err)
			}
			if err := journal.Close(); err != nil {
				t.Fatal(err)
			}

			storedSession, err := session.Read(directory, writer.Name())
			if test.wantFailure {
				if err == nil || !strings.Contains(err.Error(), "malformed journal line 3") {
					t.Fatalf("got %v, want the malformed third line reported", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(storedSession.Events) != 1 || storedSession.Events[0].Text != "kept" {
				t.Errorf("unexpected recovered events: %#v", storedSession.Events)
			}
		})
	}
}

func TestTurnCompletionIsReadBackSeparatelyFromEventsAndItems(t *testing.T) {
	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Item(json.RawMessage(`{"role":"user"}`)); err != nil {
		t.Fatal(err)
	}
	if err := writer.CompleteTurn(session.TurnSummary{}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := session.Read(directory, writer.Name())
	if err != nil {
		t.Fatal(err)
	}
	if storedSession.TurnCompletions != 1 || len(storedSession.Events) != 1 || len(storedSession.Items) != 1 || storedSession.HasIncompleteTurn {
		t.Errorf("unexpected stored session: %+v", storedSession)
	}
}

func TestTheLastCacheReadingIsReadBackWithTheMomentItWasWritten(t *testing.T) {
	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	cachedAs := func(readTokens int, writeTokens int) *agent.Usage {
		return &agent.Usage{
			InputTokens: readTokens + writeTokens,
			Cache:       &agent.CacheUsage{ReadTokens: readTokens, WriteTokens: writeTokens},
		}
	}

	events := []agent.Event{
		{Kind: agent.ModelMessageEvent, Text: "first", Usage: cachedAs(9000, 400)},
		{Kind: agent.ModelMessageEvent, Text: "second", Usage: cachedAs(48000, 900)},
		{Kind: agent.CacheRebuildEvent, Name: string(agent.CacheRebuilt), Usage: cachedAs(0, 49000)},
		{Kind: agent.ModelMessageEvent, Text: "third"},
	}

	var writtenAt time.Time
	for _, event := range events {
		moment, err := writer.Event(event)
		if err != nil {
			t.Fatal(err)
		}
		if event.Kind == agent.ModelMessageEvent && event.Text == "second" {
			writtenAt = moment
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := session.Read(directory, writer.Name())
	if err != nil {
		t.Fatal(err)
	}

	want := agent.CacheReading{ReadTokens: 48000, At: writtenAt}
	if got := storedSession.CacheReading; got.ReadTokens != want.ReadTokens || !got.At.Equal(want.At) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestProviderStateCarryingThreeLargestReadableImagesCanBeListed(t *testing.T) {
	const readToolMaximumImageBytes = 20 * 1024 * 1024

	encodedImageBytes := 4 * ((readToolMaximumImageBytes + 2) / 3)
	encodedImage := strings.Repeat("A", encodedImageBytes)
	payload := json.RawMessage(`{"images":["` + encodedImage + `","` + encodedImage + `","` + encodedImage + `"]}`)

	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Item(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	storedSessions, err := session.List(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(storedSessions) != 1 || len(storedSessions[0].Items) != 1 {
		t.Fatalf("large session was omitted: %+v", storedSessions)
	}
	if got := len(storedSessions[0].Items[0]); got != len(payload) {
		t.Errorf("large provider state has %d bytes, want %d", got, len(payload))
	}
}

func TestSeveralMessagesCanShareOneCompletedTurn(t *testing.T) {
	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range []string{"mode changed", "continue"} {
		if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: message}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.CompleteTurn(session.TurnSummary{}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := session.Read(directory, writer.Name())
	if err != nil {
		t.Fatal(err)
	}
	if storedSession.HasIncompleteTurn {
		t.Error("completed turn containing two messages was read as incomplete")
	}
}

func TestAUserMessageAfterAnExtraCompletionIsStillIncomplete(t *testing.T) {
	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.CompleteTurn(session.TurnSummary{}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "unfinished"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := session.Read(directory, writer.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !storedSession.HasIncompleteTurn {
		t.Error("unfinished message was masked by an earlier completion")
	}
}

func TestAResumedSessionContinuesItsMeta(t *testing.T) {
	directory := t.TempDir()
	journalMeta := json.RawMessage(`{"model":"some-model"}`)
	writer, err := session.Create(directory, journalMeta, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	resumed, err := session.Open(directory, writer.Name())
	if err != nil {
		t.Fatal(err)
	}
	openedMeta := resumed.JournalMeta()
	if string(openedMeta) != string(journalMeta) {
		t.Errorf("got opened journal meta %s, want %s", openedMeta, journalMeta)
	}
	openedMeta[0] = '!'
	if got := resumed.JournalMeta(); string(got) != string(journalMeta) {
		t.Errorf("opened journal meta changed through its recipient: %s", got)
	}
	if _, err := resumed.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "second"}); err != nil {
		t.Fatal(err)
	}
	if err := resumed.Close(); err != nil {
		t.Fatal(err)
	}

	meta, err := session.ReadMeta(directory, writer.Name())
	if err != nil {
		t.Fatal(err)
	}
	if meta.Title != "first" || meta.Messages != 2 {
		t.Errorf("unexpected resumed metadata: %+v", meta)
	}
}

func TestMetadataAreNewestFirst(t *testing.T) {
	directory := t.TempDir()
	first, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "second"}); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}

	metadata, err := session.ListMeta(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata) != 2 || metadata[0].Name != second.Name() || metadata[1].Name != first.Name() {
		t.Errorf("unexpected metadata order: %+v", metadata)
	}
}

func TestMetadataReportMissingMetadata(t *testing.T) {
	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.EnsurePersisted(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(directory, writer.Name(), "meta.json")); err != nil {
		t.Fatal(err)
	}

	if _, err := session.ListMeta(directory); err == nil {
		t.Error("expected the missing metadata to be reported")
	}
}

func TestJournalCarriesMetaEventsAndItems(t *testing.T) {
	directory := t.TempDir()
	meta := json.RawMessage(`{"world":"weather"}`)
	writer, err := session.Create(directory, meta, meta)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "will it rain?"}); err != nil {
		t.Fatal(err)
	}
	state := json.RawMessage(`{"path":"weather.txt","sha256":"abc"}`)
	if _, err := writer.Event(agent.Event{Kind: agent.StateChangeEvent, Name: "file_read", State: state}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Item(json.RawMessage(`{"type":"message"}`)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := session.Read(directory, writer.Name())
	if err != nil {
		t.Fatal(err)
	}
	if string(storedSession.Meta) != string(meta) {
		t.Errorf("got meta %s, want %s", storedSession.Meta, meta)
	}
	if len(storedSession.Events) != 2 || storedSession.Events[0].Text != "will it rain?" {
		t.Errorf("got events %v", storedSession.Events)
	}
	if storedState := storedSession.Events[1]; storedState.Kind != agent.StateChangeEvent || string(storedState.State) != string(state) {
		t.Errorf("got stored state %#v", storedState)
	}
	if len(storedSession.Items) != 1 || string(storedSession.Items[0]) != `{"type":"message"}` {
		t.Errorf("got items %v", storedSession.Items)
	}
}

func storedSession(t *testing.T, directory string) *session.Writer {
	t.Helper()

	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
		t.Fatal(err)
	}

	return writer
}

func TestASessionInUseIsRefusedToASecondWriter(t *testing.T) {
	directory := t.TempDir()
	writer := storedSession(t, directory)
	defer func() { _ = writer.Close() }()

	second, err := session.Open(directory, writer.Name())
	if !errors.Is(err, session.ErrInUse) {
		_ = second.Close()
		t.Fatalf("expected the second writer to be refused, got %v", err)
	}
}

func TestAMissingSessionIsNamedRatherThanItsPath(t *testing.T) {
	directory := t.TempDir()

	reads := map[string]func() error{
		"IsInUse": func() error { _, err := session.IsInUse(directory, "able-dolphin"); return err },
		"Open":    func() error { _, err := session.Open(directory, "able-dolphin"); return err },
		"Read":    func() error { _, err := session.Read(directory, "able-dolphin"); return err },
	}

	for name, read := range reads {
		err := read()
		if !errors.Is(err, session.ErrNotFound) {
			t.Errorf("%s: expected the session to be reported missing, got %v", name, err)
		} else if got := err.Error(); got != `no session named "able-dolphin"` {
			t.Errorf("%s: got %q", name, got)
		}
	}
}

func TestASessionReportsWhetherItIsInUse(t *testing.T) {
	directory := t.TempDir()
	writer := storedSession(t, directory)

	isInUse, err := session.IsInUse(directory, writer.Name())
	if err != nil || !isInUse {
		t.Fatalf("expected the open session to be in use, got %t and %v", isInUse, err)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	isInUse, err = session.IsInUse(directory, writer.Name())
	if err != nil || isInUse {
		t.Errorf("expected the closed session to be free, got %t and %v", isInUse, err)
	}
}

func TestAcquiringABundleLockRefusesALockedJournal(t *testing.T) {
	directory := t.TempDir()
	writer := storedSession(t, directory)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	journalPath := filepath.Join(directory, writer.Name(), "session.jsonl")
	journal, err := os.Open(journalPath) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = journal.Close() }()

	if err := syscall.Flock(int(journal.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}

	heldLock, err := session.AcquireLock(directory, writer.Name())
	if !errors.Is(err, session.ErrInUse) {
		if heldLock != nil {
			_ = heldLock.Release()
		}
		t.Fatalf("expected the journal lock to be honoured, got %v", err)
	}
}

func TestReplacingAnOpenJournalDoesNotVoidItsLock(t *testing.T) {
	directory := t.TempDir()
	writer := storedSession(t, directory)

	journalPath := filepath.Join(directory, writer.Name(), "session.jsonl")
	journal, err := os.ReadFile(journalPath) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	replacementPath := filepath.Join(directory, writer.Name(), "replacement.jsonl")
	if err := os.WriteFile(replacementPath, journal, 0o600); err != nil { //nolint:gosec // the test's own path
		t.Fatal(err)
	}
	if err := os.Rename(replacementPath, journalPath); err != nil {
		t.Fatal(err)
	}

	second, err := session.Open(directory, writer.Name())
	if !errors.Is(err, session.ErrInUse) {
		if second != nil {
			_ = second.Close()
		}
		t.Fatalf("expected the bundle lock to survive replacing the journal, got %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestASessionInUseIsGivenUpOnceItIsClosed(t *testing.T) {
	directory := t.TempDir()
	writer := storedSession(t, directory)

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := session.Open(directory, writer.Name())
	if err != nil {
		t.Fatalf("expected the closed session to be free, got %v", err)
	}

	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAJournalSaysWhichFormatItWasWrittenIn(t *testing.T) {
	directory := t.TempDir()

	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.EnsurePersisted(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := session.Entries(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one stored session, got %d", len(entries))
	}
	if entries[0].Format != session.JournalFormat {
		t.Errorf("expected format %d, got %d", session.JournalFormat, entries[0].Format)
	}

	outdated, err := session.Outdated(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(outdated) != 0 {
		t.Errorf("expected a session just written to be current, got %v", outdated)
	}
}

func TestAJournalFromANewerOhIsNamedAndNotReplayed(t *testing.T) {
	directory := t.TempDir()
	name := "tame-impala"

	if err := os.MkdirAll(filepath.Join(directory, name), 0o750); err != nil {
		t.Fatal(err)
	}

	head := fmt.Sprintf(
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":%d,"id":"one","name":"tame-impala"}`,
		session.JournalFormat+1,
	) + "\n"
	if err := os.WriteFile(filepath.Join(directory, name, "session.jsonl"), []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}

	ahead, err := session.Ahead(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(ahead) != 1 || ahead[0] != name {
		t.Errorf("expected the newer journal to be named as ahead, got %v", ahead)
	}

	outdated, err := session.Outdated(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(outdated) != 0 {
		t.Errorf("expected a newer journal not to count as outdated, got %v", outdated)
	}

	if _, err := session.Read(directory, name); err == nil || !strings.Contains(err.Error(), "newer build") {
		t.Errorf("expected reading a newer journal to say where it came from, got %v", err)
	}
}

func TestAJournalWithoutAVersionCountsAsTheFirstFormat(t *testing.T) {
	directory := t.TempDir()
	name := "tame-impala"

	if err := os.MkdirAll(filepath.Join(directory, name), 0o750); err != nil {
		t.Fatal(err)
	}

	head := `{"kind":"head","time":"2026-08-01T00:00:00Z","id":"one","name":"tame-impala"}` + "\n"
	if err := os.WriteFile(filepath.Join(directory, name, "session.jsonl"), []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}

	entries, err := session.Entries(directory)
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Format != 1 {
		t.Errorf("expected an unnumbered journal to count as the first format, got %d", entries[0].Format)
	}

	outdated, err := session.Outdated(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(outdated) != 1 || outdated[0] != name {
		t.Errorf("expected the old journal to be named as outdated, got %v", outdated)
	}
}

func TestMetadataIsStampedWithItsFormat(t *testing.T) {
	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.EnsurePersisted(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	meta, err := session.ReadMeta(directory, writer.Name())
	if err != nil {
		t.Fatal(err)
	}
	if meta.Version != session.MetaFormat {
		t.Errorf("expected metadata stamped %d, got %d", session.MetaFormat, meta.Version)
	}
}

func TestMetadataInAnotherFormatIsNamedAsSuch(t *testing.T) {
	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.EnsurePersisted(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(directory, writer.Name(), "meta.json")
	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(stored, &fields); err != nil {
		t.Fatal(err)
	}
	fields["version"] = json.RawMessage(strconv.Itoa(session.MetaFormat + 1))
	ahead, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, ahead, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := session.ReadMeta(directory, writer.Name()); !errors.Is(err, session.ErrMetaOutOfDate) {
		t.Errorf("expected metadata in another format to be named as such, got %v", err)
	}
}

func titleState(t *testing.T, name string) json.RawMessage {
	t.Helper()

	state, err := json.Marshal(agent.TitleState{Title: name})
	if err != nil {
		t.Fatal(err)
	}

	return state
}

func TestTheTitleTheModelGivesReplacesTheOpeningMessage(t *testing.T) {
	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	events := []agent.Event{
		{Kind: agent.UserMessageEvent, Text: "have a look at the picker"},
		{Kind: agent.StateChangeEvent, Name: agent.TitleStateKey, State: titleState(t, "fix the picker clipping")},
		{Kind: agent.ModelMessageEvent, Text: "done"},
		{Kind: agent.StateChangeEvent, Name: agent.TitleStateKey, State: titleState(t, "give sessions a title")},
		{Kind: agent.StateChangeEvent, Name: agent.TitleStateKey, State: json.RawMessage(`"another shape"`)},
		{Kind: agent.StateChangeEvent, Name: agent.TitleStateKey, State: json.RawMessage(`{"title":""}`)},
		{Kind: agent.StateChangeEvent, Name: "file_read", State: json.RawMessage(`{"title":"not a title"}`)},
		{Kind: agent.UserMessageEvent, Text: "carry on"},
	}
	for _, event := range events {
		if _, err := writer.Event(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	meta, err := session.ReadMeta(directory, writer.Name())
	if err != nil {
		t.Fatal(err)
	}
	if meta.Title != "give sessions a title" || meta.Messages != 3 {
		t.Errorf("unexpected metadata: %+v", meta)
	}

	if err := os.Remove(filepath.Join(directory, writer.Name(), "meta.json")); err != nil {
		t.Fatal(err)
	}
	if err := session.RebuildMeta(directory, writer.Name(), nil); err != nil {
		t.Fatal(err)
	}

	rebuilt, err := session.ReadMeta(directory, writer.Name())
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.Title != meta.Title || rebuilt.Messages != meta.Messages {
		t.Errorf("a rebuilt listing lost the title: %+v", rebuilt)
	}
}

func TestASessionKeepsItsOpeningMessageUntilItIsTitled(t *testing.T) {
	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "have a look"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	meta, err := session.ReadMeta(directory, writer.Name())
	if err != nil {
		t.Fatal(err)
	}
	if meta.Title != "have a look" {
		t.Errorf("unexpected metadata: %+v", meta)
	}
}
