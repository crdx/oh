package subagent

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/pkg/tool"
)

type fakeManager struct {
	limit   time.Duration
	tasks   []Task
	names   []string
	sent    string
	message string
}

func (self *fakeManager) Start(_ context.Context, _ string, tasks []Task) (string, error) {
	self.tasks = tasks
	return "started frugal-otter, frugal-heron", nil
}

func (self *fakeManager) Send(_ context.Context, name string, message string) (string, error) {
	self.sent, self.message = name, message
	return "sent to " + name, nil
}

func (self *fakeManager) Status(names []string) (string, error) {
	self.names = names
	return "running", nil
}

func (self *fakeManager) Output(names []string) (string, error) {
	self.names = names
	return "result", nil
}

func (self *fakeManager) Wait(_ context.Context, names []string, limit time.Duration) (string, error) {
	self.names = names
	self.limit = limit
	return "result", nil
}

func (self *fakeManager) Stop(names []string) (string, error) {
	self.names = names
	return "stopped", nil
}

func (self *fakeManager) List() string { return "frugal-otter" }

func TestStartTakesIndependentObjectPrompts(t *testing.T) {
	manager := &fakeManager{}
	built := New(manager, "")
	schema, err := json.Marshal(built.Schema())
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`"prompt"`, `"required":["prompt","intent"]`, `"additionalProperties":false`} {
		if !strings.Contains(string(schema), required) {
			t.Errorf("schema %s omits %s", schema, required)
		}
	}
	parsed, err := built.Parse(`{"action":"start","system_prompt":"review independently","subagents":[{"prompt":"same","intent":"reviewing the same thing"},{"prompt":"same","intent":"Reviewing it again","workspace":"/tmp/job"}]}`)
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
	built := New(manager, "")
	for _, input := range []string{
		`{"action":"status","names":["frugal-otter"]}`,
		`{"action":"output","names":["frugal-otter"]}`,
		`{"action":"wait","names":["frugal-otter"]}`,
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
	parsed, err := built.Parse(`{"action":"wait"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parsed.Exec(context.Background()); err != nil || manager.names != nil {
		t.Errorf("waiting on no names did not mean all of them: %v, %v", manager.names, err)
	}
}

func TestSendResumesAFinishedAgent(t *testing.T) {
	manager := &fakeManager{}
	built := New(manager, "")
	parsed, err := built.Parse(`{"action":"send","name":"frugal-otter","message":"now check the tests"}`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := parsed.Exec(context.Background())
	if err != nil || result.Output != "sent to frugal-otter" || manager.message != "now check the tests" {
		t.Fatalf("got %+v, %v, %+v", result, err, manager)
	}
}

func TestInvalidChildArgumentsAreRefused(t *testing.T) {
	built := New(&fakeManager{}, "")
	for _, input := range []string{
		`{"action":"start","subagents":[{"prompt":""}]}`,
		`{"action":"start","subagents":[{"prompt":"x"}]}`,
		`{"action":"start","subagents":[{"prompt":"x","intent":" "}]}`,
		`{"action":"status","wait_seconds":5}`,
		`{"action":"start","subagents":[{"prompt":"x","intent":"Doing x","model":"unapproved"}]}`,
		`{"action":"status","names":["frugal-otter"],"subagents":[{"prompt":"x"}]}`,
		`{"action":"send","name":"frugal-otter"}`,
		`{"action":"send","message":"x"}`,
		`{"action":"start","names":["frugal-otter"],"subagents":[{"prompt":"x"}]}`,
		`{"action":"list","names":["frugal-otter"]}`,
		`{"action":"wait","name":"frugal-otter"}`,
	} {
		if _, err := built.Parse(input); err == nil {
			t.Errorf("accepted %s", input)
		}
	}
	if _, err := tool.ParseUnder(built, built.Schema(), `{"action":"list"}`); err != nil {
		t.Fatal(err)
	}
}

func TestAWaitRendersAndIsLimitedAsAJobWaitIs(t *testing.T) {
	manager := &fakeManager{}
	built := New(manager, "")
	for _, run := range []struct {
		input   string
		subject string
		limit   time.Duration
		bound   string
	}{
		{`{"action":"wait","names":["frugal-otter","frugal-heron"]}`, "frugal-otter && frugal-heron", waitLimit, ""},
		{`{"action":"wait"}`, "all", waitLimit, ""},
		{`{"action":"wait","names":["frugal-otter"],"wait_seconds":20}`, "frugal-otter", 20 * time.Second, "for up to 20s"},
		{`{"action":"wait","wait_seconds":100000}`, "all", waitLimit, "for up to 10m"},
	} {
		parsed, err := built.Parse(run.input)
		if err != nil {
			t.Fatal(err)
		}
		rendering := parsed.Rendering()
		if rendering.Subject != run.subject || rendering.Qualifier != run.bound || parsed.TimeLimit() != run.limit {
			t.Errorf("%s drew %q %q limited to %s", run.input, rendering.Subject, rendering.Qualifier, parsed.TimeLimit())
		}
		if _, err := parsed.Exec(context.Background()); err != nil || manager.limit != run.limit {
			t.Errorf("%s waited at most %s: %v", run.input, manager.limit, err)
		}
	}
}

func TestEveryCallNamingSubagentsMentionsThem(t *testing.T) {
	built := New(&fakeManager{}, "")
	for input, want := range map[string][]string{
		`{"action":"wait","names":["frugal-otter"]}`:                  {"subagent:frugal-otter"},
		`{"action":"status","names":["frugal-otter","frugal-heron"]}`: {"subagent:frugal-otter", "subagent:frugal-heron"},
		`{"action":"output","names":["frugal-heron"]}`:                {"subagent:frugal-heron"},
		`{"action":"stop","names":["frugal-otter"]}`:                  {"subagent:frugal-otter"},
		`{"action":"send","name":"frugal-otter","message":"x"}`:       {"subagent:frugal-otter"},
		`{"action":"wait"}`: nil,
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
	single, err := built.Parse(`{"action":"start","subagents":[{"prompt":"x","intent":"reading the parser"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if intent := single.Rendering().Intent; intent != "Reading the parser" {
		t.Errorf("a single spawn led with %q", intent)
	}
	several, err := built.Parse(`{"action":"start","subagents":[{"prompt":"x","intent":"Reading"},{"prompt":"y","intent":"Writing"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if intent := several.Rendering().Intent; intent != "" {
		t.Errorf("a spawn of several led with %q", intent)
	}
}

func TestASpawnNamesTheModelItsSubagentsRunOn(t *testing.T) {
	parsed, err := New(&fakeManager{}, "claude-sonnet-5").Parse(`{"action":"start","subagents":[{"prompt":"x","intent":"Doing x"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if qualifier := parsed.Rendering().Qualifier; qualifier != "on claude-sonnet-5" {
		t.Errorf("a spawn was qualified as %q", qualifier)
	}
	waited, err := New(&fakeManager{}, "claude-sonnet-5").Parse(`{"action":"wait"}`)
	if err != nil {
		t.Fatal(err)
	}
	if qualifier := waited.Rendering().Qualifier; qualifier != "" {
		t.Errorf("a wait was qualified as %q", qualifier)
	}
}
