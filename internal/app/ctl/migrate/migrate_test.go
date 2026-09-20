package migrate_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"crdx.org/io/internal/app/caps"
	"crdx.org/io/internal/app/ctl/migrate"
	"crdx.org/io/internal/app/interrupt"
	"crdx.org/io/internal/app/pathgrant"
	"crdx.org/io/internal/app/store"
	"crdx.org/io/internal/app/turn"
	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/session"
)

func storedJournal(t *testing.T, lines ...string) (string, string) {
	t.Helper()

	directory := t.TempDir()
	name := "tame-impala"

	if err := os.MkdirAll(filepath.Join(directory, name), 0o750); err != nil {
		t.Fatal(err)
	}

	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(directory, name, "session.jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return directory, name
}

func options(directory string) migrate.Options {
	return migrate.Options{Directory: directory, BackupDir: directory + "_copies"}
}

func dryRun(directory string) migrate.Options {
	held := options(directory)
	held.DryRun = true

	return held
}

func journalLines(t *testing.T, directory string, name string) []map[string]json.RawMessage {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(directory, name, "session.jsonl")) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	var lines []map[string]json.RawMessage

	for text := range strings.SplitSeq(strings.TrimSpace(string(body)), "\n") {
		var line map[string]json.RawMessage
		if err := json.Unmarshal([]byte(text), &line); err != nil {
			t.Fatal(err)
		}

		lines = append(lines, line)
	}

	return lines
}

func TestAJournalWithoutAVersionIsMigratedFromTheFirstFormat(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","id":"one","name":"tame-impala","meta":{"workspaceDir":"/workspace"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"tool_call_request","name":"read","highlight":{"kind":"focus","value":"draw.go"}}}`,
		`{"kind":"event","time":"2026-08-01T00:00:02Z","event":{"kind":"user_message","text":"first question"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:03Z","event":{"kind":"model_message","text":"first answer"}}`,
	)

	from, err := migrate.Session(options(directory), name)
	if err != nil {
		t.Fatal(err)
	}

	if from != 1 {
		t.Errorf("expected an unnumbered journal to count as the first format, got %d", from)
	}

	lines := journalLines(t, directory, name)

	if got := string(lines[0]["version"]); got != strconv.Itoa(session.JournalFormat) {
		t.Errorf("expected the head to say format %d, got %q", session.JournalFormat, got)
	}

	event := string(lines[1]["event"])
	if strings.Contains(event, "highlight") || !strings.Contains(event, `"emphasis"`) {
		t.Errorf("expected highlight to have become emphasis, got %s", event)
	}
	if !strings.Contains(event, `"value":"draw.go"`) {
		t.Errorf("expected what the field said to survive the rename, got %s", event)
	}

	meta, err := session.ReadMeta(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != name || meta.Title != "first question" || meta.Messages != 2 {
		t.Errorf("unexpected migrated metadata: %+v", meta)
	}
	if string(meta.Data) != `{"workspaceDir":"/workspace"}` {
		t.Errorf("unexpected migrated data: %s", meta.Data)
	}
}

func TestAJournalAlreadyCurrentIsLeftAlone(t *testing.T) {
	head := fmt.Sprintf(`{"kind":"head","time":"2026-08-01T00:00:00Z","version":%d,"id":"one","name":"tame-impala"}`, session.JournalFormat)
	directory, name := storedJournal(t, head)

	from, err := migrate.Session(options(directory), name)
	if err != nil {
		t.Fatal(err)
	}

	if from != session.JournalFormat {
		t.Errorf("expected the current format, got %d", from)
	}

	if got := string(journalLines(t, directory, name)[0]["id"]); got != `"one"` {
		t.Errorf("expected the journal untouched, got %s", got)
	}
}

