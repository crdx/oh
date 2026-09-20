package jobs

import (
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/sandbox"
)

const answeredWithin = 20 * time.Second

func answerFor(t *testing.T, runner sandbox.Runner, command string) (sandbox.Result, error) {
	t.Helper()

	type answer struct {
		result sandbox.Result
		err    error
	}

	answered := make(chan answer, 1)

	go func() {
		result, err := runner.Run(t.Context(), t.TempDir(), command, sandbox.Policy{
			Env:     []string{"PATH"},
			Timeout: 10 * time.Second,
		})
		answered <- answer{result: result, err: err}
	}()

	select {
	case given := <-answered:
		return given.result, given.err
	case <-time.After(answeredWithin):
		t.Fatalf("a command of %d bytes never came back", len(command))
	}

	return sandbox.Result{}, nil
}

func TestALongCommandStillRuns(t *testing.T) {
	if err := sandbox.Supported(t.Context()); err != nil {
		t.Skipf("the sandbox cannot be built here: %v", err)
	}

	runner, shut := openRunner(t)
	defer shut()

	for _, size := range []int{16 << 10, 32 << 10, 40 << 10} {
		result, err := answerFor(t, runner, "true # "+strings.Repeat("x", size))
		if err != nil {
			t.Errorf("a command of %d bytes failed: %v", size, err)
		}
		if result.ExitCode != 0 {
			t.Errorf("a command of %d bytes exited %d", size, result.ExitCode)
		}
	}
}

func TestACommandTooLongToSendIsRefusedRatherThanHanging(t *testing.T) {
	if err := sandbox.Supported(t.Context()); err != nil {
		t.Skipf("the sandbox cannot be built here: %v", err)
	}

	runner, shut := openRunner(t)
	defer shut()

	_, err := answerFor(t, runner, "true # "+strings.Repeat("x", 150<<10))
	if err == nil {
		t.Fatal("an oversized command was accepted")
	}
	if !strings.Contains(err.Error(), "too long to reach the keeper") {
		t.Errorf("got %v, want it to say the command could not be sent", err)
	}
}
