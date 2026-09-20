package jobs

import (
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/internal/sandbox/keeper"
)

func TestMain(m *testing.M) {
	sandbox.Init()
	m.Run()
}

func openRunner(t *testing.T) (sandbox.Runner, func()) {
	t.Helper()

	if err := sandbox.Supported(t.Context()); err != nil {
		t.Skipf("the sandbox cannot be built here: %v", err)
	}

	keeperProcess, err := keeper.Open(t.Context())
	if err != nil {
		t.Fatalf("could not open the keeper: %v", err)
	}

	return sandbox.Wrapped(keeperProcess), func() { _ = keeperProcess.Close() }
}

func TestTheKeeperRunsACommandAndReportsItsOutput(t *testing.T) {
	if err := sandbox.Supported(t.Context()); err != nil {
		t.Skipf("the sandbox cannot be built here: %v", err)
	}

	runner, shut := openRunner(t)
	defer shut()

	policy := sandbox.Policy{Env: []string{"PATH"}, Timeout: 10 * time.Second}

	result, err := runner.Run(t.Context(), t.TempDir(), "echo hello", policy)
	if err != nil {
		t.Fatalf("the command failed: %v", err)
	}
	if result.ExitCode != 0 || strings.TrimSpace(result.Output) != "hello" {
		t.Errorf("got exit %d with output %q", result.ExitCode, result.Output)
	}
}

func TestAnOrdinaryCommandReachesAListenerAJobLeftBehind(t *testing.T) {
	if err := sandbox.Supported(t.Context()); err != nil {
		t.Skipf("the sandbox cannot be built here: %v", err)
	}

	runner, shut := openRunner(t)
	defer shut()

	policy := sandbox.Policy{Env: []string{"PATH"}, Timeout: 30 * time.Second}
	manager := New(runner)

	jobPolicy := policy
	jobPolicy.Timeout = 0

	if _, err := manager.Start(t.Context(), "listener", t.TempDir(), "exec python3 -m http.server 21931 --bind 127.0.0.1", jobPolicy); err != nil {
		t.Fatalf("could not start the listener: %v", err)
	}

	time.Sleep(2 * time.Second)

	result, err := runner.Run(t.Context(), t.TempDir(), "exec 3<>/dev/tcp/127.0.0.1/21931 && printf reached", policy)
	if err != nil {
		t.Fatalf("the bash call failed: %v — listener said %q", err, mustOutput(t, manager, "listener"))
	}

	if !strings.Contains(result.Output, "reached") {
		t.Errorf("got %q, want an ordinary command to reach the job's listener", result.Output)
	}
}

func mustOutput(t *testing.T, manager *Manager, name string) string {
	t.Helper()
	text, _, _ := manager.Output(name)
	return text
}

func TestWaitingHoldsUntilARunningJobHasFinished(t *testing.T) {
	if err := sandbox.Supported(t.Context()); err != nil {
		t.Skipf("the sandbox cannot be built here: %v", err)
	}

	runner, shut := openRunner(t)
	defer shut()

	manager := New(runner)
	defer func() { _ = manager.Close() }()

	if _, err := manager.Start(t.Context(), "brief", t.TempDir(), "sleep 1; echo over", sandbox.Policy{Env: []string{"PATH"}}); err != nil {
		t.Fatalf("could not start the job: %v", err)
	}

	startedAt := time.Now()
	name, err := manager.Wait(t.Context(), []string{"brief"})
	if err != nil {
		t.Fatalf("the wait failed: %v", err)
	}
	if name != "brief" {
		t.Errorf("got %q, want the finished job's name", name)
	}
	elapsed := time.Since(startedAt)

	snapshot, err := manager.Status("brief")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State != StateComplete {
		t.Errorf("got state %s, want the wait to return only once the job was complete", snapshot.State)
	}
	if elapsed < time.Second {
		t.Errorf("the wait returned after %s, want it to have held for the second the job ran", elapsed)
	}
	if !strings.Contains(mustOutput(t, manager, "brief"), "over") {
		t.Errorf("got %q, want everything the job printed to be there once the wait returns", mustOutput(t, manager, "brief"))
	}
}

func TestClosingTheManagerKillsAJobThatIsStillRunning(t *testing.T) {
	if err := sandbox.Supported(t.Context()); err != nil {
		t.Skipf("the sandbox cannot be built here: %v", err)
	}

	runner, shut := openRunner(t)
	defer shut()

	manager := New(runner)
	policy := sandbox.Policy{Env: []string{"PATH"}}
	directory := t.TempDir()

	if _, err := manager.Start(t.Context(), "sleeper", directory, "exec sleep 60", policy); err != nil {
		t.Fatalf("could not start the job: %v", err)
	}

	startedAt := time.Now()
	if err := manager.Close(); err != nil {
		t.Fatalf("could not close the manager: %v", err)
	}

	if elapsed := time.Since(startedAt); elapsed > shutdownGracePeriod+2*time.Second {
		t.Errorf("closing took %s, want the shutdown grace period at most", elapsed)
	}

	snapshot, err := manager.Status("sleeper")
	if err != nil {
		t.Fatalf("could not read the job: %v", err)
	}
	if snapshot.IsLive() {
		t.Errorf("got state %s, want the job stopped with the session", snapshot.State)
	}
}

func TestSeveralJobsAreClosedTogetherRatherThanOneAfterAnother(t *testing.T) {
	if err := sandbox.Supported(t.Context()); err != nil {
		t.Skipf("the sandbox cannot be built here: %v", err)
	}

	runner, shut := openRunner(t)
	defer shut()

	manager := New(runner)
	policy := sandbox.Policy{Env: []string{"PATH"}}

	for _, name := range []string{"one", "two", "three"} {
		if _, err := manager.Start(t.Context(), name, t.TempDir(), "exec sleep 60", policy); err != nil {
			t.Fatalf("could not start %s: %v", name, err)
		}
	}

	startedAt := time.Now()
	if err := manager.Close(); err != nil {
		t.Fatalf("could not close the manager: %v", err)
	}

	if elapsed := time.Since(startedAt); elapsed > shutdownGracePeriod+2*time.Second {
		t.Errorf("closing three jobs took %s, want them ended together rather than in turn", elapsed)
	}
}

func TestAJobThatHandlesTerminationStopsWithoutWaitingForTheGracePeriod(t *testing.T) {
	if err := sandbox.Supported(t.Context()); err != nil {
		t.Skipf("the sandbox cannot be built here: %v", err)
	}

	runner, shut := openRunner(t)
	defer shut()

	manager := New(runner)
	policy := sandbox.Policy{Env: []string{"PATH"}}
	command := "trap 'exit 0' TERM; while :; do sleep 0.1; done"

	if _, err := manager.Start(t.Context(), "polite", t.TempDir(), command, policy); err != nil {
		t.Fatalf("could not start the job: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	startedAt := time.Now()
	if _, err := manager.Stop("polite"); err != nil {
		t.Fatalf("could not stop the job: %v", err)
	}

	if elapsed := time.Since(startedAt); elapsed >= normalGracePeriod {
		t.Errorf("stopping took %s, want a handled termination to be prompt", elapsed)
	}
}
