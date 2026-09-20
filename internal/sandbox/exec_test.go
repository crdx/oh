package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/stop"
)

func TestOnlyTheNamedEnvironmentIsPassedOn(t *testing.T) {
	t.Setenv("IO_SANDBOX_PRESENT", "value")

	environment := getEnvironment([]string{"IO_SANDBOX_PRESENT", "IO_SANDBOX_ABSENT"})

	if !slices.Equal(environment, []string{"IO_SANDBOX_PRESENT=value"}) {
		t.Errorf("got %v, want only the variable that is set", environment)
	}
}

func TestASetVariableReplacesTheOneItNames(t *testing.T) {
	t.Setenv("IO_SANDBOX_PRESENT", "parent")

	environment := configuredEnvironment(
		[]string{"IO_SANDBOX_PRESENT"},
		map[string]string{"IO_SANDBOX_PRESENT": "chosen"},
	)

	if !slices.Equal(environment, []string{"IO_SANDBOX_PRESENT=chosen"}) {
		t.Errorf("got %v, want the parent's value replaced exactly once", environment)
	}
}

func TestSetVariablesAreWrittenInAFixedOrder(t *testing.T) {
	set := map[string]string{"THIRD": "3", "FIRST": "1", "SECOND": "2"}

	first := configuredEnvironment(nil, set)
	second := configuredEnvironment(nil, set)

	if !slices.Equal(first, []string{"FIRST=1", "SECOND=2", "THIRD=3"}) {
		t.Errorf("got %v, want the names in order", first)
	}
	if !slices.Equal(first, second) {
		t.Errorf("got %v then %v, want the same environment twice", first, second)
	}
}

func TestAPolicySurvivesBeingWrittenAndReadBack(t *testing.T) {
	policy := Policy{
		Read:         []string{"/read"},
		Write:        []string{"/write"},
		Exec:         []string{"/exec"},
		TmpDir:       "/scratch",
		Env:          []string{"PATH"},
		SetEnv:       map[string]string{"NAME": "value"},
		Timeout:      time.Second,
		MaxCPUTime:   2 * time.Second,
		MaxFileSize:  1024,
		MaxOpenFiles: 64,
		MaxProcesses: 128,
	}

	encoded, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}

	var got Policy
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}

	if got.TmpDir != policy.TmpDir || got.Timeout != policy.Timeout || got.MaxCPUTime != policy.MaxCPUTime {
		t.Errorf("got %+v, want %+v", got, policy)
	}
	if got.MaxFileSize != policy.MaxFileSize || got.MaxOpenFiles != policy.MaxOpenFiles {
		t.Errorf("got %+v, want %+v", got, policy)
	}
	if got.MaxProcesses != policy.MaxProcesses {
		t.Errorf("got %+v, want %+v", got, policy)
	}
	if !slices.Equal(got.Read, policy.Read) || !slices.Equal(got.Write, policy.Write) {
		t.Errorf("got %+v, want %+v", got, policy)
	}
	if !slices.Equal(got.Exec, policy.Exec) || got.SetEnv["NAME"] != "value" {
		t.Errorf("got %+v, want %+v", got, policy)
	}
}

func TestOutputIsKeptUpToTheLimitAndAcceptedBeyondIt(t *testing.T) {
	var output boundedBuffer

	const chunk = 1 << 20

	written := 0
	for range 3 * maxOutput / chunk {
		count, err := output.Write(bytes.Repeat([]byte("x"), chunk))
		if err != nil {
			t.Fatalf("got %v, want a write a command can never be blocked by", err)
		}
		written += count
	}

	if written != 3*maxOutput {
		t.Errorf("got %d bytes accepted, want every one of %d", written, 3*maxOutput)
	}

	if len(output.String()) != maxOutput {
		t.Errorf("got %d bytes kept, want no more than %d", len(output.String()), maxOutput)
	}
}

func TestOutputWithinTheLimitIsKeptWhole(t *testing.T) {
	var output boundedBuffer

	if _, err := output.Write([]byte("hello ")); err != nil {
		t.Fatal(err)
	}
	if _, err := output.Write([]byte("world")); err != nil {
		t.Fatal(err)
	}

	if output.String() != "hello world" {
		t.Errorf("got %q, want the writes in the order they arrived", output.String())
	}
}

func TestACommandStoppedByItsTimeoutSaysSo(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 0)
	defer cancel()
	<-ctx.Done()

	result, err := stoppedResult(ctx, Policy{Timeout: time.Second}, Result{Output: "partial"}, time.Now())

	if err == nil || !strings.Contains(err.Error(), "did not finish within 1s") {
		t.Errorf("got %v, want a complaint about the timeout", err)
	}
	if result.Output != "partial" {
		t.Errorf("got %q, want what the command managed to write", result.Output)
	}
}

func TestACommandStoppedByItsCallerDoesNotBlameTheTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := stoppedResult(ctx, Policy{Timeout: time.Hour}, Result{}, time.Now())

	if err == nil || !strings.HasPrefix(err.Error(), "the command was stopped after ") {
		t.Errorf("got %v, want a plain stop", err)
	}
	if strings.Contains(err.Error(), "because") {
		t.Errorf("got %v, want no reason when the caller gave no reason", err)
	}
}

func TestACommandStoppedForAReasonRepeatsIt(t *testing.T) {
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(stop.Because("the user pressed escape"))

	result, err := stoppedResult(ctx, Policy{}, Result{Output: "partial"}, time.Now().Add(-12*time.Second))

	want := "the command was stopped after 12s because the user pressed escape"
	if err == nil || err.Error() != want {
		t.Errorf("got %v, want %q", err, want)
	}
	if result.Output != "partial" {
		t.Errorf("got %q, want what the command managed to write", result.Output)
	}
}

func TestAYoloCommandRunsWithNothingAroundIt(t *testing.T) {
	result, err := Run(
		t.Context(),
		t.TempDir(),
		"printf %s \"$IO_SANDBOX_MARKER\"",
		Policy{
			Yolo:    true,
			SetEnv:  map[string]string{"IO_SANDBOX_MARKER": "yolo"},
			Timeout: time.Minute,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.Output != "yolo" {
		t.Errorf("got exit %d and %q, want the command's own output", result.ExitCode, result.Output)
	}
}

func TestAYoloCommandIsNotHeldToAPolicyItCannotKeep(t *testing.T) {
	unkeepable := Policy{
		Yolo:    true,
		Read:    []string{"/there/is/no/such/path"},
		Timeout: time.Minute,
	}

	if err := validate(t.Context(), unkeepable); err == nil {
		t.Fatal("expected a confined run of this policy to be refused")
	}

	result, err := Run(t.Context(), t.TempDir(), "exit 3", unkeepable)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 3 {
		t.Errorf("got exit %d, want the command's own status", result.ExitCode)
	}
}

func TestAYoloCommandIsStoppedWhenItRunsOutOfTime(t *testing.T) {
	result, err := Run(
		t.Context(),
		t.TempDir(),
		"sleep 30",
		Policy{Yolo: true, Env: []string{"PATH"}, Timeout: 100 * time.Millisecond},
	)
	if err == nil || !strings.Contains(err.Error(), "did not finish within") {
		t.Fatalf("got %v and %v, want the command stopped at its deadline", result, err)
	}
}
