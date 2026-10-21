package sandbox_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/sandbox"
)

func TestMain(m *testing.M) {
	sandbox.Init()
	os.Exit(m.Run())
}

func TestPolicyModifiersDoNotChangeTheSourcePolicy(t *testing.T) {
	source := sandbox.Policy{
		Read:   []string{"read"},
		Write:  []string{"write"},
		SetEnv: map[string]string{"EXISTING": "original"},
	}

	modified := source.WithRead("more-read").WithWrite("more-write").WithSetEnv("ADDED", "value")
	modified.Read[0] = "changed"
	modified.Write[0] = "changed"
	modified.SetEnv["EXISTING"] = "changed"

	if source.Read[0] != "read" {
		t.Errorf("read paths changed to %v", source.Read)
	}
	if source.Write[0] != "write" {
		t.Errorf("write paths changed to %v", source.Write)
	}
	if source.SetEnv["EXISTING"] != "original" {
		t.Errorf("environment changed to %v", source.SetEnv)
	}
}

func TestWithoutReadRemovesPathsWithoutChangingTheSourcePolicy(t *testing.T) {
	source := sandbox.Policy{Read: []string{"remove", "keep"}}
	modified := source.WithoutRead("remove")

	if !slices.Equal(modified.Read, []string{"keep"}) {
		t.Errorf("got readable paths %v, want only keep", modified.Read)
	}
	modified.Read[0] = "changed"
	if !slices.Equal(source.Read, []string{"remove", "keep"}) {
		t.Errorf("source readable paths changed to %v", source.Read)
	}
}

func requireLandlock(t *testing.T) {
	t.Helper()

	if err := sandbox.Available(); err != nil {
		t.Skipf("landlock is unavailable: %v", err)
	}
}

func TestACancelledContextStartsNoCommand(t *testing.T) {
	directory := t.TempDir()
	marker := filepath.Join(directory, "never-started")
	policy := sandbox.Policy{Write: []string{directory}, Env: []string{"PATH"}}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := sandbox.Run(ctx, directory, "printf started > "+marker, policy)
	if err == nil {
		t.Error("the command with an already cancelled context was allowed to run")
	}
	if content, err := os.ReadFile(marker); err == nil { //nolint:gosec // reading the test's own marker is intended
		t.Errorf("the cancelled command wrote %q", content)
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("could not inspect the command marker: %v", err)
	}
}

func TestAGrantTheScratchWouldCoverIsRefused(t *testing.T) {
	policy := sandbox.Policy{
		TmpDir: t.TempDir(),
		Write:  []string{filepath.Join(sandbox.TmpDir, "elsewhere")},
	}

	_, err := sandbox.Run(t.Context(), t.TempDir(), "true", policy)
	if err == nil || !strings.Contains(err.Error(), "granted but unreachable") {
		t.Errorf("expected the covered grant to be named, got %v", err)
	}
}

func TestTheScratchItselfIsNotRefused(t *testing.T) {
	policy := sandbox.Policy{TmpDir: t.TempDir(), Write: []string{sandbox.TmpDir}}

	_, err := sandbox.Run(t.Context(), t.TempDir(), "true", policy)
	if err != nil && strings.Contains(err.Error(), "granted but unreachable") {
		t.Errorf("expected the scratch to be granted, got %v", err)
	}
}

func TestALimitThatCouldNeverBeMetIsRefused(t *testing.T) {
	requireLandlock(t)

	directory := t.TempDir()

	_, err := sandbox.Run(context.Background(), directory, "true", sandbox.Policy{
		Write:      []string{directory},
		MaxCPUTime: time.Millisecond,
	})

	if err == nil || !strings.Contains(err.Error(), "no time at all") {
		t.Errorf("got %v, want a complaint about the limit", err)
	}
}

func TestAPolicyNamingAMissingPathIsRefused(t *testing.T) {
	requireLandlock(t)

	directory := t.TempDir()

	_, err := sandbox.Run(context.Background(), directory, "true", sandbox.Policy{
		Read: []string{filepath.Join(directory, "nowhere")},
	})

	if err == nil || !strings.Contains(err.Error(), "do not exist") {
		t.Errorf("got %v, want a complaint about the missing path", err)
	}
}

func TestAPolicyWithALimitThatIsNotALimitIsRefusedBeforeAnythingRuns(t *testing.T) {
	for _, policy := range []sandbox.Policy{{MaxFileSize: -1}, {MaxOpenFiles: -1}} {
		if _, err := sandbox.Run(t.Context(), t.TempDir(), "true", policy); err == nil {
			t.Errorf("%+v was accepted", policy)
		}
	}
}

func TestAGrantThroughAModelSymlinkRefusesTheCommand(t *testing.T) {
	requireLandlock(t)

	home := t.TempDir()
	victim := t.TempDir()
	planted := filepath.Join(home, ".cache")
	if err := os.Symlink(victim, planted); err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()
	command := "touch " + filepath.Join(victim, "pwned")

	_, err := sandbox.Run(t.Context(), directory, command, sandbox.Policy{
		Write: []string{home, planted},
		Env:   []string{"PATH"},
	})
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Errorf("got %v, want the planted link to refuse the command", err)
	}

	if _, statErr := os.Stat(filepath.Join(victim, "pwned")); statErr == nil {
		t.Error("the redirected grant wrote outside the sandbox")
	}
}