func TestFormatThreeMigrationMarksOnlyTurnsWithDurableProviderState(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":3,"id":"one","name":"tame-impala"}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"user_message","text":"complete"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:02Z","event":{"kind":"model_message","text":"done"}}`,
		`{"kind":"item","time":"2026-08-01T00:00:03Z","payload":{"role":"assistant"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:04Z","event":{"kind":"user_message","text":"crashed"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:05Z","event":{"kind":"model_message","text":"looks done but was not flushed"}}`,
	)

	from, err := migrate.Session(options(directory), name)
	if err != nil {
		t.Fatal(err)
	}
	if from != 3 {
		t.Errorf("migrated from format %d, want 3", from)
	}

	lines := journalLines(t, directory, name)
	completionCount := 0
	for _, line := range lines {
		if string(line["kind"]) == `"turn_completion"` {
			completionCount++
		}
	}
	if completionCount != 1 {
		t.Errorf("wrote %d completion records, want 1", completionCount)
	}

	storedSession, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	if storedSession.TurnCompletions != 1 {
		t.Errorf("migrated %d completed turns, want 1", storedSession.TurnCompletions)
	}
	if storedSession.CanResume() {
		t.Error("expected the migrated crashed turn to remain unsafe")
	}
}

func TestFormatThreeMigrationDoesNotCompleteAPartialProviderStateWrite(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":3,"id":"one","name":"tame-impala"}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"user_message","text":"crashed"}}`,
		`{"kind":"item","time":"2026-08-01T00:00:02Z","payload":{"type":"partial"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:03Z","event":{"kind":"harness_message","text":"the conversation state could not be stored: disk full","failed":true}}`,
	)

	if _, err := migrate.Session(options(directory), name); err != nil {
		t.Fatal(err)
	}
	storedSession, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	if storedSession.TurnCompletions != 0 || storedSession.CanResume() {
		t.Errorf("partial state write became resumable: %+v", storedSession)
	}
}

func TestFormatFourMigrationRecoversTheLastKnownMode(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":4,"id":"one","name":"tame-impala","meta":{"system_prompt":"# State\n\n- The workspace (/workspace) is read-only\n- The .git directory within it (/workspace/.git) is read-only\n- Background processes are killed when their shell command ends\n- The bash tool is granted"}}`,
		`{"kind":"item","time":"2026-08-01T00:00:01Z","payload":{"role":"user","content":"The workspace is now read-write."}}`,
		`{"kind":"event","time":"2026-08-01T00:00:02Z","event":{"kind":"mode_change","text":"rxw"}}`,
		`{"kind":"item","time":"2026-08-01T00:00:03Z","payload":{"role":"user","content":[{"type":"text","text":"The workspace is now read-only."}]}}`,
	)

	from, err := migrate.Session(options(directory), name)
	if err != nil {
		t.Fatal(err)
	}
	if from != 4 {
		t.Errorf("migrated from format %d, want 4", from)
	}

	storedSession, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}

	var modeEvents int
	for _, event := range storedSession.Events {
		if event.Kind == caps.ModeChange {
			modeEvents++
		}
	}
	if modeEvents != 1 {
		t.Errorf("kept %d mode events, want one authoritative event", modeEvents)
	}
	if currentCaps, recorded := caps.LastRecordedMode(storedSession.Events); !recorded || currentCaps != caps.Read|caps.Shell {
		t.Errorf("recovered %s and %t, want rx", currentCaps.Flags(), recorded)
	}
}

func TestFormatFourMigrationPreservesARealModeChange(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":4,"id":"one","name":"tame-impala","meta":{"system_prompt":"# State\n\n- The workspace (/workspace) is read-only\n- The .git directory within it (/workspace/.git) is read-only\n- Background processes are killed when their shell command ends\n- The bash tool is granted"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"mode_change","text":"rxw"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:02Z","event":{"kind":"mode_change","name":"g","text":"rxg"}}`,
	)

	if _, err := migrate.Session(options(directory), name); err != nil {
		t.Fatal(err)
	}
	storedSession, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	if currentCaps, recorded := caps.LastRecordedMode(storedSession.Events); !recorded || currentCaps != caps.Read|caps.Shell|caps.Git {
		t.Errorf("recovered %s and %t, want rxg", currentCaps.Flags(), recorded)
	}
}

func TestADryRunWritesNothing(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","id":"one","name":"tame-impala"}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"tool_call_request","highlight":{"kind":"focus"}}}`,
	)

	if _, err := migrate.Session(dryRun(directory), name); err != nil {
		t.Fatal(err)
	}

	lines := journalLines(t, directory, name)
	if _, isNumbered := lines[0]["version"]; isNumbered {
		t.Error("expected a dry run to leave the head unnumbered")
	}
	if !strings.Contains(string(lines[1]["event"]), "highlight") {
		t.Error("expected a dry run to leave the event as it found it")
	}
	if _, err := session.ReadMeta(directory, name); err == nil {
		t.Error("expected a dry run not to create metadata")
	}
}

