package migrate_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/contextsource"
	"crdx.org/oh/internal/app/ctl/migrate"
	"crdx.org/oh/internal/app/interrupt"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/store/wire"
	"crdx.org/oh/internal/app/turn"
	"crdx.org/oh/internal/req"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
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
		{Path: "/three", Access: pathgrant.ReadAccess},
		{Path: "/two", Access: pathgrant.ReadAccess | pathgrant.WriteAccess},
	}
	if !found || !slices.Equal(restored, want) {
		t.Errorf("recovered %#v and %t", restored, found)
	}
}

func TestFormatSixteenMigrationMakesExecutionImplicitInTemporaryPathGrants(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":16,"id":"one","name":"tame-impala"}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"path_grant_change","state":`+
			`{"grants":[{"path":"/one","access":"r"},{"path":"/two","access":"rx"},`+
			`{"path":"/three","access":"rw"},{"path":"/four","access":"rxw"}]}}}`,
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
		{Path: "/four", Access: pathgrant.ReadAccess | pathgrant.WriteAccess},
		{Path: "/one", Access: pathgrant.ReadAccess},
		{Path: "/three", Access: pathgrant.ReadAccess | pathgrant.WriteAccess},
		{Path: "/two", Access: pathgrant.ReadAccess},
	}
	if !found || !slices.Equal(restored, want) {
		t.Errorf("recovered %#v and %t", restored, found)
	}
}

func TestFormatSeventeenMigrationStoresContextSourceKindsRatherThanTheirNames(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":17,"id":"one","name":"tame-impala","meta":{`+
			`"system_context_files":[{"path":"/config/SYSTEM.md","estimated_tokens":700}],`+
			`"system_context_sources":[{"name":"harness","estimated_tokens":3379}],`+
			`"session_context_sources":[{"name":"skill catalogue (25 skills)","estimated_tokens":3276},`+
			`{"name":"skill catalogue (1 skill)","estimated_tokens":30},`+
			`{"name":"2 skill definitions","estimated_tokens":60},`+
			`{"name":"harness instructions","estimated_tokens":10}]}}`,
	)

	if _, err := migrate.Session(options(directory), name); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}

	wantFiles := []contextsource.Source{{Path: "/config/SYSTEM.md", EstimatedTokens: 700}}
	if !slices.Equal(storedSession.Meta.SystemContextFiles, wantFiles) {
		t.Errorf("got system files %#v", storedSession.Meta.SystemContextFiles)
	}
	wantSystem := []contextsource.Source{{Kind: contextsource.HarnessInstructions, EstimatedTokens: 3379}}
	if !slices.Equal(storedSession.Meta.SystemContextSources, wantSystem) {
		t.Errorf("got system sources %#v", storedSession.Meta.SystemContextSources)
	}
	wantSession := []contextsource.Source{
		{Kind: contextsource.SkillDefinitions, Count: 25, EstimatedTokens: 3276},
		{Kind: contextsource.SkillDefinitions, Count: 1, EstimatedTokens: 30},
		{Kind: contextsource.SkillDefinitions, Count: 2, EstimatedTokens: 60},
		{Kind: contextsource.HarnessInstructions, EstimatedTokens: 10},
	}
	if !slices.Equal(storedSession.Meta.SessionContextSources, wantSession) {
		t.Errorf("got session sources %#v", storedSession.Meta.SessionContextSources)
	}
}

