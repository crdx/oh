package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/pkg/tool"
)

type fakeManager struct {
	tasks       []Task
	names       []string
	sent        string
	message     string
	broadcastTo []string
}

func (self *fakeManager) Start(_ context.Context, _ string, tasks []Task) (string, error) {
	self.tasks = tasks
	return "started frugal-otter, frugal-heron", nil
}

func (self *fakeManager) Send(_ context.Context, name string, message string) (string, error) {
	self.sent, self.message = name, message
	return "sent to " + name, nil
}

func (self *fakeManager) Broadcast(_ context.Context, message string) ([]string, error) {
	self.message = message
	if self.broadcastTo != nil {
		return self.broadcastTo, nil
	}
	return []string{"frugal-otter", "frugal-heron"}, nil
}

func (self *fakeManager) Status(names []string) (string, error) {
	self.names = names
	return "running", nil
}

func (self *fakeManager) Output(names []string) (string, error) {
	self.names = names
	return "result", nil
}

func (self *fakeManager) Stop(names []string) (string, error) {
	self.names = names
	return "stopped", nil
}

func (self *fakeManager) List() string { return "frugal-otter" }

func (self *fakeManager) Models() string { return "- anthropic/claude-haiku-5@high" }

func isGranted() bool { return true }

func TestEveryActionIsRefusedWithoutSubagentAccess(t *testing.T) {
	for _, arguments := range []string{
		`{"action":"start","subagents":[{"model":"anthropic/claude-opus-5","prompt":"x","intent":"Doing x"}]}`,
		`{"action":"send","name":"frugal-otter","message":"hello"}`,
		`{"action":"status"}`,
		`{"action":"output"}`,
		`{"action":"stop"}`,
		`{"action":"list"}`,
		`{"action":"models"}`,
	} {
		manager := &fakeManager{}
		call, err := New(manager, func() bool { return false }).Parse(arguments)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := call.Exec(t.Context()); !errors.Is(err, ErrWithheld) {
			t.Errorf("%s: got %v, want %v", arguments, err, ErrWithheld)
		}
		if manager.tasks != nil || manager.names != nil || manager.message != "" {
			t.Errorf("%s: the manager was reached", arguments)
		}
	}
}