func TestAnInUseJournalIsNotMigrated(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","id":"one","name":"tame-impala"}`,
	)

	heldLock, err := session.AcquireLock(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = heldLock.Release() }()

	if _, err := migrate.Session(options(directory), name); !errors.Is(err, session.ErrInUse) {
		t.Fatalf("expected an in-use session to be refused, got %v", err)
	}

	lines := journalLines(t, directory, name)
	if _, isNumbered := lines[0]["version"]; isNumbered {
		t.Error("expected the in-use journal to be left untouched")
	}
	if _, err := os.Stat(options(directory).BackupDir); !os.IsNotExist(err) {
		t.Error("expected no backup of the in-use journal")
	}
}

func TestAJournalFromANewerBuildIsRefused(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":99,"id":"one","name":"tame-impala"}`,
	)

	_, err := migrate.Session(options(directory), name)
	if err == nil {
		t.Fatal("expected a journal from the future to be refused")
	}

	if !strings.Contains(err.Error(), "upgrade oh") {
		t.Errorf("expected the error to say where it came from, got %v", err)
	}
}

func TestAnEmptyJournalIsRefused(t *testing.T) {
	directory := t.TempDir()
	name := "tame-impala"

	if err := os.MkdirAll(filepath.Join(directory, name), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, name, "session.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := migrate.Session(options(directory), name); err == nil {
		t.Fatal("expected an empty journal to be refused")
	}
}

func TestACopyOfTheBundleIsKeptBeforeItIsWritten(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","id":"one","name":"tame-impala"}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"tool_call_request","highlight":{"kind":"focus"}}}`,
	)

	held := options(directory)

	if _, err := migrate.Session(held, name); err != nil {
		t.Fatal(err)
	}

	kept, err := os.ReadFile(filepath.Join(held.BackupDir, name, "session.jsonl")) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(kept), `"highlight"`) {
		t.Error("expected the copy to hold the journal as it stood before")
	}
}

func TestACopyIsNotWrittenOver(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","id":"one","name":"tame-impala"}`,
	)

	held := options(directory)
	if err := os.MkdirAll(filepath.Join(held.BackupDir, name), 0o750); err != nil {
		t.Fatal(err)
	}

	_, err := migrate.Session(held, name)
	if err == nil {
		t.Fatal("expected a copy already kept to stop the migration")
	}

	if !strings.Contains(err.Error(), "move it aside") {
		t.Errorf("expected the error to say what to do, got %v", err)
	}
}

func TestADryRunKeepsNoCopy(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","id":"one","name":"tame-impala"}`,
	)

	held := dryRun(directory)
	if _, err := migrate.Session(held, name); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(held.BackupDir); !os.IsNotExist(err) {
		t.Error("expected a dry run to leave no copies behind")
	}
}

func TestTheTranscriptIsWrittenAgainFromTheCarriedJournal(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","id":"one","name":"tame-impala"}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"tool_call_request","name":"read","render":"draw.go","highlight":{"kind":"focus","value":"draw.go"}}}`,
	)

	held := options(directory)

	stale := filepath.Join(directory, name, "chat.md")
	if err := os.WriteFile(stale, []byte("# what it used to say\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := migrate.Session(held, name); err != nil {
		t.Fatal(err)
	}

	written, err := os.ReadFile(stale) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(written), "what it used to say") {
		t.Error("expected the transcript to be written again rather than left as it was")
	}

	if !strings.Contains(string(written), "draw.go") {
		t.Errorf("expected the transcript to say what the migrated journal says, got %s", written)
	}
}

