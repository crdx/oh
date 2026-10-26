package harness

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/klauspost/compress/zstd"

	"crdx.org/oh/internal/app/backend"
	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/ctl/migrate"
	"crdx.org/oh/internal/app/environment"
	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/prompt"
	"crdx.org/oh/internal/app/sessions"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/sim"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
)

type inProcessOpenCodeRequests struct {
	mutex  sync.Mutex
	paths  []string
	keys   []string
	ids    []string
	bodies []map[string]any
}

type inProcessOpenCodeServer struct {
	endpoint *sim.Endpoint
	server   *httptest.Server
	address  string
	requests *inProcessOpenCodeRequests
}

type inProcessOpenCodeSnapshot struct {
	paths  []string
	keys   []string
	ids    []string
	bodies []map[string]any
}

func newInProcessOpenCodeServer(t *testing.T, scenario *sim.Scenario, keyHeader string) *inProcessOpenCodeServer {
	t.Helper()

	endpoint := sim.New(scenario)
	requests := &inProcessOpenCodeRequests{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Opencode-Session") != "" {
			raw, err := sim.ReadBody(request)
			if err != nil {
				http.Error(writer, err.Error(), http.StatusBadRequest)
				return
			}
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			requests.mutex.Lock()
			requests.paths = append(requests.paths, request.URL.Path)
			requests.keys = append(requests.keys, request.Header.Get(keyHeader))
			requests.ids = append(requests.ids, request.Header.Get("X-Opencode-Session"))
			requests.bodies = append(requests.bodies, body)
			requests.mutex.Unlock()
			request.Header.Del("Content-Encoding")
			request.Body = io.NopCloser(bytes.NewReader(raw))
		}
		endpoint.ServeHTTP(writer, request)
	}))
	t.Cleanup(server.Close)

	return &inProcessOpenCodeServer{
		endpoint: endpoint,
		server:   server,
		address:  endpoint.Addresses(server.URL)[sim.Completions],
		requests: requests,
	}
}

func (self *inProcessOpenCodeServer) snapshot() inProcessOpenCodeSnapshot {
	self.requests.mutex.Lock()
	defer self.requests.mutex.Unlock()
	return inProcessOpenCodeSnapshot{
		paths:  slices.Clone(self.requests.paths),
		keys:   slices.Clone(self.requests.keys),
		ids:    slices.Clone(self.requests.ids),
		bodies: slices.Clone(self.requests.bodies),
	}
}

func inProcessOpenCodeChoice(modelID string, wire string, isEffortless bool) model.Choice {
	choice := model.Choice{
		Provider:        model.OpencodeGoProvider,
		ID:              modelID,
		MaxOutputTokens: 128000,
		Wire:            agent.Wire(wire),
	}
	if !isEffortless {
		choice.EffortLevels = []string{"high"}
	}
	return choice
}

func inProcessOpenCodeKeyHeader(wire string) (string, string) {
	switch wire {
	case sim.Completions, sim.Responses:
		return "Authorization", "Bearer stand-in"
	case sim.Messages:
		return "X-Api-Key", "stand-in"
	case sim.GenerateContent:
		return "X-Goog-Api-Key", "stand-in"
	default:
		panic("unknown OpenCode wire " + wire)
	}
}

func inProcessSystemPrompt(t *testing.T, writer *store.Writer, sessionDirectory string) string {
	t.Helper()

	workspace := work.At(t.TempDir())
	if err := workspace.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	globalContext := filepath.Join(t.TempDir(), "SYSTEM.md")
	config := prompt.Config{
		GlobalPath:  globalContext,
		Workspace:   workspace,
		SessionName: writer.Name(),
		SessionsDir: filepath.Dir(sessionDirectory),
		SessionDir:  sessionDirectory,
		ConfigFile:  filepath.Join(t.TempDir(), "config.toml"),
		TmpDir:      t.TempDir(),
		HomeDir:     t.TempDir(),
		CurrentCaps: caps.Unconfined(),
		Yolo:        true,
		Notes:       agent.NoteFormatOf(nil),
	}
	system, _, err := prompt.Load(config)
	if err != nil {
		t.Fatal(err)
	}
	return system
}