func TestFormatSeventeenMigrationRefusesAContextSourceItDoesNotKnow(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":17,"id":"one","name":"tame-impala","meta":{`+
			`"system_context_sources":[{"name":"something else","estimated_tokens":10}]}}`,
	)

	if _, err := migrate.Session(options(directory), name); err == nil {
		t.Error("an unknown context source was migrated")
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
			if !isSaid || !slices.Equal(notice, []string{"The workspace is now read-only."}) {
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

func TestFormatFifteenMigrationCompletesToolCallRenderings(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":14,"id":"one","name":"tame-impala"}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"tool_call_request","id":"bash","name":"bash","arguments":"{\"command\":\"echo hi\"}","render":"echo hi"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:02Z","event":{"kind":"tool_call_request","id":"read","name":"read","arguments":"{\"path\":\"main.go\",\"offset\":10,\"limit\":5}","render":"main.go","detail":"10-14"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:02Z","event":{"kind":"tool_call_request","id":"read-open","name":"read","arguments":"{\"path\":\"main.go\",\"offset\":10}","render":"main.go","detail":"10+"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:02Z","event":{"kind":"tool_call_request","id":"read-limit","name":"read","arguments":"{\"path\":\"main.go\",\"limit\":5}","render":"main.go","detail":"1-5"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:03Z","event":{"kind":"tool_call_request","id":"skill","name":"read","arguments":"{\"path\":\"/skills/golang/SKILL.md\"}","render":"/skills/golang/SKILL.md","emphasis":{"kind":"focus","value":"SKILL.md"}}}`,
		`{"kind":"event","time":"2026-08-01T00:00:04Z","event":{"kind":"tool_call_request","id":"job-start","name":"job","arguments":"{\"action\":\"start\",\"name\":\"docs\",\"command\":\"serve\"}","render":"docs","continuation":[{"name":"bash","render":"serve"}]}}`,
		`{"kind":"event","time":"2026-08-01T00:00:05Z","event":{"kind":"tool_call_request","id":"job-restart","name":"job","arguments":"{\"action\":\"start\",\"name\":\"docs\"}","render":"docs","detail":"start"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:06Z","event":{"kind":"tool_call_request","id":"job-wait","name":"job","arguments":"{\"action\":\"wait\",\"names\":[\"build\",\"lint\"],\"wait_for\":\"all\",\"wait_seconds\":20}","render":"build, lint","detail":"wait"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:07Z","event":{"kind":"tool_call_request","id":"job-list","name":"job","arguments":"{\"action\":\"list\"}","detail":"list"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:08Z","event":{"kind":"tool_call_request","id":"job-stop","name":"job","arguments":"{\"action\":\"stop\",\"name\":\"docs\"}","render":"docs","detail":"stop"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:08Z","event":{"kind":"tool_call_request","id":"job-status","name":"job","arguments":"{\"action\":\"status\",\"name\":\"docs\"}","render":"docs","detail":"status"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:08Z","event":{"kind":"tool_call_request","id":"job-output","name":"job","arguments":"{\"action\":\"output\",\"name\":\"docs\"}","render":"docs","detail":"output"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:08Z","event":{"kind":"tool_call_request","id":"job-discard","name":"job","arguments":"{\"action\":\"discard\",\"name\":\"docs\"}","render":"docs","detail":"discard"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:08Z","event":{"kind":"tool_call_request","id":"job-prune","name":"job","arguments":"{\"action\":\"prune\"}","detail":"prune"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:08Z","event":{"kind":"tool_call_request","id":"job-wait-any","name":"job","arguments":"{\"action\":\"wait\",\"name\":\"docs\"}","render":"docs","detail":"wait"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:09Z","event":{"kind":"tool_call_request","id":"expose-plain","name":"expose","arguments":"{\"action\":\"add\",\"port\":3000}","render":"3000"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:09Z","event":{"kind":"tool_call_request","id":"expose","name":"expose","arguments":"{\"action\":\"add\",\"port\":8000,\"job_name\":\"docs\"}","render":"8000"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:10Z","event":{"kind":"tool_call_request","id":"unexpose","name":"expose","arguments":"{\"action\":\"remove\",\"port\":8000}","render":"8000","detail":"remove"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:11Z","event":{"kind":"tool_call_request","id":"expose-list","name":"expose","arguments":"{\"action\":\"list\"}","detail":"list"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:12Z","event":{"kind":"tool_call_request","id":"lookup","name":"lookup","arguments":"{\"query\":\"Go\"}","render":"Go"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:13Z","event":{"kind":"tool_call_request","id":"fetch","name":"fetch","arguments":"{\"url\":\"https://example.test\",\"type\":\"markdown\"}","render":"https://example.test","detail":"markdown"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:14Z","event":{"kind":"tool_call_request","id":"notify","name":"notify","arguments":"{\"title\":\"Done\",\"message\":\"All green\"}","render":"Done","detail":"All green"}}`,
	)

	if _, err := migrate.Session(options(directory), name); err != nil {
		t.Fatal(err)
	}
	storedSession, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}

	calls := make(map[string]agent.Event)
	for _, event := range storedSession.Events {
		if event.Kind == agent.ToolCallRequestEvent {
			calls[event.ID] = event
		}
	}

	assertRendering := func(id string, renderingKind string, subject string, detail string) {
		t.Helper()
		event := calls[id]
		if event.RenderingKind != renderingKind || event.Subject != subject || event.Note != detail {
			t.Errorf("%s migrated to %+v", id, event.FallbackRendering)
		}
	}
	assertRendering("bash", "", "echo hi", "")
	if !calls["bash"].ShowOutput {
		t.Error("bash no longer says to show its output")
	}
	assertRendering("read", "", "main.go", "10-14")
	for id, pathLine := range map[string]string{"read": "10-14", "read-open": "10+", "read-limit": "1-5"} {
		if calls[id].PathLine != pathLine {
			t.Errorf("%s path line is %q, want %q", id, calls[id].PathLine, pathLine)
		}
	}
	assertRendering("skill", "skill", "/skills/golang/SKILL.md", "")
	if calls["skill"].Emphasis.Value != "golang" {
		t.Errorf("skill emphasis is %+v", calls["skill"].Emphasis)
	}
	assertRendering("job-start", "job_start", "docs", "")
	continuation := calls["job-start"].Continuation
	if len(continuation) != 1 || continuation[0].Kind != "bash" {
		t.Errorf("job continuation is %+v", continuation)
	}
	assertRendering("job-restart", "job_restart", "docs", "")
	assertRendering("job-wait", "job_wait_all", "build && lint", "up to 20s")
	assertRendering("job-list", "job_list", "jobs", "")
	assertRendering("job-stop", "job_stop", "docs", "")
	assertRendering("job-status", "job_status", "docs", "")
	assertRendering("job-output", "job_output", "docs", "")
	assertRendering("job-discard", "job_discard", "docs", "")
	assertRendering("job-prune", "job_prune", "jobs", "")
	assertRendering("job-wait-any", "job_wait_any", "docs", "")
	assertRendering("expose-plain", "forward_add", "3000", "")
	assertRendering("expose", "forward_add", "docs:8000", "")
	assertRendering("unexpose", "forward_remove", "8000", "")
	assertRendering("expose-list", "forward_list", "forwards", "")
	assertRendering("lookup", "", "Go", "")
	assertRendering("fetch", "", "https://example.test", "as markdown")
	assertRendering("notify", "", "Done", "— All green")
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

func TestFormatFifteenMigrationSaysForwardWhereItSaidExpose(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":15,"id":"one","name":"tame-impala"}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"tool_call_request","id":"add","name":"expose","arguments":"{\"action\":\"add\",\"port\":3000}","rendering_kind":"expose_add","render":"3000"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:02Z","event":{"kind":"tool_call_request","id":"remove","name":"expose","arguments":"{\"action\":\"remove\",\"port\":3000}","rendering_kind":"expose_remove","render":"3000"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:03Z","event":{"kind":"tool_call_request","id":"list","name":"expose","arguments":"{\"action\":\"list\"}","rendering_kind":"expose_list","render":"exposed ports"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:04Z","event":{"kind":"tool_call_request","id":"malformed","name":"expose","arguments":"{}","rendering_kind":"expose"}}`,
		`{"kind":"event","time":"2026-08-01T00:00:05Z","event":{"kind":"tool_call_request","id":"bash","name":"bash","arguments":"{\"command\":\"ls\"}","rendering_kind":"bash","render":"ls"}}`,
	)

	if _, err := migrate.Session(options(directory), name); err != nil {
		t.Fatal(err)
	}
	storedSession, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}

	calls := make(map[string]agent.Event)
	for _, event := range storedSession.Events {
		calls[event.ID] = event
	}
	for id, want := range map[string]string{
		"add":       "forward_add",
		"remove":    "forward_remove",
		"list":      "forward_list",
		"malformed": "forward",
		"bash":      "bash",
	} {
		if got := calls[id].RenderingKind; got != want {
			t.Errorf("%s migrated to rendering kind %q, want %q", id, got, want)
		}
	}
	if got := calls["list"].Subject; got != "forwards" {
		t.Errorf("the list migrated to subject %q, want %q", got, "forwards")
	}
}

func TestFormatFifteenMigrationForgetsHostLoopbackPorts(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":15,"id":"one","name":"tame-impala"}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"sandbox_to_host_change","name":"3000","state":{"ports":[3000]}}}`,
		`{"kind":"item","time":"2026-08-01T00:00:01Z","payload":{"role":"user","content":[{"type":"text","text":"TCP port 3000 on the host loopback is now reachable at 127.0.0.1:3000 from the sandbox."}]}}`,
		`{"kind":"event","time":"2026-08-01T00:00:02Z","event":{"kind":"port_grant_change","name":"8080","state":{"host":"127.9.9.9","ports":[8080]}}}`,
		`{"kind":"event","time":"2026-08-01T00:00:03Z","event":{"kind":"sandbox_to_host_change","name":"3000","state":{"ports":[]}}}`,
	)

	from, err := migrate.Session(options(directory), name)
	if err != nil {
		t.Fatal(err)
	}
	if from != 15 {
		t.Errorf("migrated from format %d, want 15", from)
	}

	storedSession, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}

	kinds := make([]agent.Kind, 0, len(storedSession.Events))
	for _, event := range storedSession.Events {
		kinds = append(kinds, event.Kind)
	}
	if want := []agent.Kind{portgrant.ForwardChange}; !slices.Equal(kinds, want) {
		t.Errorf("kept %q, want only %q", kinds, want)
	}

	body, err := os.ReadFile(filepath.Join(directory, name, "session.jsonl")) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"port_grant_change"`) || !strings.Contains(string(body), `"kind":"item"`) {
		t.Errorf("the migration dropped more than the host loopback events:\n%s", body)
	}
	if strings.Contains(string(body), "sandbox_to_host_change") {
		t.Errorf("the migration kept a host loopback event:\n%s", body)
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
		fmt.Sprintf(`"version":%d`, session.JournalFormat),
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

func formatBeforeCompressedWire() string {
	return `{"kind":"head","time":"2026-08-01T00:00:00Z","version":13,"id":"one","name":"tame-impala","meta":{"workspaceDir":"/workspace"}}`
}

func plainWireTranscript() string {
	body := `{"messages":["` + strings.Repeat("the same conversation again ", 20000) + `"]}`
	return "# HTTP transcript\n# session: tame-impala\n\n" +
		"# exchange 6 start 2026-08-01T00:00:01Z\n> POST https://example.test/ HTTP/1.1\n\n" + body + "\n" +
		"# exchange 6 end 2026-08-01T00:00:02Z elapsed=1s completed\n\n" +
		"# exchange 7 start 2026-08-01T00:00:03Z\n> POST https://example.test/ HTTP/1.1\n\n" + body + "\n" +
		"# exchange 7 read 2026-08-01T00:00:04Z elapsed=1s bytes=10\ndata: one\n" +
		"# exchange 7 end 2026-08-01T00:00:05Z elapsed=2s completed\n\n"
}

func storedWithPlainWire(t *testing.T) (string, string) {
	t.Helper()

	directory, name := storedJournal(t, formatBeforeCompressedWire())
	if err := os.WriteFile(filepath.Join(directory, name, "wire.http"), []byte(plainWireTranscript()), 0o600); err != nil {
		t.Fatal(err)
	}

	return directory, name
}

func decompressedWire(t *testing.T, path string) string {
	t.Helper()

	file, err := os.Open(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()

	decoder, err := zstd.NewReader(file, zstd.WithDecoderMaxWindow(128<<20))
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()

	transcript, err := io.ReadAll(decoder)
	if err != nil {
		t.Fatalf("%s does not decompress: %v", path, err)
	}

	return string(transcript)
}

func TestAPlainWireTranscriptIsCompressedWithoutLosingAByte(t *testing.T) {
	directory, name := storedWithPlainWire(t)

	if _, err := migrate.Session(options(directory), name); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(directory, name, "wire.http")); !os.IsNotExist(err) {
		t.Errorf("expected the plain transcript to be gone: %v", err)
	}

	compressedPath := filepath.Join(directory, name, "wire.http.zst")
	if decompressedWire(t, compressedPath) != plainWireTranscript() {
		t.Error("expected the compressed transcript to hold every byte of the plain one")
	}

	info, err := os.Stat(compressedPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected the compressed transcript to be private, got %o", info.Mode().Perm())
	}
	if info.Size()*100 > int64(len(plainWireTranscript())) {
		t.Errorf("expected the repeated request to cost almost nothing, got %d bytes of %d", info.Size(), len(plainWireTranscript()))
	}

	kept, err := os.ReadFile(filepath.Join(options(directory).BackupDir, name, "wire.http")) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != plainWireTranscript() {
		t.Error("expected the copy to keep the plain transcript as it stood")
	}
}

func TestRecordingCarriesOnFromAMigratedWireTranscript(t *testing.T) {
	directory, name := storedWithPlainWire(t)

	if _, err := migrate.Session(options(directory), name); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(directory, name, "wire.http.zst")
	recorder, err := wire.Open(path, wire.Meta{}, func(err error) {
		t.Errorf("unexpected recorder failure: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder.Start(req.Request{StartedAt: time.Unix(9, 0), Method: http.MethodPost}).Finish(time.Unix(10, 0), nil, false)
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	transcript := decompressedWire(t, path)
	if !strings.HasPrefix(transcript, plainWireTranscript()) {
		t.Error("expected the migrated transcript to stand unchanged ahead of the new exchange")
	}
	if !strings.Contains(transcript, "# exchange 8 start") {
		t.Errorf("expected numbering to carry on from the migrated transcript, got:\n%s", transcript[len(plainWireTranscript()):])
	}
}

func TestASessionWithoutAWireTranscriptMigratesWithoutOne(t *testing.T) {
	directory, name := storedJournal(t, formatBeforeCompressedWire())

	if _, err := migrate.Session(options(directory), name); err != nil {
		t.Fatal(err)
	}

	for _, wireName := range []string{"wire.http", "wire.http.zst"} {
		if _, err := os.Stat(filepath.Join(directory, name, wireName)); !os.IsNotExist(err) {
			t.Errorf("expected no %s: %v", wireName, err)
		}
	}
}

func TestTheCopyOfABundleSharesItsFilesRatherThanDuplicatingThem(t *testing.T) {
	directory, name := storedJournal(t, `{"kind":"head","time":"2026-08-01T00:00:00Z","id":"one","name":"tame-impala"}`)
	dropPath := filepath.Join(directory, name, "drops", "picture.png")
	if err := os.MkdirAll(filepath.Dir(dropPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dropPath, []byte("picture"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := migrate.Session(options(directory), name); err != nil {
		t.Fatal(err)
	}

	stored, err := os.Stat(dropPath)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := os.Stat(filepath.Join(options(directory).BackupDir, name, "drops", "picture.png"))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(stored, kept) {
		t.Error("expected the copy to be a link to the file it keeps")
	}
}

func TestAnArchivedSessionIsKeptAsItsArchive(t *testing.T) {
	directory, name := storedJournal(t, `{"kind":"head","time":"2026-08-01T00:00:00Z","id":"one","name":"tame-impala"}`)
	if err := session.Archive(directory, name); err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(session.ArchivePath(directory, name))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := migrate.Session(options(directory), name); err != nil {
		t.Fatal(err)
	}

	backupDir := options(directory).BackupDir
	kept, err := os.ReadFile(filepath.Join(backupDir, name+session.ArchiveSuffix)) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(kept, archive) {
		t.Error("expected the archive as it stood before to be kept")
	}
	if _, err := os.Stat(filepath.Join(backupDir, name)); !os.IsNotExist(err) {
		t.Errorf("expected the archive alone to be kept, not the bundle unpacked from it: %v", err)
	}
	if !session.IsArchived(directory, name) {
		t.Error("expected the migrated session to be archived again")
	}
}

func TestAnArchivedSessionAlreadyCurrentKeepsNoCopy(t *testing.T) {
	head := fmt.Sprintf(`{"kind":"head","time":"2026-08-01T00:00:00Z","version":%d,"id":"one","name":"tame-impala"}`, session.JournalFormat)
	directory, name := storedJournal(t, head)
	if err := store.RebuildMeta(directory, name); err != nil {
		t.Fatal(err)
	}
	if err := session.Archive(directory, name); err != nil {
		t.Fatal(err)
	}

	if _, err := migrate.Session(options(directory), name); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(options(directory).BackupDir, name+session.ArchiveSuffix)); !os.IsNotExist(err) {
		t.Errorf("expected no copy of a session that needed nothing: %v", err)
	}
}

func TestFormatElevenMigrationForgetsATotalThatSaysNothingMore(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":11,"id":"one","name":"tame-impala"}`,
		`{"kind":"event","time":"2026-08-01T00:00:01Z","event":{"kind":"tool_call","stats":{"bytes":10,"total_bytes":10}}}`,
		`{"kind":"event","time":"2026-08-01T00:00:02Z","event":{"kind":"tool_call","stats":{"bytes":10,"total_bytes":20}}}`,
	)

	if _, err := migrate.Session(options(directory), name); err != nil {
		t.Fatal(err)
	}

	var measurements []string
	for _, line := range journalLines(t, directory, name) {
		var event struct {
			Stats json.RawMessage `json:"stats"`
		}
		if raw, hasEvent := line["event"]; hasEvent {
			if err := json.Unmarshal(raw, &event); err != nil {
				t.Fatal(err)
			}
			measurements = append(measurements, string(event.Stats))
		}
	}

	want := []string{`{"bytes":10}`, `{"bytes":10,"total_bytes":20}`}
	if !slices.Equal(measurements, want) {
		t.Errorf("got %q, want %q", measurements, want)
	}
}

func TestEveryFormatThatReadsEventsRefusesOneItCannotRead(t *testing.T) {
	for _, testCase := range []struct {
		version int
		event   string
	}{
		{version: 1, event: `"not an event"`},
		{version: 5, event: `"not an event"`},
		{version: 10, event: `"not an event"`},
		{version: 11, event: `"not an event"`},
		{version: 11, event: `{"kind":7}`},
		{version: 11, event: `{"kind":"tool_call","stats":"not measurements"}`},
		{version: 11, event: `{"kind":"user_message","text":7}`},
		{version: 12, event: `"not an event"`},
		{version: 15, event: `"not an event"`},
	} {
		t.Run(fmt.Sprintf("%d %s", testCase.version, testCase.event), func(t *testing.T) {
			directory, name := storedJournal(t,
				fmt.Sprintf(`{"kind":"head","time":"2026-08-01T00:00:00Z","version":%d,"id":"one","name":"tame-impala"}`, testCase.version),
				`{"kind":"event","time":"2026-08-01T00:00:01Z","event":`+testCase.event+`}`,
			)

			if _, err := migrate.Session(options(directory), name); err == nil {
				t.Error("an unreadable event was migrated")
			}
		})
	}
}

func TestFormatEighteenMigrationStoresTheIntentInTheRendering(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":18,"id":"one","name":"tame-impala","meta":{}}`,
		`{"kind":"event","event":{"kind":"tool_call_request","id":"1","name":"bash",`+
			`"arguments":"{\"command\":\"go test ./...\",\"intent\":\"Run   every test\"}"}}`,
		`{"kind":"event","event":{"kind":"tool_call_request","id":"2","name":"bash",`+
			`"arguments":"{\"command\":\"true\",\"intent\":\"API checks only\"}"}}`,
		`{"kind":"event","event":{"kind":"tool_call_request","id":"3","name":"bash",`+
			`"arguments":"{\"command\":\"true\",\"intent\":\"  \"}"}}`,
		`{"kind":"event","event":{"kind":"tool_call_request","id":"4","name":"job",`+
			`"arguments":"{\"action\":\"start\",\"name\":\"docs\",\"command\":\"just docs\",\"intent\":\"serve the docs\"}"}}`,
		`{"kind":"event","event":{"kind":"tool_call_request","id":"5","name":"job",`+
			`"arguments":"{\"action\":\"stop\",\"name\":\"docs\",\"intent\":\"stop the docs\"}"}}`,
		`{"kind":"event","event":{"kind":"tool_call_request","id":"6","name":"read",`+
			`"arguments":"{\"path\":\"README.md\",\"intent\":\"read it\"}"}}`,
	)

	if _, err := migrate.Session(options(directory), name); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{"1": "Run every test", "2": "API checks only", "4": "Serve the docs"}
	for _, event := range storedSession.Events {
		if event.Kind != agent.ToolCallRequestEvent {
			continue
		}
		if event.Intent != want[event.ID] {
			t.Errorf("call %s stored the intent %q, want %q", event.ID, event.Intent, want[event.ID])
		}
	}
	for _, event := range storedSession.Events {
		switch event.ID {
		case "4":
			if event.Introduces != "docs" {
				t.Errorf("the start introduced %q, want docs", event.Introduces)
			}
		case "5":
			if !slices.Equal(event.Mentions, []string{"docs"}) {
				t.Errorf("the stop mentioned %q, want docs", event.Mentions)
			}
		}
	}
}

func TestFormatNineteenMigrationRecordsTheWireAnOpenCodeGoSessionWasSpokenOver(t *testing.T) {
	for _, test := range []struct {
		choice string
		want   agent.Wire
	}{
		{`{"provider":"opencode-go","id":"deepseek-v4-pro"}`, agent.CompletionsWire},
		{`{"provider":"opencode-go","id":"grok-4.7"}`, agent.ResponsesWire},
		{`{"provider":"opencode-go","id":"muse-spark-1.3-contributor"}`, agent.ResponsesWire},
		{`{"provider":"opencode-go","id":"gpt-6-luna"}`, agent.ResponsesWire},
		{`{"provider":"opencode-go","id":"minimax-m3"}`, agent.MessagesWire},
		{`{"provider":"opencode-go","id":"qwen3.8-max"}`, agent.MessagesWire},
		{`{"provider":"opencode-go","id":"qwen3.8-max","wire":"completions"}`, agent.CompletionsWire},
		{`{"provider":"anthropic","id":"claude-opus-5"}`, ""},
	} {
		directory, name := storedJournal(t,
			`{"kind":"head","time":"2026-08-01T00:00:00Z","version":19,"id":"one","name":"tame-impala",`+
				`"meta":{"provider":"opencode-go","model":"m","model_choice":`+test.choice+`}}`,
		)

		if _, err := migrate.Session(options(directory), name); err != nil {
			t.Fatal(err)
		}

		storedSession, err := store.Read(directory, name)
		if err != nil {
			t.Fatal(err)
		}

		if got := storedSession.Meta.ModelChoice.Wire; got != test.want {
			t.Errorf("%s: stored the wire %q, want %q", test.choice, got, test.want)
		}
	}
}

func TestFormatNineteenMigrationLeavesASessionHoldingNoModelChoiceAlone(t *testing.T) {
	directory, name := storedJournal(t,
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":19,"id":"one","name":"tame-impala",`+
			`"meta":{"provider":"opencode-go","model":"deepseek-v4-pro"}}`,
	)

	if _, err := migrate.Session(options(directory), name); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, name)
	if err != nil {
		t.Fatal(err)
	}

	if storedSession.Meta.ModelChoice != nil {
		t.Errorf("expected no model choice, got %+v", storedSession.Meta.ModelChoice)
	}
}