func TestStartTakesIndependentObjectPrompts(t *testing.T) {
	manager := &fakeManager{}
	built := New(manager, isGranted)
	schema, err := json.Marshal(built.Schema())
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`"prompt"`, `"required":["model","prompt","intent"]`, `"additionalProperties":false`} {
		if !strings.Contains(string(schema), required) {
			t.Errorf("schema %s omits %s", schema, required)
		}
	}
	parsed, err := built.Parse(`{"action":"start","system_prompt":"review independently","subagents":[{"model":"anthropic/claude-opus-5","prompt":"same","intent":"reviewing the same thing"},{"model":"anthropic/claude-opus-5","prompt":"same","intent":"Reviewing it again","workspace":"/tmp/job"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := parsed.Exec(context.Background())
	if err != nil || result.Output != "started frugal-otter, frugal-heron" || len(manager.tasks) != 2 {
		t.Fatalf("got %+v, %v, tasks=%+v", result, err, manager.tasks)
	}
	if manager.tasks[0].Workspace != "" || manager.tasks[1].Workspace != "/tmp/job" {
		t.Errorf("workspaces were not handed on: %+v", manager.tasks)
	}
	if manager.tasks[0].Intent != "Reviewing the same thing" {
		t.Errorf("an intent was handed on as %q, not spoken", manager.tasks[0].Intent)
	}
}

func TestNamesSelectAgentsAndNoNamesMeansAll(t *testing.T) {
	manager := &fakeManager{}
	built := New(manager, isGranted)
	for _, input := range []string{
		`{"action":"status","names":["frugal-otter"]}`,
		`{"action":"output","names":["frugal-otter"]}`,
		`{"action":"stop","names":["frugal-otter"]}`,
	} {
		manager.names = nil
		parsed, err := built.Parse(input)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parsed.Exec(context.Background()); err != nil || len(manager.names) != 1 || manager.names[0] != "frugal-otter" {
			t.Errorf("%s reached the manager as %v, %v", input, manager.names, err)
		}
	}
	parsed, err := built.Parse(`{"action":"status"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parsed.Exec(context.Background()); err != nil || manager.names != nil {
		t.Errorf("asking after no names did not mean all of them: %v, %v", manager.names, err)
	}
}

func TestSendResumesAFinishedAgent(t *testing.T) {
	manager := &fakeManager{}
	built := New(manager, isGranted)
	parsed, err := built.Parse(`{"action":"send","name":"frugal-otter","message":"now check the tests"}`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := parsed.Exec(context.Background())
	if err != nil || result.Output != "sent to frugal-otter" || manager.message != "now check the tests" {
		t.Fatalf("got %+v, %v, %+v", result, err, manager)
	}
}

func TestSendWithoutANameReachesEveryRunningAgent(t *testing.T) {
	manager := &fakeManager{}
	built := New(manager, isGranted)
	parsed, err := built.Parse(`{"action":"send","message":"stop guessing"}`)
	if err != nil {
		t.Fatal(err)
	}
	if rendering := parsed.Rendering(); rendering.Subject != "every running subagent" || rendering.Mentions != nil {
		t.Errorf("a broadcast was drawn as %+v", rendering)
	}
	result, err := parsed.Exec(context.Background())
	if err != nil || manager.message != "stop guessing" || manager.sent != "" {
		t.Fatalf("got %+v, %v, %+v", result, err, manager)
	}
	if want := "queued for frugal-otter and frugal-heron"; result.Output != want {
		t.Errorf("a broadcast came back %q, want %q", result.Output, want)
	}
}

func TestABroadcastNamesItsRecipientsAsProse(t *testing.T) {
	for _, test := range []struct {
		recipients []string
		want       string
	}{
		{[]string{"frugal-otter"}, "queued for frugal-otter"},
		{[]string{"frugal-otter", "frugal-heron"}, "queued for frugal-otter and frugal-heron"},
		{[]string{"frugal-otter", "frugal-heron", "frugal-stoat"}, "queued for frugal-otter, frugal-heron and frugal-stoat"},
	} {
		parsed, err := New(&fakeManager{broadcastTo: test.recipients}, isGranted).Parse(`{"action":"send","message":"stop guessing"}`)
		if err != nil {
			t.Fatal(err)
		}
		result, err := parsed.Exec(context.Background())
		if err != nil || result.Output != test.want {
			t.Errorf("a broadcast to %v came back %q, %v, want %q", test.recipients, result.Output, err, test.want)
		}
	}
}

func TestTheDescriptionLeavesTheLimitToStartSinceItCanChange(t *testing.T) {
	description := New(&fakeManager{}, isGranted).Description()
	if regexp.MustCompile(`\d`).MatchString(description) || !strings.Contains(description, "start says how many are running against the limit") {
		t.Errorf("the description freezes a limit that can change, or hides where to find it: %q", description)
	}
}

func TestInvalidChildArgumentsAreRefused(t *testing.T) {
	built := New(&fakeManager{}, isGranted)
	for _, input := range []string{
		`{"action":"start","subagents":[{"model":"anthropic/claude-opus-5","prompt":""}]}`,
		`{"action":"start","subagents":[{"model":"anthropic/claude-opus-5","prompt":"x"}]}`,
		`{"action":"start","subagents":[{"model":"anthropic/claude-opus-5","prompt":"x","intent":" "}]}`,
		`{"action":"status","wait_seconds":5}`,
		`{"action":"start","subagents":[{"prompt":"x","intent":"Doing x"}]}`,
		`{"action":"start","subagents":[{"model":" ","prompt":"x","intent":"Doing x"}]}`,
		`{"action":"start","subagents":[{"model":"codex/gpt-6-luna@high+fast","prompt":"x","intent":"Doing x"}]}`,
		`{"action":"status","names":["frugal-otter"],"subagents":[{"model":"anthropic/claude-opus-5","prompt":"x"}]}`,
		`{"action":"send","name":"frugal-otter"}`,
		`{"action":"send","message":" "}`,
		`{"action":"start","names":["frugal-otter"],"subagents":[{"model":"anthropic/claude-opus-5","prompt":"x"}]}`,
		`{"action":"list","names":["frugal-otter"]}`,
		`{"action":"wait","name":"frugal-otter"}`,
		`{"action":"wait"}`,
	} {
		if _, err := built.Parse(input); err == nil {
			t.Errorf("accepted %s", input)
		}
	}
	if _, err := tool.ParseUnder(built, built.Schema(), `{"action":"list"}`); err != nil {
		t.Fatal(err)
	}
}

func TestEveryCallNamingSubagentsMentionsThem(t *testing.T) {
	built := New(&fakeManager{}, isGranted)
	for input, want := range map[string][]string{
		`{"action":"status","names":["frugal-otter","frugal-heron"]}`: {"subagent:frugal-otter", "subagent:frugal-heron"},
		`{"action":"output","names":["frugal-heron"]}`:                {"subagent:frugal-heron"},
		`{"action":"stop","names":["frugal-otter"]}`:                  {"subagent:frugal-otter"},
		`{"action":"send","name":"frugal-otter","message":"x"}`:       {"subagent:frugal-otter"},
		`{"action":"list"}`: nil,
	} {
		parsed, err := built.Parse(input)
		if err != nil {
			t.Fatal(err)
		}
		if got := parsed.Rendering().Mentions; !slices.Equal(got, want) {
			t.Errorf("%s mentioned %v, want %v", input, got, want)
		}
	}
	single, err := built.Parse(`{"action":"start","subagents":[{"model":"anthropic/claude-opus-5","prompt":"x","intent":"reading the parser"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if intent := single.Rendering().Intent; intent != "Reading the parser" {
		t.Errorf("a single spawn led with %q", intent)
	}
	several, err := built.Parse(`{"action":"start","subagents":[{"model":"anthropic/claude-opus-5","prompt":"x","intent":"Reading"},{"model":"anthropic/claude-opus-5","prompt":"y","intent":"Writing"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if intent := several.Rendering().Intent; intent != "" {
		t.Errorf("a spawn of several led with %q", intent)
	}
}

func TestAStartRowShowsSelectionsBeforeItsIntents(t *testing.T) {
	built := New(&fakeManager{}, isGranted)
	for _, test := range []struct {
		input   string
		subject string
		intent  string
	}{
		{
			`{"action":"start","subagents":[{"model":"anthropic/claude-opus-5","prompt":"x","intent":"reading the readme"},{"model":"anthropic/claude-opus-5","prompt":"y","intent":"Finding the parser"}]}`,
			"2 subagents · anthropic/claude-opus-5, anthropic/claude-opus-5 · Reading the readme, Finding the parser",
			"",
		},
		{`{"action":"start","subagents":[{"model":"anthropic/claude-opus-5","prompt":"x","intent":"Reading the readme"}]}`, "1 subagent · anthropic/claude-opus-5", "Reading the readme"},
	} {
		parsed, err := built.Parse(test.input)
		if err != nil {
			t.Fatal(err)
		}
		rendering := parsed.Rendering()
		if rendering.Subject != test.subject || rendering.Intent != test.intent || rendering.Qualifier != "" {
			t.Errorf("a start drew %q, intent %q, qualified %q", rendering.Subject, rendering.Intent, rendering.Qualifier)
		}
	}
}

func TestOnlyActionsThatAnswerWithAStatusReportOne(t *testing.T) {
	for _, action := range []string{Start, Send, Status, Stop, Output, List, Models} {
		doesReportStatus := slices.Contains([]string{Start, Send, Status, Stop}, action)
		if got := Describe(Args{Action: action}).ReportsStatus; got != doesReportStatus {
			t.Errorf("%s reports a status: %v, want %v", action, got, doesReportStatus)
		}
	}
}

func TestModelsListsValidSelectionsWithoutNamingAChild(t *testing.T) {
	parsed, err := New(&fakeManager{}, isGranted).Parse(`{"action":"models"}`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := parsed.Exec(t.Context())
	if err != nil || result.Output != "- anthropic/claude-haiku-5@high" {
		t.Fatalf("models returned %+v, %v", result, err)
	}
	if rendering := parsed.Rendering(); rendering.Subject != "models" || rendering.ReportsStatus || len(rendering.Mentions) != 0 {
		t.Errorf("models rendered as %+v", rendering)
	}
}

func TestTheDescriptionExplainsValidSelectionRatherThanProviderExceptions(t *testing.T) {
	description := New(&fakeManager{}, isGranted).Description()
	if !strings.Contains(description, "provider/model@effort") || strings.Contains(description, "+fast") || strings.Contains(description, "ollama") {
		t.Errorf("the description does not explain just the valid selection format: %s", description)
	}
}

func TestABatchedStartShowsEveryChosenSelection(t *testing.T) {
	rendering := Describe(Args{Action: Start, Subagents: []Task{
		{Model: "codex/gpt-6-luna@low", Intent: "Reviewing the parser"},
		{Model: "opencode-go/step-5-preview-free@high", Intent: "Checking the tests"},
	}})
	want := "2 subagents · codex/gpt-6-luna@low, opencode-go/step-5-preview-free@high · Reviewing the parser, Checking the tests"
	if rendering.Subject != want {
		t.Errorf("batch rendered %q, want %q", rendering.Subject, want)
	}
}