func TestFormatFiveMigrationAddsEventStatuses(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":5,"id":"one","name":"tame-impala"}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"harness_message","text":"stopped"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:02Z","event":{"kind":"harness_message","text":"broken","failed":true}}`,
		`{"kind":"event","time":"2026-08-01T00:00:03Z","event":{"kind":"tool_call_result","text":"failed","failed":true}}`,
		`{"kind":"event","time":"2026-08-01T00:00:04Z","event":{"kind":"tool_call_result","text":"done"}}`,
	)

	from, err := migrate.Session(options(directory), name)
	if err != nil {
		t.Fatal(err)
	}
	if from != 5 {
		t.Errorf("migrated from format %d, want 5", from)
	}

	storedSession, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(storedSession.Events); got != 2 {
		t.Fatalf("kept %d events, want the tool results the notices left behind: %+v", got, storedSession.Events)
	}
	if got := storedSession.Events[0].Status; got != agent.ErrorStatus {
		t.Errorf("got failed tool status %q", got)
	}
	if got := storedSession.Events[1].Status; got != agent.SuccessStatus {
		t.Errorf("got successful tool status %q", got)
	}
	for index, line := range journalLines(t, directory, name) {
		var event map[string]json.RawMessage
		if index > 0 && json.Unmarshal(line["event"], &event) == nil {
			if _, hasFailed := event["failed"]; hasFailed {
				t.Errorf("line %d kept the legacy failed field: %s", index+1, line["event"])
			}
		}
	}
}

func TestFormatSevenMigrationCountsTheWholeSystemPrompt(t *testing.T) {
	systemPrompt := strings.Repeat("x", 3000)
	directory, name := storedJournal(t,
		fmt.Sprintf(
			`{"kind":"head","time":"2026-08-01T00:00:00Z","version":7,"id":"one","name":"tame-impala","meta":{"system_prompt":%q}}`,
			systemPrompt,
		),
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"startup","state":{"session":"tame-impala","context":[{"name":"SYSTEM.md","bytes":740}],"tools":614}}}`,
	)

	from, err := migrate.Session(options(directory), name)
	if err != nil {
		t.Fatal(err)
	}
	if from != 7 {
		t.Errorf("migrated from format %d, want 7", from)
	}

	state := startupState(t, journalLines(t, directory, name)[1])
	if _, hasFiles := state["context"]; hasFiles {
		t.Errorf("the startup facts kept the context files: %s", state)
	}
	if got := string(state["prompt"]); got != strconv.Itoa(len(systemPrompt)) {
		t.Errorf("got prompt bytes %s, want %d", got, len(systemPrompt))
	}
	if got := string(state["tools"]); got != "614" {
		t.Errorf("got tool bytes %s, want 614", got)
	}
}

func TestFormatSevenMigrationFallsBackToTheContextFilesItHas(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":7,"id":"one","name":"tame-impala"}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"startup","state":{"context":[{"name":"SYSTEM.md","bytes":740},{"name":"AGENTS.md","bytes":260}]}}}`,
	)

	if _, err := migrate.Session(options(directory), name); err != nil {
		t.Fatal(err)
	}

	if got := string(startupState(t, journalLines(t, directory, name)[1])["prompt"]); got != "1000" {
		t.Errorf("got prompt bytes %s, want the 1000 the files came to", got)
	}
}

func TestAnOlderJournalNamingTheBackgroundCapabilityMigratesAllTheWay(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":4,"id":"one","name":"tame-impala","meta":{"system_prompt":"# State\n\n- Background processes are allowed to outlive shell commands\n- The bash tool is granted"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"mode_change","name":"b","text":"rxb"}}`,
	)

	if _, err := migrate.Session(options(directory), name); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	if currentCaps, recorded := caps.LastRecordedMode(storedSession.Events); !recorded || currentCaps != caps.Read|caps.Shell {
		t.Errorf("recovered %s and %t, want rx", currentCaps.Flags(), recorded)
	}
}

func TestFormatEightMigrationForgetsTheBackgroundCapability(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":8,"id":"one","name":"tame-impala"}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"mode_change","text":"rxwb"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:02Z","event":{"kind":"mode_change","name":"b","text":"rxw"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:03Z","event":{"kind":"mode_change","name":"g","text":"rxwbg"}}`,
	)

	from, err := migrate.Session(options(directory), name)
	if err != nil {
		t.Fatal(err)
	}
	if from != 8 {
		t.Errorf("migrated from format %d, want 8", from)
	}

	storedSession, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}

	var modeEvents int
	for _, event := range storedSession.Events {
		if event.Kind == caps.ModeChange {
			modeEvents++
		}
	}
	if modeEvents != 2 {
		t.Errorf("kept %d mode events, want the two that still say something", modeEvents)
	}

	currentCaps, recorded := caps.LastRecordedMode(storedSession.Events)
	if !recorded || currentCaps != caps.Read|caps.Shell|caps.Write|caps.Git {
		t.Errorf("recovered %s and %t, want rxwg", currentCaps.Flags(), recorded)
	}
}

