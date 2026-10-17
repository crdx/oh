package job_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"

	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/jobs"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/bash"
	"crdx.org/oh/pkg/toolbox/forward"
	"crdx.org/oh/pkg/toolbox/job"
)

func run(t *testing.T, manager *jobs.Manager, arguments any) (string, error) {
	t.Helper()

	encoded, err := json.Marshal(arguments)
	if err != nil {
		t.Fatal(err)
	}
	encoded = withIntent(t, encoded)

	built := job.New(manager, nil, func(context.Context) (sandbox.Policy, error) {
		return sandbox.Policy{}, nil
	}, nil)

	call, err := built.Parse(string(encoded))
	if err != nil {
		return "", err
	}

	result, err := call.Exec(t.Context())

	return result.Output, err
}

func withFinishedJobs(t *testing.T) *jobs.Manager {
	t.Helper()

	manager := jobs.New(nil)
	manager.Restore([]jobs.Snapshot{
		{Name: "build", Command: "just build", State: jobs.StateFailed, ExitCode: 1},
		{Name: "watch", Command: "just watch", State: jobs.StateComplete},
	})

	return manager
}

type heldRunner struct{}

func (heldRunner) Run(context.Context, string, string, sandbox.Policy) (sandbox.Result, error) {
	panic("unexpected foreground run")
}

func (heldRunner) Start(
	context.Context,
	string,
	string,
	sandbox.Policy,
	sandbox.Output,
) (sandbox.Command, error) {
	return &heldCommand{over: make(chan struct{})}, nil
}

type heldCommand struct {
	over chan struct{}
	once sync.Once
}

func (self *heldCommand) Wait() (sandbox.Result, error) {
	<-self.over
	return sandbox.Result{}, nil
}

func (self *heldCommand) Signal(syscall.Signal) error {
	self.Stop()
	return nil
}

func (self *heldCommand) Stop() {
	self.once.Do(func() { close(self.over) })
}

type recordedPorts struct {
	publications []forward.Publication
	refusal      error
}

func (self *recordedPorts) Forward(port uint16, jobName string) (string, error) {
	if self.refusal != nil {
		return "", self.refusal
	}
	self.publications = append(self.publications, forward.Publication{Port: port, JobName: jobName})
	return "http://session.test:" + strconv.Itoa(int(port)), nil
}

func (self *recordedPorts) Revoke(uint16) error { return nil }

func (self *recordedPorts) List() []forward.Publication { return self.publications }

func newJobTool(t *testing.T, manager *jobs.Manager, ports forward.Ports) tool.Tool {
	t.Helper()

	openedRoot, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = openedRoot.Close() })

	return job.New(
		manager,
		file.New(openedRoot, func(string) error { return nil }),
		func(context.Context) (sandbox.Policy, error) { return sandbox.Policy{}, nil },
		ports,
	)
}

func callJob(t *testing.T, built tool.Tool, arguments string) (tool.ToolCallResult, error) {
	t.Helper()

	call, err := built.Parse(arguments)
	if err != nil {
		return tool.ToolCallResult{}, err
	}

	return call.Exec(t.Context())
}

func TestPruningNamesWhatItRemoved(t *testing.T) {
	output, err := run(t, withFinishedJobs(t), map[string]string{"action": "prune"})
	if err != nil {
		t.Fatal(err)
	}

	if output != "pruned build, watch." {
		t.Errorf("got %q, want it to name what it pruned", output)
	}
}

func TestPruningNothingSaysSo(t *testing.T) {
	output, err := run(t, jobs.New(nil), map[string]string{"action": "prune"})
	if err != nil {
		t.Fatal(err)
	}

	if output != "there are no finished jobs to prune." {
		t.Errorf("got %q, want it to say there was nothing to do", output)
	}
}

func TestDiscardingReportsTheJobItRemoved(t *testing.T) {
	output, err := run(t, withFinishedJobs(t), map[string]string{"action": "discard", "name": "build"})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(output, "build: failed") || !strings.HasSuffix(output, "and discarded") {
		t.Errorf("got %q, want the job described and marked discarded", output)
	}
}

func TestListingNamesEveryJobWithItsCommand(t *testing.T) {
	output, err := run(t, withFinishedJobs(t), map[string]string{"action": "list"})
	if err != nil {
		t.Fatal(err)
	}

	for _, wanted := range []string{"build: failed", "just build", "watch: complete", "just watch"} {
		if !strings.Contains(output, wanted) {
			t.Errorf("got %q, want it to carry %q", output, wanted)
		}
	}
}

