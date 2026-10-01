package prompt

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/conditions"
	"crdx.org/oh/internal/app/work"
)

var workflowCommand = regexp.MustCompile(`(?m)^    - (Check if a standalone patch is already applied|Verify a standalone patch applies|Tell the user to apply it with): (?:/!)?(.+)$`)

func TestTheWorkflowsCommandsJudgeAWorkspaceInsideARepositoryTruthfully(t *testing.T) {
	repository := t.TempDir()
	workspaceDirectory := filepath.Join(repository, "config", "kitty")
	if err := os.MkdirAll(workspaceDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repository, "init", "-q")
	configPath := filepath.Join(workspaceDirectory, "open.conf")
	if err := os.WriteFile(configPath, []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	patchPath := filepath.Join(t.TempDir(), "change.patch")
	text := "diff --git a/open.conf b/open.conf\n--- a/open.conf\n+++ b/open.conf\n@@ -1 +1 @@\n-before\n+after\n"
	if err := os.WriteFile(patchPath, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}

	got := harnessContext(Config{
		Workspace:    work.At(workspaceDirectory),
		SessionName:  "session-id",
		TmpDir:       "/state/farm/session",
		HomeDir:      "/state/home",
		CurrentCaps:  caps.Read | caps.Shell,
		OfferedTools: []string{"bash"},
		Conditions:   conditions.Conditions{Interactive: true},
	})
	commands := map[string]string{}
	for _, match := range workflowCommand.FindAllStringSubmatch(got, -1) {
		command := strings.NewReplacer(
			"<workspace>", workspaceDirectory,
			"<patch>", patchPath,
			"<user's path to patch>", patchPath,
		).Replace(match[2])
		commands[match[1]] = command
	}
	if len(commands) != 3 {
		t.Fatalf("found the workflow commands %q, want all three", commands)
	}
	isApplied := func() bool {
		return bashIn(t, workspaceDirectory, commands["Check if a standalone patch is already applied"]) == nil
	}

	if bashIn(t, workspaceDirectory, "git -C "+workspaceDirectory+" apply --reverse --check "+patchPath) != nil {
		t.Fatal("git no longer skips a nested workspace's paths, so this test proves nothing")
	}
	if isApplied() {
		t.Error("the patch is reported applied before it was")
	}
	if err := bashIn(t, workspaceDirectory, commands["Verify a standalone patch applies"]); err != nil {
		t.Errorf("the patch is reported not to apply: %v", err)
	}
	if err := bashIn(t, workspaceDirectory, commands["Tell the user to apply it with"]); err != nil {
		t.Fatalf("the handoff did not apply the patch: %v", err)
	}
	if content, err := os.ReadFile(configPath); err != nil || string(content) != "after\n" { //nolint:gosec // test fixture
		t.Errorf("the handoff left %q (%v), want the patched file", content, err)
	}
	if !isApplied() {
		t.Error("the patch is not reported applied after it was")
	}
}

func gitIn(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", arguments...) //nolint:gosec // test-owned arguments
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
}

func bashIn(t *testing.T, directory string, script string) error {
	t.Helper()
	command := exec.CommandContext(t.Context(), "bash", "-c", script) //nolint:gosec // the workflow's own commands
	command.Dir = directory
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	return command.Run()
}