func TestFormatNineMigrationForgetsTheModeASessionWasClosedOn(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":9,"id":"one","name":"tame-impala"}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"mode_change","text":"rxwgs"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:02Z","event":{"kind":"user_message","text":"begin"}}`,
		`{"kind":"item","time":"2026-08-01T00:00:03Z","payload":{"role":"user","content":"begin"}}`,
		`{"kind":"turn_completion","time":"2026-08-01T00:00:04Z"}`,
		`{"kind":"event","time":"2026-08-01T00:00:05Z","event":{"kind":"mode_change","text":"rxgs"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:06Z","event":{"kind":"user_message","text":"The workspace is now read-only."}}`,
	)

	if _, err := migrate.Session(options(directory), name); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	if !storedSession.CanResume() {
		t.Error("a session closed on a mode change was left unresumable")
	}
	if got := len(storedSession.Events); got != 2 {
		t.Errorf("kept %d events, want the two the completed turn holds", got)
	}
	currentCaps, recorded := caps.LastRecordedMode(storedSession.Events)
	if !recorded || currentCaps != caps.Read|caps.Shell|caps.Write|caps.Git|caps.Lookup {
		t.Errorf("recovered %s and %t, want the mode the turn ran in", currentCaps.Flags(), recorded)
	}
}

func TestFormatNineMigrationKeepsAMessageTheModeChangeDoesNotAnnounce(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":9,"id":"one","name":"tame-impala"}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"mode_change","text":"rxwgs"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:02Z","event":{"kind":"user_message","text":"begin"}}`,
		`{"kind":"item","time":"2026-08-01T00:00:03Z","payload":{"role":"user","content":"begin"}}`,
		`{"kind":"turn_completion","time":"2026-08-01T00:00:04Z"}`,
		`{"kind":"event","time":"2026-08-01T00:00:05Z","event":{"kind":"mode_change","text":"rxgs"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:06Z","event":{"kind":"user_message","text":"The .git directory is now read-only."}}`,
	)

	if _, err := migrate.Session(options(directory), name); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	if storedSession.CanResume() {
		t.Error("a message the mode change never said was taken for a notice")
	}
}

func TestFormatNineMigrationKeepsTheMessagesOfACrashedTurn(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":9,"id":"one","name":"tame-impala"}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"mode_change","text":"rxwgs"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:02Z","event":{"kind":"user_message","text":"begin"}}`,
		`{"kind":"item","time":"2026-08-01T00:00:03Z","payload":{"role":"user","content":"begin"}}`,
		`{"kind":"turn_completion","time":"2026-08-01T00:00:04Z"}`,
		`{"kind":"event","time":"2026-08-01T00:00:05Z","event":{"kind":"mode_change","text":"rxgs"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:06Z","event":{"kind":"user_message","text":"The workspace is now read-only."}}`,
		`{"kind":"event","time":"2026-08-01T00:00:07Z","event":{"kind":"user_message","text":"carry on"}}`,
	)

	if _, err := migrate.Session(options(directory), name); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}
	if storedSession.CanResume() {
		t.Error("a crashed turn became resumable")
	}
	if got := len(storedSession.Events); got != 4 {
		t.Errorf("kept %d events, want every one the crashed turn recorded but the notice folded away", got)
	}
}

