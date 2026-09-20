package job_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"crdx.org/io/internal/jobs"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/pkg/tool"
	"crdx.org/io/pkg/toolbox/bash"
	"crdx.org/io/pkg/toolbox/job"
)

func run(t *testing.T, manager *jobs.Manager, arguments any) (string, error) {
	t.Helper()

	encoded, err := json.Marshal(arguments)
	if err != nil {
		t.Fatal(err)
	}

	built := job.New(manager, nil, func(context.Context) (sandbox.Policy, error) {
		return sandbox.Policy{}, nil
	}, false)

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
	built := job.New(nil, nil, nil, false)
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
	if err == nil || !strings.Contains(err.Error(), "must be any or all") {
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

func TestAJobCallIsRenderedByItsSubject(t *testing.T) {
	subject, qualifier := job.Describe(job.Args{Action: "start", Name: "docs", Command: "python3  -m\nhttp.server"})
	if subject != "docs" || qualifier != "" {
		t.Errorf("got %q / %q, want only the job name in the primary rendering", subject, qualifier)
	}

	parsedCall, err := job.New(nil, nil, nil, false).Parse(
		`{"action":"start","name":"docs","command":"python3  -m\nhttp.server"}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	wantContinuation := []tool.CallRendering{bash.DescribeCommand("python3  -m\nhttp.server")}
	if continuation := parsedCall.Continuation(); !reflect.DeepEqual(continuation, wantContinuation) {
		t.Errorf("got %#v, want the bash command continuation %#v", continuation, wantContinuation)
	}

	subject, qualifier = job.Describe(job.Args{Action: "status", Name: "docs"})
	if subject != "docs" || qualifier != "status" {
		t.Errorf("got %q / %q, want the name and the action", subject, qualifier)
	}

	subject, qualifier = job.Describe(job.Args{Action: "start", Name: "docs"})
	if subject != "docs" || qualifier != "start" {
		t.Errorf("got %q / %q, want a restart to read by name", subject, qualifier)
	}

	subject, qualifier = job.Describe(job.Args{Action: "wait", Names: []string{"build", "lint"}})
	if subject != "build, lint" || qualifier != "wait" {
		t.Errorf("got %q / %q, want every watched job named", subject, qualifier)
	}
}

func TestTheNameParameterStatesTheNameLimit(t *testing.T) {
	var description string
	for _, parameter := range job.New(nil, nil, nil, false).Schema() {
		if parameter.Name == "name" {
			description = parameter.Description
		}
	}

	for _, wanted := range []string{"1–10", "[a-z0-9-]"} {
		if !strings.Contains(description, wanted) {
			t.Errorf("name description %q does not contain %q", description, wanted)
		}
	}
}

func TestTheToolSaysWhetherAnEndedJobWakesTheConversation(t *testing.T) {
	waking := job.New(nil, nil, nil, true).Description()
	if !strings.Contains(waking, "You will be notified automatically when it finishes") {
		t.Errorf("got %q, want a waking harness to promise the model it will be told", waking)
	}

	sleeping := job.New(nil, nil, nil, false).Description()
	if !strings.Contains(sleeping, "You will not be notified automatically when it finishes") {
		t.Errorf("got %q, want no promise of waking a harness that does not wake", sleeping)
	}
}

var _ tool.Tool = job.New(nil, nil, nil, false)
