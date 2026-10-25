package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/startup"
	"crdx.org/oh/internal/app/style"
)

const startupDenyConfig = "[grants]\ndeny = [\"*.key\"]\n"

func largeDenyWorkspace(t *testing.T) string {
	t.Helper()

	workspace := reachableWorkspaceDir(t)
	for i := range startup.MaxDenyWorkspaceEntries + 1 {
		name := filepath.Join(workspace, fmt.Sprintf("entry-%06d", i))
		if err := os.WriteFile(name, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return workspace
}

func TestGoldenStartupChecksNameDenyWorkspaceSize(t *testing.T) {
	workspace := largeDenyWorkspace(t)
	newRun := func(answers ...string) *startupRun {
		run := newStartupRun(t, startupDenyConfig, answers...)
		run.rig.workspace = workspace
		return run
	}
	compareWithGolden(t, "startup-deny-workspace", ".txt", map[string]func() string{
		"a large workspace refuses name denies with shell access": func() string {
			run := newRun()
			output := run.run(t, "-p", "-cx", "-m", "opencode-go/fake", "a question")
			return strings.ReplaceAll(output, workspace, "<workspace>")
		},
		"new and resumed yolo sessions ignore the workspace size": func() string {
			run := newRun("Yolo answer.", "Resumed yolo answer.")
			first := run.run(t, "-p", "--yolo", "-m", "opencode-go/fake", "a question")
			name := run.onlySessionName(t)
			resumed := run.run(t, "-p", "-r", name, "another question")
			return strings.ReplaceAll(first+resumed, name, "<session>")
		},
	})
}

func TestDenyWorkspaceExemptionsReachSandboxValidation(t *testing.T) {
	workspace := largeDenyWorkspace(t)
	sandboxError := shell.RequireSandbox(t.Context())
	for _, test := range []struct {
		name      string
		config    string
		arguments []string
		isSmall   bool
	}{
		{name: "forced", config: startupDenyConfig, arguments: []string{"-cx", "-f"}},
		{name: "no shell", config: startupDenyConfig, arguments: []string{"-cr"}},
		{
			name:      "absolute deny",
			config:    fmt.Sprintf("[grants]\ndeny = [%q]\n", filepath.Join(workspace, "entry-000000")),
			arguments: []string{"-cx"},
		},
		{name: "no denies", arguments: []string{"-cx"}},
		{name: "unreadable directory", config: startupDenyConfig, arguments: []string{"-cx"}, isSmall: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			run := newStartupRun(t, test.config, "Accepted answer.")
			if test.isSmall {
				hidden := filepath.Join(run.rig.workspace, "unreadable")
				if err := os.Mkdir(hidden, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(hidden, 0o700) }) //nolint:gosec // Restore access to the test directory.
			} else {
				run.rig.workspace = workspace
			}
			arguments := append([]string{"-p"}, test.arguments...)
			arguments = append(arguments, "-m", "opencode-go/fake", "a question")
			output := run.run(t, arguments...)
			if strings.Contains(output, "deny-search check") {
				t.Fatalf("an exempt startup searched the workspace:\n%s", output)
			}
			if sandboxError != nil {
				if !strings.Contains(strings.ToLower(output), strings.ToLower(style.Plain(style.Error(sandboxError)))) || !strings.Contains(output, "[exit 1]") {
					t.Fatalf("startup did not reach the sandbox validation:\n%s", output)
				}
				return
			}
			if !strings.Contains(output, "Accepted answer.") || !strings.Contains(output, "[exit 0]") {
				t.Fatalf("startup failed after the workspace check:\n%s", output)
			}
		})
	}
}