func TestFormatTenMigrationSpellsPathGrantsWithFlags(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":10,"id":"one","name":"tame-impala"}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"mode_change","text":"rxwgs"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:02Z","event":{"kind":"path_grant_change","state":`+
			`{"grants":[{"path":"/one","access":"read"},{"path":"/two","access":"write"},`+
			`{"path":"/three","access":"exec"}]}}}`,
		`{"kind":"event","time":"2026-08-01T00:00:03Z","event":{"kind":"user_message","text":"begin"}}`,
		`{"kind":"item","time":"2026-08-01T00:00:04Z","payload":{"role":"user","content":"begin"}}`,
		`{"kind":"turn_completion","time":"2026-08-01T00:00:05Z"}`,
	)

	if _, err := migrate.Session(options(directory), name); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}

	restored, found := pathgrant.LastRecorded(storedSession.Events)
	want := []pathgrant.Grant{
		{Path: "/one", Access: pathgrant.ReadAccess},
		{Path: "/three", Access: pathgrant.ReadAccess | pathgrant.ExecAccess},
		{Path: "/two", Access: pathgrant.ReadAccess | pathgrant.WriteAccess},
	}
	if !found || !slices.Equal(restored, want) {
		t.Errorf("recovered %#v and %t", restored, found)
	}
}

func startupState(t *testing.T, line map[string]json.RawMessage) map[string]json.RawMessage {
	t.Helper()

	var event map[string]json.RawMessage
	if err := json.Unmarshal(line["event"], &event); err != nil {
		t.Fatal(err)
	}

	var state map[string]json.RawMessage
	if err := json.Unmarshal(event["state"], &state); err != nil {
		t.Fatal(err)
	}

	return state
}

func TestFormatElevenMigrationKeepsTheFactAndDropsTheProse(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":11,"id":"one","name":"tame-impala"}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"startup","took":1000000}}`,
		`{"kind":"event","time":"2026-08-01T00:00:02Z","event":{"kind":"mode_change","text":"rxwgs"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:03Z","event":{"kind":"user_message","text":"begin"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:04Z","event":{"kind":"interruption","text":"the user pressed escape"}}`,
		`{"kind":"item","time":"2026-08-01T00:00:05Z","payload":{"role":"user","content":"begin"}}`,
		`{"kind":"turn_completion","time":"2026-08-01T00:00:06Z"}`,
		`{"kind":"event","time":"2026-08-01T00:00:07Z","event":{"kind":"mode_change","text":"rxgs"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:08Z","event":{"kind":"user_message","text":"The workspace is now read-only."}}`,
		`{"kind":"event","time":"2026-08-01T00:00:09Z","event":{"kind":"harness_message","status":"warning","text":"`+agent.SilentTurnNotice+`"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:10Z","event":{"kind":"user_message","text":"`+turn.PokeMessage+`"}}`,
		`{"kind":"item","time":"2026-08-01T00:00:11Z","payload":{"role":"user","content":"carry on"}}`,
		`{"kind":"turn_completion","time":"2026-08-01T00:00:12Z"}`,
	)

	if _, err := migrate.Session(options(directory), name); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}

	var kinds []string
	for _, event := range storedSession.Events {
		kinds = append(kinds, string(event.Kind))
	}
	wantKinds := []string{
		string(agent.StartupEvent),
		string(caps.ModeChange),
		string(agent.UserMessageEvent),
		string(agent.InterruptionEvent),
		string(caps.ModeChange),
		string(agent.SilentTurnEvent),
		string(turn.HarnessPoke),
	}
	if !slices.Equal(kinds, wantKinds) {
		t.Errorf("got kinds %q, want %q", kinds, wantKinds)
	}

	for _, event := range storedSession.Events {
		if event.Kind == agent.InterruptionEvent && interrupt.Reason(event) != interrupt.Sentence(interrupt.Escape) {
			t.Errorf("the interruption lost its cause: %+v", event)
		}
		if event.Kind == caps.ModeChange && event.Name == "w" {
			notice, isSaid := caps.ModeNotice(event)
			if !isSaid || !slices.Equal(notice, []string{"Workspace is now read-only."}) {
				t.Errorf("the mode change cannot say itself: %q %t", notice, isSaid)
			}
		}
		isFactOnly := event.Kind == agent.InterruptionEvent ||
			event.Kind == agent.SilentTurnEvent ||
			event.Kind == turn.HarnessPoke
		if isFactOnly && event.Text != "" {
			t.Errorf("prose was kept on %+v", event)
		}
	}
}