func runInProcessOpenCodeTurn(
	t *testing.T,
	endpoint *inProcessOpenCodeServer,
	sessionsDirectory string,
	storedSession *store.Session,
	choice model.Choice,
	selection model.Selection,
	message string,
	shouldSkipWire bool,
) (*store.Session, string, int64) {
	t.Helper()

	meta := store.Meta{
		Model:          choice.ID,
		Provider:       choice.Provider,
		Effort:         selection.Effort,
		ModelChoice:    &choice,
		WorkspaceDir:   t.TempDir(),
		ShouldSkipWire: shouldSkipWire,
		Yolo:           true,
	}
	writer, err := sessions.OpenWriter(sessionsDirectory, storedSession, meta)
	if err != nil {
		t.Fatal(err)
	}
	sessionDirectory := filepath.Join(sessionsDirectory, writer.Name())
	system := inProcessSystemPrompt(t, writer, sessionDirectory)
	meta.SystemPrompt = system
	if storedSession == nil {
		wireLogPath := ""
		if writer.RecordsWire() {
			wireLogPath = "debug/wire.http.zst"
		}
		capturedEnvironment := environment.Capture(shell.Paths{}, nil, false, "", wireLogPath)
		meta.Environment = &capturedEnvironment
		if err := writer.SetMeta(meta); err != nil {
			t.Fatal(err)
		}
	} else {
		system = storedSession.Meta.SystemPrompt
	}

	connection, err := backend.Connect(choice, selection, backend.EndpointSettings{OverrideURL: endpoint.address})
	if err != nil {
		_ = writer.Close()
		t.Fatal(err)
	}
	connection.UseSession(writer.ID())
	if storedSession != nil {
		connection.Load(storedSession.Items)
	}
	connection.Configure(system, nil)
	connection.ObserveHTTP(writer.Observer())
	connection.AddUserMessage(message)
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: message}); err != nil {
		_ = writer.Close()
		t.Fatal(err)
	}

	var answer strings.Builder
	_, err = connection.Send(t.Context(), func(output agent.Output) bool {
		if output.Kind == agent.ModelMessageEvent {
			answer.WriteString(output.Text)
		}
		return true
	})
	if err != nil {
		_ = writer.Close()
		t.Fatal(err)
	}
	state := connection.Dump()
	for _, item := range state {
		if err := writer.Item(item); err != nil {
			_ = writer.Close()
			t.Fatal(err)
		}
	}
	if err := writer.Event(agent.Event{Kind: agent.ModelMessageEvent, Text: answer.String()}); err != nil {
		_ = writer.Close()
		t.Fatal(err)
	}
	if err := writer.CompleteTurn(session.TurnSummary{}); err != nil {
		_ = writer.Close()
		t.Fatal(err)
	}
	name := writer.Name()
	wireSize := fileSizeOrZero(t, filepath.Join(sessionDirectory, "debug", "wire.http.zst"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	stored, err := store.Read(sessionsDirectory, name)
	if err != nil {
		t.Fatal(err)
	}
	return stored, answer.String(), wireSize
}

func inProcessWireTranscript(t *testing.T, path string) string {
	t.Helper()
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	stored, err := root.ReadFile(filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := zstd.NewReader(bytes.NewReader(stored), zstd.WithDecoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	transcript, err := io.ReadAll(decoder)
	if err != nil {
		t.Fatal(err)
	}
	return string(transcript)
}

func TestInProcessASessionResumesWithItsStoredModelAfterTheListForgetsIt(t *testing.T) {
	endpoint := newInProcessOpenCodeServer(t, &sim.Scenario{Model: "fake", Turns: []sim.Turn{{Say: "First answer."}, {Say: "Second answer."}}}, "Authorization")
	sessionsDirectory := t.TempDir()
	choice := inProcessOpenCodeChoice("fake", sim.Completions, false)
	selection := model.Selection{Provider: choice.Provider, Model: choice.ID, Effort: "high"}
	stored, first, _ := runInProcessOpenCodeTurn(t, endpoint, sessionsDirectory, nil, choice, selection, "first question", false)
	if !strings.Contains(first, "First answer.") {
		t.Errorf("expected the first answer, got %q", first)
	}
	frozenChoice := stored.Meta.ModelChoice
	if frozenChoice == nil || frozenChoice.Provider != model.OpencodeGoProvider || frozenChoice.ID != "fake" {
		t.Fatalf("expected the session to hold the model it was created with, got %+v", frozenChoice)
	}
	resumedChoice, err := sessions.ModelChoice(stored, []model.Choice{inProcessOpenCodeChoice("fake-2", sim.Completions, false)}, filepath.Join(t.TempDir(), "seen_models.json"), sessions.ModelSelection(stored))
	if err != nil {
		t.Fatal(err)
	}
	_, second, _ := runInProcessOpenCodeTurn(t, endpoint, sessionsDirectory, stored, resumedChoice, sessions.ModelSelection(stored), "second question", false)
	if !strings.Contains(second, "Second answer.") {
		t.Errorf("expected the session to resume on its own model, got %q", second)
	}
}

func TestInProcessASessionResumesOverItsStoredWireAfterTheListMovesIt(t *testing.T) {
	endpoint := newInProcessOpenCodeServer(t, &sim.Scenario{Model: "fake", Wire: sim.Messages, Turns: []sim.Turn{{Say: "First answer."}, {Say: "Second answer."}}}, "X-Api-Key")
	sessionsDirectory := t.TempDir()
	choice := inProcessOpenCodeChoice("fake", sim.Messages, false)
	selection := model.Selection{Provider: choice.Provider, Model: choice.ID, Effort: "high"}
	stored, first, _ := runInProcessOpenCodeTurn(t, endpoint, sessionsDirectory, nil, choice, selection, "first question", false)
	if !strings.Contains(first, "First answer.") {
		t.Errorf("expected the first answer, got %q", first)
	}
	if frozenChoice := stored.Meta.ModelChoice; frozenChoice == nil || frozenChoice.Wire != agent.MessagesWire {
		t.Fatalf("expected the session to hold the wire it was created over, got %+v", frozenChoice)
	}
	resumedChoice, err := sessions.ModelChoice(stored, []model.Choice{inProcessOpenCodeChoice("fake", sim.Completions, false)}, filepath.Join(t.TempDir(), "seen_models.json"), sessions.ModelSelection(stored))
	if err != nil {
		t.Fatal(err)
	}
	_, second, _ := runInProcessOpenCodeTurn(t, endpoint, sessionsDirectory, stored, resumedChoice, sessions.ModelSelection(stored), "second question", false)
	if !strings.Contains(second, "Second answer.") {
		t.Errorf("expected the session to resume, got %q", second)
	}
	requests := endpoint.snapshot()
	if want := []string{"/v1/messages", "/v1/messages"}; !slices.Equal(requests.paths, want) {
		t.Errorf("got conversation paths %q, want %q", requests.paths, want)
	}
}

func TestInProcessALegacySessionChoiceMigratesOntoItsSpokenWire(t *testing.T) {
	endpoint := newInProcessOpenCodeServer(t, &sim.Scenario{Model: "qwen-fake", Wire: sim.Messages, Turns: []sim.Turn{{Say: "First answer."}, {Say: "Second answer."}}}, "X-Api-Key")
	sessionsDirectory := t.TempDir()
	choice := inProcessOpenCodeChoice("qwen-fake", sim.Messages, false)
	selection := model.Selection{Provider: choice.Provider, Model: choice.ID, Effort: "high"}
	stored, first, _ := runInProcessOpenCodeTurn(t, endpoint, sessionsDirectory, nil, choice, selection, "first question", false)
	if !strings.Contains(first, "First answer.") {
		t.Errorf("expected the first answer, got %q", first)
	}
	rewriteJournalHeadAsFormatNineteen(
		t,
		filepath.Join(sessionsDirectory, stored.Name, "session.jsonl"),
		withoutTheWire,
	)
	if _, err := migrate.Session(migrate.Options{Directory: sessionsDirectory, BackupDir: t.TempDir()}, stored.Name); err != nil {
		t.Fatal(err)
	}
	migrated, err := store.Read(sessionsDirectory, stored.Name)
	if err != nil {
		t.Fatal(err)
	}
	if frozenChoice := migrated.Meta.ModelChoice; frozenChoice == nil || frozenChoice.Wire != agent.MessagesWire {
		t.Fatalf("expected the migration to record the messages wire, got %+v", frozenChoice)
	}
	_, second, _ := runInProcessOpenCodeTurn(t, endpoint, sessionsDirectory, migrated, *migrated.Meta.ModelChoice, sessions.ModelSelection(migrated), "second question", false)
	if !strings.Contains(second, "Second answer.") {
		t.Errorf("expected the session to resume, got %q", second)
	}
	requests := endpoint.snapshot()
	if want := []string{"/v1/messages", "/v1/messages"}; !slices.Equal(requests.paths, want) {
		t.Errorf("got conversation paths %q, want %q", requests.paths, want)
	}
}

func TestInProcessASessionWithoutAChoiceUsesItsMigratedSeenModelWire(t *testing.T) {
	endpoint := newInProcessOpenCodeServer(t, &sim.Scenario{Model: "qwen-fake", Wire: sim.Messages, Turns: []sim.Turn{{Say: "First answer."}, {Say: "Second answer."}}}, "X-Api-Key")
	sessionsDirectory := t.TempDir()
	choice := inProcessOpenCodeChoice("qwen-fake", sim.Messages, false)
	selection := model.Selection{Provider: choice.Provider, Model: choice.ID, Effort: "high"}
	stored, first, _ := runInProcessOpenCodeTurn(t, endpoint, sessionsDirectory, nil, choice, selection, "first question", false)
	if !strings.Contains(first, "First answer.") {
		t.Errorf("expected the first answer, got %q", first)
	}
	rewriteJournalHeadAsFormatNineteen(
		t,
		filepath.Join(sessionsDirectory, stored.Name, "session.jsonl"),
		withoutTheModelChoice,
	)
	stateRoot := t.TempDir()
	t.Setenv(location.StateDirVariable, stateRoot)
	legacyModels := checkedModelCache(`{"opencode-go":{"models":[{"id":"fake-2","efforts":["high"],"output":128000,"wire":"completions"}]}}`)
	legacySeen := `{"version":1,"providers":{"opencode-go":[{"id":"qwen-fake","efforts":["high"],"output":128000}]}}`
	stateDirectory, err := os.OpenRoot(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stateDirectory.Close() }()
	if err := stateDirectory.WriteFile("models.json", legacyModels, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stateDirectory.WriteFile("seen_models.json", []byte(legacySeen), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := location.MigrateDataLayout(false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"models.json", "seen_models.json"} {
		if _, err := stateDirectory.Stat(filepath.Join("data", name)); err != nil {
			t.Errorf("%s was not moved into data: %v", name, err)
		}
		if _, err := stateDirectory.Stat(name); !os.IsNotExist(err) {
			t.Errorf("legacy %s remains: %v", name, err)
		}
	}
	seenPath := filepath.Join(stateRoot, "data", "seen_models.json")
	if _, hasSeen, err := migrate.MigrateSeenModels(migrate.SeenModelsOptions{Path: seenPath}); err != nil || !hasSeen {
		t.Fatalf("migrate seen models: hasSeen=%t err=%v", hasSeen, err)
	}
	if _, err := migrate.Session(migrate.Options{Directory: sessionsDirectory, BackupDir: t.TempDir()}, stored.Name); err != nil {
		t.Fatal(err)
	}
	currentChoices := model.Choices(filepath.Join(stateRoot, "data", "models.json"))
	migrated, err := store.Read(sessionsDirectory, stored.Name)
	if err != nil {
		t.Fatal(err)
	}
	if migrated.Meta.ModelChoice != nil {
		t.Fatalf("expected the session to still hold no model choice, got %+v", migrated.Meta.ModelChoice)
	}
	resumedChoice, err := sessions.ModelChoice(migrated, currentChoices, seenPath, sessions.ModelSelection(migrated))
	if err != nil {
		t.Fatal(err)
	}
	if resumedChoice.Wire != agent.MessagesWire {
		t.Fatalf("expected the seen model to migrate onto messages, got %+v", resumedChoice)
	}
	_, second, _ := runInProcessOpenCodeTurn(t, endpoint, sessionsDirectory, migrated, resumedChoice, sessions.ModelSelection(migrated), "second question", false)
	if !strings.Contains(second, "Second answer.") {
		t.Errorf("expected the session to resume, got %q", second)
	}
	requests := endpoint.snapshot()
	if want := []string{"/v1/messages", "/v1/messages"}; !slices.Equal(requests.paths, want) {
		t.Errorf("got conversation paths %q, want %q", requests.paths, want)
	}
}

func TestInProcessAnEffortlessOpenCodeModelResumesWithoutAnEffort(t *testing.T) {
	for _, test := range []struct {
		wire          string
		path          string
		requireEffort func(t *testing.T, request int, body map[string]any)
	}{
		{sim.Messages, "/messages", requireThinkingWithoutAnEffort},
		{sim.Responses, "/responses", requireAReasoningSummaryWithoutAnEffort},
		{sim.GenerateContent, ":streamGenerateContent", requireThoughtsWithoutAThinkingLevel},
	} {
		t.Run(test.wire, func(t *testing.T) {
			keyHeader, _ := inProcessOpenCodeKeyHeader(test.wire)
			endpoint := newInProcessOpenCodeServer(t, &sim.Scenario{Model: "fake", Wire: test.wire, Turns: []sim.Turn{{Say: "First answer."}, {Say: "Second answer."}}}, keyHeader)
			choice := inProcessOpenCodeChoice("fake", test.wire, true)
			if _, err := model.ParseSelection([]model.Choice{choice}, "opencode-go/fake@high", model.Defaults{}); err == nil || !strings.Contains(err.Error(), "takes no effort level") {
				t.Errorf("expected an effort to be refused, got %v", err)
			}
			selection, err := model.ParseSelection([]model.Choice{choice}, "opencode-go/fake", model.Defaults{})
			if err != nil {
				t.Fatal(err)
			}
			sessionsDirectory := t.TempDir()
			stored, first, _ := runInProcessOpenCodeTurn(t, endpoint, sessionsDirectory, nil, choice, selection, "first question", false)
			if !strings.Contains(first, "First answer.") {
				t.Errorf("expected the first answer, got %q", first)
			}
			if effort := stored.Meta.Effort; effort != "" {
				t.Errorf("expected the session to hold no effort, got %q", effort)
			}
			_, second, _ := runInProcessOpenCodeTurn(t, endpoint, sessionsDirectory, stored, *stored.Meta.ModelChoice, sessions.ModelSelection(stored), "second question", false)
			if !strings.Contains(second, "Second answer.") {
				t.Errorf("expected the session to resume, got %q", second)
			}
			requests := endpoint.snapshot()
			if len(requests.bodies) != 2 {
				t.Fatalf("got %d conversation requests, want two", len(requests.bodies))
			}
			for index, body := range requests.bodies {
				test.requireEffort(t, index, body)
			}
		})
	}
}

func TestInProcessOpenCodeRequestsCarryTheKeyAndStoredSessionIdentifier(t *testing.T) {
	for _, test := range []struct {
		wire     string
		path     string
		keyName  string
		keyValue string
	}{
		{sim.Completions, "/v1/chat/completions", "Authorization", "Bearer stand-in"},
		{sim.Responses, "/v1/responses", "Authorization", "Bearer stand-in"},
		{sim.Messages, "/v1/messages", "X-Api-Key", "stand-in"},
		{sim.GenerateContent, "/google/v1beta/models/fake:streamGenerateContent", "X-Goog-Api-Key", "stand-in"},
	} {
		t.Run(test.wire, func(t *testing.T) {
			endpoint := newInProcessOpenCodeServer(t, &sim.Scenario{Model: "fake", Wire: test.wire, Turns: []sim.Turn{{Say: "First answer."}, {Say: "Second answer."}}}, test.keyName)
			choice := inProcessOpenCodeChoice("fake", test.wire, false)
			selection := model.Selection{Provider: choice.Provider, Model: choice.ID, Effort: "high"}
			sessionsDirectory := t.TempDir()
			stored, first, _ := runInProcessOpenCodeTurn(t, endpoint, sessionsDirectory, nil, choice, selection, "first question", false)
			if !strings.Contains(first, "First answer.") {
				t.Errorf("expected the first answer, got %q", first)
			}
			_, second, _ := runInProcessOpenCodeTurn(t, endpoint, sessionsDirectory, stored, *stored.Meta.ModelChoice, sessions.ModelSelection(stored), "second question", false)
			if !strings.Contains(second, "Second answer.") {
				t.Errorf("expected the session to resume, got %q", second)
			}
			requests := endpoint.snapshot()
			if !slices.Equal(requests.ids, []string{stored.ID, stored.ID}) {
				t.Errorf("got OpenCode session headers %q, want the stored ID %q twice", requests.ids, stored.ID)
			}
			if slices.Contains(requests.ids, stored.Name) {
				t.Errorf("sent the human-readable session name %q", stored.Name)
			}
			if !slices.Equal(requests.paths, []string{test.path, test.path}) {
				t.Errorf("got conversation paths %q, want %s twice", requests.paths, test.path)
			}
			if !slices.Equal(requests.keys, []string{test.keyValue, test.keyValue}) {
				t.Errorf("got %s headers %q, want %q twice", test.keyName, requests.keys, test.keyValue)
			}
		})
	}
}

func TestInProcessASessionKeepsItsWireRecordingChoice(t *testing.T) {
	for _, test := range []struct {
		name          string
		isWireOn      bool
		isWireOnLater bool
	}{
		{name: "recorded then turned off", isWireOn: true},
		{name: "unrecorded then turned on", isWireOnLater: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoint := newInProcessOpenCodeServer(t, &sim.Scenario{Model: "fake", Wire: sim.Messages, Loop: true, Turns: []sim.Turn{{Say: "Done."}}}, "X-Api-Key")
			sessionsDirectory := t.TempDir()
			choice := inProcessOpenCodeChoice("fake", sim.Messages, false)
			selection := model.Selection{Provider: choice.Provider, Model: choice.ID, Effort: "high"}
			stored, _, firstSize := runInProcessOpenCodeTurn(t, endpoint, sessionsDirectory, nil, choice, selection, "say something", !test.isWireOn)
			if isRecorded := firstSize > 0; isRecorded != test.isWireOn {
				t.Fatalf("expected the wire recorded to be %t, got a transcript of %d bytes", test.isWireOn, firstSize)
			}
			wirePath := filepath.Join(sessionsDirectory, stored.Name, "debug", "wire.http.zst")
			if test.isWireOn {
				transcript := inProcessWireTranscript(t, wirePath)
				requestLine := "> POST " + endpoint.server.URL + "/v1/messages HTTP/1.1"
				if !strings.Contains(transcript, "# exchange 1 start") || !strings.Contains(transcript, requestLine) || !strings.Contains(transcript, "say something") {
					t.Errorf("wire transcript did not contain the initial conversation request: %q", transcript)
				}
			}
			if isRecorded := !stored.Meta.ShouldSkipWire; isRecorded != test.isWireOn {
				t.Fatalf("stored recording choice is %t, want %t", isRecorded, test.isWireOn)
			}
			if stored.Meta.Environment == nil || stored.Meta.Environment.WireLogPath != map[bool]string{true: "debug/wire.http.zst", false: ""}[test.isWireOn] {
				t.Fatalf("stored environment has wire path %+v, recording is %t", stored.Meta.Environment, test.isWireOn)
			}
			resumed, _, secondSize := runInProcessOpenCodeTurn(t, endpoint, sessionsDirectory, stored, *stored.Meta.ModelChoice, sessions.ModelSelection(stored), "say it again", !test.isWireOnLater)
			if resumed.Meta.Environment == nil || resumed.Meta.Environment.WireLogPath != stored.Meta.Environment.WireLogPath {
				t.Errorf("resumed environment changed wire path from %+v to %+v", stored.Meta.Environment, resumed.Meta.Environment)
			}
			if test.isWireOn && secondSize <= firstSize {
				t.Errorf("expected the resumed session to keep recording, got %d bytes then %d", firstSize, secondSize)
			}
			if test.isWireOn {
				transcript := inProcessWireTranscript(t, filepath.Join(sessionsDirectory, stored.Name, "debug", "wire.http.zst"))
				requestLine := "> POST " + endpoint.server.URL + "/v1/messages HTTP/1.1"
				if !strings.Contains(transcript, "# exchange 1 start") || !strings.Contains(transcript, "# exchange 2 start") ||
					!strings.Contains(transcript, requestLine) ||
					strings.Count(transcript, requestLine) != 2 ||
					!strings.Contains(transcript, "say something") || !strings.Contains(transcript, "say it again") {
					t.Errorf("wire transcript did not contain both conversation requests: %q", transcript)
				}
			}
			if !test.isWireOn && secondSize != 0 {
				t.Errorf("expected the resumed session to keep recording nothing, got %d bytes", secondSize)
			}
			requests := endpoint.snapshot()
			if len(requests.bodies) != 2 {
				t.Fatalf("got %d conversation requests, want one for each run", len(requests.bodies))
			}
			for index, body := range requests.bodies {
				encoded, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(encoded, []byte(`wire.http.zst`)) {
					t.Errorf("request %d: the system prompt names an internal wire transcript", index)
				}
			}
		})
	}
}