func TestAnEmptyListingSaysSo(t *testing.T) {
	output, err := run(t, jobs.New(nil), map[string]string{"action": "list"})
	if err != nil {
		t.Fatal(err)
	}

	if output != "no jobs have been started in this session." {
		t.Errorf("got %q, want it to say nothing has been started", output)
	}
}

func TestStartingAnUnknownNameWithNoCommandIsRefused(t *testing.T) {
	_, err := run(t, jobs.New(nil), map[string]string{"action": "start", "name": "ghost"})
	if err == nil || !strings.Contains(err.Error(), "command required") {
		t.Errorf("got %v, want it to ask for a command", err)
	}
}

func TestOnlyStartingAJobCarriesItsIntent(t *testing.T) {
	built := job.New(nil, nil, nil, nil)
	for _, arguments := range []string{
		`{"action":"start","name":"docs","command":"serve docs","intent":"serving the documentation"}`,
		`{"action":"start","name":"docs","intent":"serving the documentation"}`,
	} {
		call, err := built.Parse(arguments)
		if err != nil {
			t.Fatal(err)
		}
		if got := call.Rendering().Intent; got != "Serving the documentation" {
			t.Errorf("%s carried the intent %q", arguments, got)
		}
	}

	call, err := built.Parse(`{"action":"status","name":"docs","intent":"checking on the docs"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := call.Rendering().Intent; got != "" {
		t.Errorf("a status call carried the intent %q, want none", got)
	}
}

func TestAStartIntroducesItsJobAndOtherCallsMentionTheirs(t *testing.T) {
	built := job.New(nil, nil, nil, nil)
	for arguments, want := range map[string]struct {
		introduces string
		mentions   []string
	}{
		`{"action":"start","name":"docs","command":"serve","intent":"serving the docs"}`: {introduces: "docs"},
		`{"action":"status","name":"docs"}`:                                              {mentions: []string{"docs"}},
		`{"action":"output","name":"docs"}`:                                              {mentions: []string{"docs"}},
		`{"action":"stop","name":"docs"}`:                                                {mentions: []string{"docs"}},
		`{"action":"discard","name":"docs"}`:                                             {mentions: []string{"docs"}},
		`{"action":"list"}`:                                                              {},
		`{"action":"prune"}`:                                                             {},
	} {
		call, err := built.Parse(arguments)
		if err != nil {
			t.Fatal(err)
		}
		rendering := call.Rendering()
		if rendering.Introduces != want.introduces || !slices.Equal(rendering.Mentions, want.mentions) {
			t.Errorf("%s introduced %q and mentioned %q", arguments, rendering.Introduces, rendering.Mentions)
		}
	}
}

func TestOnlyStartingAJobNeedsAnIntent(t *testing.T) {
	built := job.New(nil, nil, nil, nil)

	if _, err := built.Parse(`{"action":"start","name":"docs","command":"serve docs"}`); err == nil ||
		err.Error() != "intent is required to start a job" {
		t.Errorf("got %v, want the start to ask for an intent", err)
	}
	if _, err := built.Parse(`{"action":"status","name":"docs"}`); err != nil {
		t.Errorf("a status call without an intent was refused: %v", err)
	}
}

func TestAStartRespawnsOnlyWhenAskedTo(t *testing.T) {
	manager := jobs.New(heldRunner{})
	t.Cleanup(func() { _ = manager.Close() })
	built := newJobTool(t, manager, nil)

	for _, current := range []struct {
		arguments string
		want      string
	}{
		{
			arguments: `{"intent":"check the job here","action":"start","name":"watch","command":"just watch","respawn":true}`,
			want:      "watch: running for 0s, run 1, respawns on exit",
		},
		{
			arguments: `{"intent":"check the job here","action":"start","name":"once","command":"just once"}`,
			want:      "once: running for 0s",
		},
	} {
		result, err := callJob(t, built, current.arguments)
		if err != nil {
			t.Fatal(err)
		}
		if result.Output != current.want {
			t.Errorf("got %q, want %q", result.Output, current.want)
		}
	}
}

func TestARestartFromTheRememberedCommandCanRespawn(t *testing.T) {
	manager := jobs.New(heldRunner{})
	t.Cleanup(func() { _ = manager.Close() })
	built := newJobTool(t, manager, nil)

	if _, err := callJob(t, built, `{"intent":"check the job here","action":"start","name":"watch","command":"just watch"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := callJob(t, built, `{"intent":"check the job here","action":"stop","name":"watch"}`); err != nil {
		t.Fatal(err)
	}

	result, err := callJob(t, built, `{"intent":"check the job here","action":"start","name":"watch","respawn":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if want := "watch: running for 0s, run 1, respawns on exit"; result.Output != want {
		t.Errorf("got %q, want %q", result.Output, want)
	}

	if _, err := callJob(t, built, `{"intent":"check the job here","action":"start","name":"watch"}`); err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.Status("watch-1")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Run != 0 || snapshot.Respawn != "" {
		t.Errorf("got %#v, want a restart without respawn to leave it off", snapshot)
	}
}

func TestStartingAJobCanForwardAndAssociateItsPort(t *testing.T) {
	manager := jobs.New(heldRunner{})
	t.Cleanup(func() { _ = manager.Close() })

	if _, err := manager.Start(t.Context(), "docs", t.TempDir(), "serve old docs", sandbox.Policy{}); err != nil {
		t.Fatal(err)
	}

	ports := &recordedPorts{}
	result, err := callJob(
		t,
		newJobTool(t, manager, ports),
		`{"intent":"check the job here","action":"start","name":"docs","port":8080,"command":"serve docs"}`,
	)
	if err != nil {
		t.Fatal(err)
	}

	wantPublications := []forward.Publication{{Port: 8080, JobName: "docs-1"}}
	if !reflect.DeepEqual(ports.publications, wantPublications) {
		t.Errorf("got publications %#v, want %#v", ports.publications, wantPublications)
	}
	for _, wanted := range []string{
		"docs-1: running",
		"Use http://session.test:8080 for the user",
		"use localhost:8080 inside the sandbox",
	} {
		if !strings.Contains(result.Output, wanted) {
			t.Errorf("got %q, want it to contain %q", result.Output, wanted)
		}
	}
}

func TestAJobIsDiscardedWhenItsPortCannotBeForwarded(t *testing.T) {
	manager := jobs.New(heldRunner{})
	t.Cleanup(func() { _ = manager.Close() })
	refused := errors.New("port is occupied")

	_, err := callJob(
		t,
		newJobTool(t, manager, &recordedPorts{refusal: refused}),
		`{"intent":"check the job here","action":"start","name":"docs","port":8080,"command":"serve docs"}`,
	)
	if !errors.Is(err, refused) || !strings.Contains(err.Error(), "stopped and discarded") {
		t.Errorf("got %v, want the forward refusal and rollback", err)
	}
	if listing := manager.List(); len(listing) != 0 {
		t.Errorf("got jobs %#v, want the failed start discarded", listing)
	}
}

func TestAnUnknownActionIsRefused(t *testing.T) {
	_, err := run(t, jobs.New(nil), map[string]string{"action": "frobnicate", "name": "docs"})
	if err == nil || !strings.Contains(err.Error(), "must be") {
		t.Errorf("got %v, want the actions listed", err)
	}
}

func TestEverySingleJobActionNeedsAName(t *testing.T) {
	for _, action := range []string{"status", "output", "stop", "discard", "start"} {
		if _, err := run(t, jobs.New(nil), map[string]string{"action": action}); err == nil ||
			!strings.Contains(err.Error(), "name is required") {
			t.Errorf("%s gave %v, want it to ask for a name", action, err)
		}
	}
}

func TestStartingValidatesTheJobName(t *testing.T) {
	built := job.New(nil, nil, nil, nil)
	testCases := []struct {
		name    string
		isValid bool
	}{
		{name: "a", isValid: true},
		{name: "abc123", isValid: true},
		{name: "abcdefghij", isValid: true},
		{name: "doc-server", isValid: true},
		{name: "-", isValid: true},
		{name: "abcdefghijk"},
		{name: "Job"},
		{name: "job_name"},
		{name: "two words"},
		{name: "é"},
	}

	for _, testCase := range testCases {
		encoded, err := json.Marshal(map[string]string{
			"intent":  "check the job name here",
			"action":  "start",
			"name":    testCase.name,
			"command": "true",
		})
		if err != nil {
			t.Fatal(err)
		}

		_, err = built.Parse(string(encoded))
		if (err == nil) != testCase.isValid {
			t.Errorf("starting %q gave %v, want validity %v", testCase.name, err, testCase.isValid)
		}
	}
}

func TestOnlyAStartTakesAValidPort(t *testing.T) {
	built := job.New(nil, nil, nil, nil)
	for name, arguments := range map[string]string{
		"port with status": `{"intent":"check the job here","action":"status","name":"docs","port":8080}`,
		"negative port":    `{"intent":"check the job here","action":"start","name":"docs","port":-1,"command":"serve"}`,
		"large port":       `{"intent":"check the job here","action":"start","name":"docs","port":65536,"command":"serve"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := built.Parse(arguments); err == nil {
				t.Error("the port was accepted")
			}
		})
	}

	if _, err := built.Parse(`{"intent":"check the job here","action":"start","name":"docs","port":8080,"command":"serve"}`); err != nil {
		t.Errorf("a valid start port was refused: %v", err)
	}
}

func TestOnlyAStartRespawns(t *testing.T) {
	_, err := run(t, jobs.New(nil), map[string]any{
		"action":  "status",
		"name":    "watch",
		"respawn": true,
	})
	if err == nil || !strings.Contains(err.Error(), `respawn requires action="start"`) {
		t.Errorf("got %v, want respawn to belong to start alone", err)
	}
}

func TestTheRespawnParameterNamesWhenItGivesUp(t *testing.T) {
	var description string
	for _, parameter := range job.New(nil, nil, nil, nil).Schema() {
		if parameter.Name == "respawn" {
			description = parameter.Description
		}
	}

	for _, wanted := range []string{"for action 'start'", "still notifying you", "stop the job", "3 runs in a row", "within 2s"} {
		if !strings.Contains(description, wanted) {
			t.Errorf("respawn description %q does not contain %q", description, wanted)
		}
	}
}

func TestAJobCallIsRenderedByItsAction(t *testing.T) {
	for name, shape := range map[string]struct {
		args job.Args
		want tool.CallRendering
	}{
		"start": {
			args: job.Args{Action: "start", Name: "docs", Command: "python3  -m\nhttp.server"},
			want: tool.CallRendering{
				Kind:          "job_start",
				ReportsStatus: true,
				Subject:       "docs",
				Continuation:  []tool.CallRendering{bash.DescribeCommand("python3  -m\nhttp.server")},
				Introduces:    "docs",
			},
		},
		"start with port": {
			args: job.Args{Action: "start", Name: "docs", Port: 8080, Command: "serve docs"},
			want: tool.CallRendering{
				Kind:          "job_start",
				ReportsStatus: true,
				Subject:       "docs:8080",
				Emphasis:      tool.Emphasis{Kind: tool.EmphasisLead, Value: "docs"},
				Continuation:  []tool.CallRendering{bash.DescribeCommand("serve docs")},
				Introduces:    "docs",
			},
		},
		"restart": {
			args: job.Args{Action: "start", Name: "docs"},
			want: tool.CallRendering{Kind: "job_restart", ReportsStatus: true, Subject: "docs", Introduces: "docs"},
		},
		"restart with port": {
			args: job.Args{Action: "start", Name: "docs", Port: 8080},
			want: tool.CallRendering{
				Kind:          "job_restart",
				ReportsStatus: true,
				Subject:       "docs:8080",
				Emphasis:      tool.Emphasis{Kind: tool.EmphasisLead, Value: "docs"},
				Introduces:    "docs",
			},
		},
		"respawning start": {
			args: job.Args{Action: "start", Name: "watch", Command: "inotifywait .", Respawn: true},
			want: tool.CallRendering{
				Kind:          "job_start",
				ReportsStatus: true,
				Subject:       "watch",
				Qualifier:     "respawning on exit",
				Continuation:  []tool.CallRendering{bash.DescribeCommand("inotifywait .")},
				Introduces:    "watch",
			},
		},
		"respawning restart": {
			args: job.Args{Action: "start", Name: "watch", Respawn: true},
			want: tool.CallRendering{Kind: "job_restart", ReportsStatus: true, Subject: "watch", Qualifier: "respawning on exit", Introduces: "watch"},
		},
		"status": {
			args: job.Args{Action: "status", Name: "docs"},
			want: tool.CallRendering{Kind: "job_status", ReportsStatus: true, Subject: "docs", Mentions: []string{"docs"}},
		},
		"output": {
			args: job.Args{Action: "output", Name: "docs"},
			want: tool.CallRendering{Kind: "job_output", Subject: "docs", Mentions: []string{"docs"}},
		},
		"stop": {
			args: job.Args{Action: "stop", Name: "docs"},
			want: tool.CallRendering{Kind: "job_stop", ReportsStatus: true, Subject: "docs", Mentions: []string{"docs"}},
		},
		"discard": {
			args: job.Args{Action: "discard", Name: "docs"},
			want: tool.CallRendering{Kind: "job_discard", ReportsStatus: true, Subject: "docs", Mentions: []string{"docs"}},
		},
		"list": {
			args: job.Args{Action: "list"},
			want: tool.CallRendering{Kind: "job_list", Subject: "jobs"},
		},
		"prune": {
			args: job.Args{Action: "prune"},
			want: tool.CallRendering{Kind: "job_prune", ReportsStatus: true, Subject: "jobs"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if rendering := job.Describe(shape.args); !reflect.DeepEqual(rendering, shape.want) {
				t.Errorf("got %#v, want %#v", rendering, shape.want)
			}
		})
	}
}

func TestTheNameParameterGivesConciseNamingAdvice(t *testing.T) {
	var description string
	for _, parameter := range job.New(nil, nil, nil, nil).Schema() {
		if parameter.Name == "name" {
			description = parameter.Description
		}
	}

	for _, wanted := range []string{"'check'", "'cachecheck'", "automatically numbered", "'check-1'", "1–10", "[a-z0-9-]"} {
		if !strings.Contains(description, wanted) {
			t.Errorf("name description %q does not contain %q", description, wanted)
		}
	}
}

func TestTheToolSaysToEndTheTurnRatherThanWait(t *testing.T) {
	for _, built := range []tool.Tool{job.New(nil, nil, nil, nil), job.NewOnHost(nil, nil, nil)} {
		if !strings.Contains(built.Description(), job.EndAdvice) {
			t.Errorf("description %q does not say how a job's ending arrives", built.Description())
		}
		for _, parameter := range built.Schema() {
			if slices.Contains([]string{"names", "wait_for", "wait_seconds"}, parameter.Name) {
				t.Errorf("the job tool still offers %s", parameter.Name)
			}
		}
		if _, err := built.Parse(`{"action":"wait","name":"docs"}`); err == nil {
			t.Error("the job tool accepted a wait")
		}
	}
}

func TestTheToolExplainsPortForwardingLimits(t *testing.T) {
	description := job.New(nil, nil, nil, nil).Description()
	for _, wanted := range []string{"bind a fixed port", "pass it as `port`", "does not discover an ephemeral port", "reports the URL"} {
		if !strings.Contains(description, wanted) {
			t.Errorf("description %q does not contain %q", description, wanted)
		}
	}
}

func TestAJobOnTheHostOffersNothingToForward(t *testing.T) {
	onHost := job.NewOnHost(nil, nil, nil)

	for _, parameter := range onHost.Schema() {
		if parameter.Name == "port" {
			t.Error("a job on the host offers a port to forward")
		}
	}
	description := onHost.Description()
	for _, wanted := range []string{"directly on the host"} {
		if !strings.Contains(description, wanted) {
			t.Errorf("description %q does not contain %q", description, wanted)
		}
	}
	if strings.Contains(description, "forward") {
		t.Errorf("description %q promises forwarding", description)
	}
	if _, err := onHost.Parse(`{"action":"start","name":"web","command":"serve","intent":"Serving the site","port":8080}`); err == nil {
		t.Error("a job on the host accepted a port")
	}
}

var _ tool.Tool = job.New(nil, nil, nil, nil)

func TestAJobIsReportedByNameForEveryActionThatNamesOne(t *testing.T) {
	for action, want := range map[string]string{
		"status": "build: failed",
		"output": "build: failed",
		"stop":   "build: failed",
	} {
		t.Run(action, func(t *testing.T) {
			output, err := run(t, withFinishedJobs(t), map[string]string{"action": action, "name": "build"})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(output, want) {
				t.Errorf("got %q, want it to begin %q", output, want)
			}
		})
	}
}

func TestEveryActionThatNamesAJobRefusesOneNobodyStarted(t *testing.T) {
	for _, action := range []string{"status", "output", "stop", "discard"} {
		t.Run(action, func(t *testing.T) {
			if _, err := run(t, withFinishedJobs(t), map[string]string{"action": action, "name": "ghost"}); err == nil {
				t.Error("a job nobody started was reported")
			}
		})
	}
}

func TestRestartingAJobFailsWhenItsPolicyCannotBeBuilt(t *testing.T) {
	built := job.New(withFinishedJobs(t), nil, func(context.Context) (sandbox.Policy, error) {
		return sandbox.Policy{}, errPolicy
	}, nil)

	call, err := built.Parse(`{"intent":"check the job here","action":"start","name":"build"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := call.Exec(t.Context()); !errors.Is(err, errPolicy) {
		t.Errorf("got %v, want the policy's failure", err)
	}
}

var errPolicy = errors.New("no policy")

func withIntent(t *testing.T, encoded []byte) []byte {
	t.Helper()

	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if _, isPresent := fields["intent"]; !isPresent {
		fields["intent"] = "check the job here"
	}

	withIntent, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return withIntent
}