func TestFormatTwelveMigrationRenamesTheWebFlagToLookup(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":12,"id":"one","name":"tame-impala"}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"session_startup","took":1000000}}`,
		`{"kind":"event","time":"2026-08-01T00:00:02Z","event":{"kind":"mode_change","state":"rxws"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:03Z","event":{"kind":"user_message","text":"begin"}}`,
		`{"kind":"item","time":"2026-08-01T00:00:04Z","payload":{"role":"user","content":"begin"}}`,
		`{"kind":"turn_completion","time":"2026-08-01T00:00:05Z"}`,
		`{"kind":"event","time":"2026-08-01T00:00:06Z","event":{"kind":"mode_change","name":"s","state":"rxw"}}`,
		`{"kind":"item","time":"2026-08-01T00:00:08Z","payload":{"role":"user","content":"carry on"}}`,
		`{"kind":"turn_completion","time":"2026-08-01T00:00:09Z"}`,
	)

	if _, err := migrate.Session(options(directory), name); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}

	grantedCaps, recorded := caps.LastRecordedMode(storedSession.Events)
	if !recorded || grantedCaps != caps.Read|caps.Shell|caps.Write {
		t.Errorf("recovered %s and %t, want the mode the session was left in", grantedCaps.Flags(), recorded)
	}

	var hasToggle bool

	for _, event := range storedSession.Events {
		if event.Kind != caps.ModeChange {
			continue
		}
		if strings.Contains(string(event.State), "s") {
			t.Errorf("the web flag survived in %+v", event)
		}
		if event.Name == "" {
			continue
		}
		hasToggle = true
		if event.Name != caps.Lookup.Flag() {
			t.Errorf("the toggled capability is still named %q", event.Name)
		}
		if notice, isSaid := caps.ModeNotice(event); !isSaid {
			t.Errorf("the mode change cannot say itself: %q", notice)
		}
	}

	if !hasToggle {
		t.Error("the migrated journal lost the capability it recorded being toggled")
	}
}

func firstFormatJournal() []string {
	return []string{
		`{"kind":"head","time":"2026-08-01T00:00:00Z","id":"one","name":"tame-impala",` +
			`"meta":{"provider":"codex","model":"gpt-5.6-sol","workspaceDir":"/workspace",` +
			`"system_prompt":"# State\n\n- Background processes are allowed to outlive shell commands\n- The bash tool is granted"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"mode_change","name":"b","text":"rxb"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:02Z","event":{"kind":"user_message","text":"look at this"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:03Z","event":{"kind":"tool_call","name":"read","highlight":{"kind":"syntax","value":"a.go"}}}`,
		`{"kind":"event","time":"2026-08-01T00:00:04Z","event":{"kind":"path_grant_change","state":{"grants":[{"path":"/tmp/x","access":"write"}]}}}`,
		`{"kind":"event","time":"2026-08-01T00:00:05Z","event":{"kind":"interruption","text":"the user pressed escape"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:06Z","event":{"kind":"model_message","text":"done"}}`,
	}
}

func TestAJournalMigratesToTheSameBytesWhetherStoredOrArchived(t *testing.T) {
	lines := firstFormatJournal()

	storedDir, name := storedJournal(t, lines...)
	if _, err := migrate.Session(options(storedDir), name); err != nil {
		t.Fatalf("the stored session did not migrate: %v", err)
	}
	storedResult, err := os.ReadFile(filepath.Join(storedDir, name, "session.jsonl")) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	archivedDir, _ := storedJournal(t, lines...)
	if err := store.RebuildMeta(archivedDir, name); err != nil {
		t.Fatal(err)
	}
	if err := session.Archive(archivedDir, name); err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Session(options(archivedDir), name); err != nil {
		t.Fatalf("the archived session did not migrate: %v", err)
	}

	if !session.IsArchived(archivedDir, name) {
		t.Fatal("the migrated session was not archived again")
	}
	archivedResult, err := session.OpenJournal(archivedDir, name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = archivedResult.Close() }()
	archivedBody, err := io.ReadAll(archivedResult)
	if err != nil {
		t.Fatal(err)
	}

	for _, wanted := range []string{
		`"version":13`,
		`"emphasis":{"kind":"syntax","value":"a.go"}`,
		`"access":"rw"`,
		`{"kind":"turn_interruption","name":"escape"}`,
		`{"kind":"mode_change","state":"rx"}`,
	} {
		if !strings.Contains(string(storedResult), wanted) {
			t.Errorf("the chain did not reach %s:\n%s", wanted, storedResult)
		}
	}

	if string(storedResult) != string(archivedBody) {
		t.Errorf("storage changed the migration\n--- stored ---\n%s\n--- archived ---\n%s", storedResult, archivedBody)
	}

	entries, err := session.Entries(archivedDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Format != session.JournalFormat {
		t.Errorf("the archived journal ended at %+v, want format %d", entries, session.JournalFormat)
	}
}
