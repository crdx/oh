package job_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
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

func TestAJobIntentIsAcceptedWithoutChangingItsRendering(t *testing.T) {
	built := job.New(nil, nil, nil, nil)
	withoutIntent, err := built.Parse(`{"action":"start","name":"docs","command":"serve docs"}`)
	if err != nil {
		t.Fatal(err)
	}
	withIntent, err := built.Parse(`{"action":"start","name":"docs","command":"serve docs","intent":"serve the documentation"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(withIntent.Rendering(), withoutIntent.Rendering()) {
		t.Errorf("got %#v, want intent to leave rendering %#v", withIntent.Rendering(), withoutIntent.Rendering())
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
		`{"action":"start","name":"docs","port":8080,"command":"serve docs"}`,
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
		`{"action":"start","name":"docs","port":8080,"command":"serve docs"}`,
	)
	if !errors.Is(err, refused) || !strings.Contains(err.Error(), "stopped and discarded") {
		t.Errorf("got %v, want the forward refusal and rollback", err)
	}
	if listing := manager.List(); len(listing) != 0 {
		t.Errorf("got jobs %#v, want the failed start discarded", listing)
	}
}

func TestWaitingOnAFinishedJobReportsItAtOnce(t *testing.T) {
	output, err := run(t, withFinishedJobs(t), map[string]string{"action": "wait", "name": "build"})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(output, "build: failed") {
		t.Errorf("got %q, want the job reported without any waiting at all", output)
	}
}

func TestWaitingForAnyFinishedJobReportsOnlyTheFirstOne(t *testing.T) {
	output, err := run(t, withFinishedJobs(t), map[string]any{
		"action": "wait",
		"names":  []string{"watch", "build"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(output, "watch: complete") || strings.Contains(output, "build: failed") {
		t.Errorf("got %q, want only the first requested finished job", output)
	}
}

func TestWaitingForAllFinishedJobsReportsEveryOne(t *testing.T) {
	output, err := run(t, withFinishedJobs(t), map[string]any{
		"action":   "wait",
		"names":    []string{"watch", "build"},
		"wait_for": "all",
	})
	if err != nil {
		t.Fatal(err)
	}

	watchPosition := strings.Index(output, "watch: complete")
	buildPosition := strings.Index(output, "build: failed")
	if watchPosition < 0 || buildPosition < watchPosition {
		t.Errorf("got %q, want every job in the requested order", output)
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

func TestAWaitNeedsEitherNameOrNames(t *testing.T) {
	if _, err := run(t, jobs.New(nil), map[string]string{"action": "wait"}); err == nil ||
		!strings.Contains(err.Error(), "wait requires name or names") {
		t.Errorf("got %v, want the wait to ask what to watch", err)
	}
}

func TestAWaitRefusesNameTogetherWithNames(t *testing.T) {
	_, err := run(t, jobs.New(nil), map[string]any{
		"action": "wait",
		"name":   "build",
		"names":  []string{"docs"},
	})
	if err == nil || !strings.Contains(err.Error(), "cannot both be set") {
		t.Errorf("got %v, want the two forms to be refused together", err)
	}
}

func TestAWaitRefusesARepeatedName(t *testing.T) {
	_, err := run(t, jobs.New(nil), map[string]any{
		"action": "wait",
		"names":  []string{"build", "build"},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate name") {
		t.Errorf("got %v, want the repeated name to be refused", err)
	}
}

func TestAWaitRefusesAnUnknownWaitForValue(t *testing.T) {
	_, err := run(t, jobs.New(nil), map[string]string{
		"action":   "wait",
		"name":     "build",
		"wait_for": "most",
	})
	if err == nil || !strings.Contains(err.Error(), "wait_for must be one of: any, all") {
		t.Errorf("got %v, want the wait_for values to be named", err)
	}
}

func TestAWaitRefusesANegativeNumberOfSeconds(t *testing.T) {
	_, err := run(t, jobs.New(nil), map[string]any{
		"action":       "wait",
		"name":         "build",
		"wait_seconds": -1,
	})
	if err == nil || !strings.Contains(err.Error(), "must be positive") {
		t.Errorf("got %v, want the negative wait to be refused", err)
	}
}

func TestOnlyAWaitTakesANumberOfSeconds(t *testing.T) {
	_, err := run(t, jobs.New(nil), map[string]any{
		"action":       "status",
		"name":         "build",
		"wait_seconds": 5,
	})
	if err == nil || !strings.Contains(err.Error(), "wait_seconds requires action=\"wait\"") {
		t.Errorf("got %v, want wait_seconds to belong to wait alone", err)
	}
}

func TestOnlyAStartTakesAValidPort(t *testing.T) {
	built := job.New(nil, nil, nil, nil)
	for name, arguments := range map[string]string{
		"port with status": `{"action":"status","name":"docs","port":8080}`,
		"negative port":    `{"action":"start","name":"docs","port":-1,"command":"serve"}`,
		"large port":       `{"action":"start","name":"docs","port":65536,"command":"serve"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := built.Parse(arguments); err == nil {
				t.Error("the port was accepted")
			}
		})
	}

	if _, err := built.Parse(`{"action":"start","name":"docs","port":8080,"command":"serve"}`); err != nil {
		t.Errorf("a valid start port was refused: %v", err)
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
				Kind:         "job_start",
				Subject:      "docs",
				Continuation: []tool.CallRendering{bash.DescribeCommand("python3  -m\nhttp.server")},
			},
		},
		"start with port": {
			args: job.Args{Action: "start", Name: "docs", Port: 8080, Command: "serve docs"},
			want: tool.CallRendering{
				Kind:         "job_start",
				Subject:      "docs:8080",
				Emphasis:     tool.Emphasis{Kind: tool.EmphasisLead, Value: "docs"},
				Continuation: []tool.CallRendering{bash.DescribeCommand("serve docs")},
			},
		},
		"restart": {
			args: job.Args{Action: "start", Name: "docs"},
			want: tool.CallRendering{Kind: "job_restart", Subject: "docs"},
		},
		"restart with port": {
			args: job.Args{Action: "start", Name: "docs", Port: 8080},
			want: tool.CallRendering{
				Kind:     "job_restart",
				Subject:  "docs:8080",
				Emphasis: tool.Emphasis{Kind: tool.EmphasisLead, Value: "docs"},
			},
		},
		"status": {
			args: job.Args{Action: "status", Name: "docs"},
			want: tool.CallRendering{Kind: "job_status", Subject: "docs"},
		},
		"output": {
			args: job.Args{Action: "output", Name: "docs"},
			want: tool.CallRendering{Kind: "job_output", Subject: "docs"},
		},
		"stop": {
			args: job.Args{Action: "stop", Name: "docs"},
			want: tool.CallRendering{Kind: "job_stop", Subject: "docs"},
		},
		"discard": {
			args: job.Args{Action: "discard", Name: "docs"},
			want: tool.CallRendering{Kind: "job_discard", Subject: "docs"},
		},
		"wait any": {
			args: job.Args{Action: "wait", Names: []string{"build", "lint"}},
			want: tool.CallRendering{Kind: "job_wait_any", Subject: "build || lint"},
		},
		"wait all": {
			args: job.Args{Action: "wait", Names: []string{"build", "lint"}, WaitFor: "all"},
			want: tool.CallRendering{Kind: "job_wait_all", Subject: "build && lint"},
		},
		"wait any with limit": {
			args: job.Args{Action: "wait", Names: []string{"build", "lint"}, WaitFor: "any", WaitSeconds: 20},
			want: tool.CallRendering{Kind: "job_wait_any", Subject: "build || lint", Qualifier: "for up to 20s"},
		},
		"wait with formatted limit": {
			args: job.Args{Action: "wait", Name: "build", WaitSeconds: 270},
			want: tool.CallRendering{Kind: "job_wait_any", Subject: "build", Qualifier: "for up to 4m 30s"},
		},
		"wait with clamped limit": {
			args: job.Args{Action: "wait", Name: "build", WaitSeconds: 300},
			want: tool.CallRendering{Kind: "job_wait_any", Subject: "build", Qualifier: "for up to 4m 30s"},
		},
		"list": {
			args: job.Args{Action: "list"},
			want: tool.CallRendering{Kind: "job_list", Subject: "jobs"},
		},
		"prune": {
			args: job.Args{Action: "prune"},
			want: tool.CallRendering{Kind: "job_prune", Subject: "jobs"},
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

func TestTheWaitParameterFormatsItsMaximumAsADuration(t *testing.T) {
	var description string
	for _, parameter := range job.New(nil, nil, nil, nil).Schema() {
		if parameter.Name == "wait_seconds" {
			description = parameter.Description
		}
	}

	if !strings.Contains(description, "max 4m 30s") {
		t.Errorf("wait_seconds description %q does not format its maximum as a duration", description)
	}
}

func TestTheToolExplainsJobNotificationsInBothSessionModes(t *testing.T) {
	description := job.New(nil, nil, nil, nil).Description()
	for _, wanted := range []string{"In an interactive session", "in a non-interactive session"} {
		if !strings.Contains(description, wanted) {
			t.Errorf("description %q does not contain %q", description, wanted)
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

	call, err := built.Parse(`{"action":"start","name":"build"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := call.Exec(t.Context()); !errors.Is(err, errPolicy) {
		t.Errorf("got %v, want the policy's failure", err)
	}
}

var errPolicy = errors.New("no policy")
