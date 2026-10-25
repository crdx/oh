package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/tool/middleware/truncate"
)

func TestGoldenScratchToolsUseBothPathsWithoutWorkspaceWrite(t *testing.T) {
	workspace := work.At(reachableWorkspaceDir(t))
	if err := workspace.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	farm := reachableWorkspaceDir(t)
	scratch := filepath.Join(farm, "tame-impala")
	if err := os.Mkdir(scratch, 0o700); err != nil {
		t.Fatal(err)
	}
	mode := caps.NewMode(caps.Read)
	state, err := buildAgentState(agentOptions{
		workspace:        workspace,
		mode:             mode,
		paths:            shell.Paths{Read: []string{farm}, Deny: []string{"*.key"}},
		roots:            openTestRoots(t, reachableWorkspaceDir(t), scratch),
		refuseHomeWrite:  shell.RefuseHomeWrite(mode),
		sessionDirectory: t.TempDir(),
		ensurePersisted:  func() error { return nil },
		outputLimit:      truncate.NewLimit(agentStateOutputLimit),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(state.Close)
	tools, err := state.tools(loudTool("bash"), agentFeatures{})
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]tool.Tool)
	for _, subject := range tools {
		byName[subject.Name()] = subject
	}
	metadata := filepath.Join(workspace.GetDir(), ".git")
	if err := os.MkdirAll(metadata, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metadata, "config"), []byte("intact"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(metadata, filepath.Join(scratch, "git-link")); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	for _, step := range []struct {
		name       string
		arguments  string
		isWritable bool
	}{
		{name: "write", arguments: `{"path":"/tmp/virtual.txt","content":"virtual\n"}`, isWritable: true},
		{name: "edit", arguments: fmt.Sprintf(`{"path":%q,"old_text":"virtual","new_text":"physical"}`, filepath.Join(scratch, "virtual.txt")), isWritable: true},
		{name: "write", arguments: fmt.Sprintf(`{"path":%q,"content":"physical\n"}`, filepath.Join(scratch, "physical.txt")), isWritable: true},
		{name: "edit", arguments: `{"path":"/tmp/physical.txt","old_text":"physical","new_text":"virtual"}`, isWritable: true},
		{name: "write", arguments: `{"path":"workspace.txt","content":"blocked\n"}`},
		{name: "write", arguments: fmt.Sprintf(`{"path":%q,"content":"blocked\n"}`, filepath.Join(farm, "neighbour", "private.txt"))},
		{name: "write", arguments: fmt.Sprintf(`{"path":%q,"content":"blocked\n"}`, filepath.Join(scratch, "secret.key"))},
		{name: "write", arguments: `{"path":"/tmp/secret.key","content":"blocked\n"}`},
		{name: "write", arguments: fmt.Sprintf(`{"path":%q,"content":"blocked\n"}`, filepath.Join(scratch, "git-link", "config"))},
		{name: "write", arguments: `{"path":"/tmp/git-link/config","content":"blocked\n"}`},
	} {
		call, err := byName[step.name].Parse(step.arguments)
		if err != nil {
			t.Fatal(err)
		}
		result, err := call.Exec(t.Context())
		if step.isWritable != (err == nil) {
			t.Errorf("%s %s: %v; want writable %t", step.name, step.arguments, err, step.isWritable)
		}
		fmt.Fprintf(&output, "%s %s\n", step.name, step.arguments)
		if err != nil {
			fmt.Fprintf(&output, "error: %v\n\n", err)
		} else {
			fmt.Fprintf(&output, "%s\n\n", result.Output)
		}
	}
	for name, want := range map[string]string{"virtual.txt": "physical\n", "physical.txt": "virtual\n"} {
		content, err := state.options.roots.Scratch.ReadFile(name)
		if err != nil || string(content) != want {
			t.Errorf("scratch file %s contains %q, %v; want %q", name, content, err, want)
		}
	}
	if content, err := workspace.GetRoot().ReadFile(".git/config"); err != nil || string(content) != "intact" {
		t.Errorf("the metadata changed to %q: %v", content, err)
	}
	for _, path := range []string{
		filepath.Join(workspace.GetDir(), "workspace.txt"),
		filepath.Join(farm, "neighbour"),
		filepath.Join(scratch, "secret.key"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("a refused write created %s: %v", path, err)
		}
	}
	text := strings.NewReplacer(scratch, "<scratch>", farm, "<farm>", workspace.GetDir(), "<workspace>").Replace(output.String())
	compareWithGolden(t, "scratch-write-paths", ".txt", map[string]func() string{
		"scratch aliases share writes and edits without widening access": func() string { return text },
	})
}
