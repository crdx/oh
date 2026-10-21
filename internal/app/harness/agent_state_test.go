package harness

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/drops"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/subagents"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/tool/middleware/truncate"
)

const agentStateOutputLimit = 1024

func loudTool(name string) tool.Tool {
	return tool.Implement(
		tool.Definition{Name: name, Description: name, Schema: tool.Schema{}},
		func(struct{}) tool.CallRendering { return tool.CallRendering{} },
	).Plain(func(context.Context, struct{}) (string, error) {
		return strings.Repeat("a line of output\n", agentStateOutputLimit), nil
	})
}

func openTestRoots(t *testing.T, homeDirectory string, scratchDirectory string) shell.Roots {
	t.Helper()

	roots, err := shell.OpenRoots(homeDirectory, scratchDirectory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = roots.Close() })
	return roots
}

func newTestAgentState(t *testing.T) *agentState {
	t.Helper()

	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "big.txt"), []byte(strings.Repeat("a line of text\n", agentStateOutputLimit)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "many"), 0o700); err != nil {
		t.Fatal(err)
	}
	for index := range agentStateOutputLimit / 8 {
		if err := os.WriteFile(filepath.Join(directory, "many", fmt.Sprintf("a-rather-long-file-name-%04d.txt", index)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	workspace := work.At(directory)
	if err := workspace.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	home, scratch := t.TempDir(), t.TempDir()
	state, err := buildAgentState(agentOptions{
		workspace:        workspace,
		mode:             caps.NewMode(caps.Read),
		roots:            openTestRoots(t, home, scratch),
		home:             shell.ScratchRoot(home),
		scratch:          shell.ScratchRoot(scratch),
		sessionDirectory: t.TempDir(),
		ensurePersisted:  func() error { return nil },
		outputLimit:      truncate.NewLimit(agentStateOutputLimit),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(state.Close)
	return state
}

func TestEveryAgentToolIsCappedSavedToItsDropsAndKeepsItsRoot(t *testing.T) {
	state := newTestAgentState(t)
	tools, err := state.tools(loudTool("bash"), agentFeatures{
		beforeShell: []tool.Tool{loudTool("before")},
		afterShell:  []tool.Tool{loudTool("after")},
		custom:      []tool.Tool{loudTool("custom")},
	})
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, subject := range tools {
		names = append(names, subject.Name())
	}
	want := []string{"read", "ls", "find", "grep", "write", "edit", "before", "bash", "after", "custom"}
	if !slices.Equal(names, want) {
		t.Errorf("got tools %v, want %v", names, want)
	}

	arguments := map[string]string{
		"read":   `{"path":"big.txt"}`,
		"ls":     `{"path":"many"}`,
		"find":   `{"pattern":"*","path":"many"}`,
		"before": `{}`,
		"bash":   `{}`,
		"after":  `{}`,
		"custom": `{}`,
	}
	for _, subject := range tools {
		argument, isLoud := arguments[subject.Name()]
		if !isLoud {
			continue
		}
		call, err := subject.Parse(argument)
		if err != nil {
			t.Fatal(err)
		}
		result, err := call.Exec(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Output) > 2*agentStateOutputLimit || !strings.Contains(result.Output, "truncated at") {
			t.Errorf("%s came back uncapped: %d bytes", subject.Name(), len(result.Output))
		}
		if subject.Name() != "read" && !strings.Contains(result.Output, "full output in "+drops.GetDirectory(state.options.sessionDirectory)) {
			t.Errorf("%s saved its overflow outside its session's drops: %q", subject.Name(), result.Output[len(result.Output)-160:])
		}
	}
}

func TestACustomToolCannotTakeAnAgentToolsName(t *testing.T) {
	state := newTestAgentState(t)
	_, err := state.tools(loudTool("bash"), agentFeatures{custom: []tool.Tool{loudTool("read")}})
	if err == nil || !strings.Contains(err.Error(), "read is already a tool") {
		t.Errorf("got %v, want the clash refused", err)
	}
}

func TestAChildWritesItsPrivateHomeAndScratchButNotItsWorkspace(t *testing.T) {
	scratchDirectory := t.TempDir()
	scratchRoot, err := os.OpenRoot(scratchDirectory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = scratchRoot.Close() })
	workspace := work.At(t.TempDir())
	if err := workspace.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	child := subagents.Child{
		Name:            "tame-adder",
		Caps:            caps.Read | caps.Shell,
		Directory:       t.TempDir(),
		Scratch:         scratchDirectory,
		ScratchRoot:     scratchRoot,
		EnsurePersisted: func() error { return nil },
	}
	state, err := buildChildState(childOptions{toolOutputLimit: truncate.NewLimit(agentStateOutputLimit)}, child, workspace, nil)
	if err != nil {
		t.Fatal(err)
	}

	home := filepath.Join(scratchDirectory, subagents.HomeName)
	for path, isWritable := range map[string]bool{
		filepath.Join(home, "notes.txt"):           true,
		filepath.Join(home, ".cache", "notes.txt"): true,
		filepath.Join(sandbox.TmpDir, "notes.txt"): true,
		filepath.Join(workspace.GetDir(), "a.txt"): false,
	} {
		root, name, err := state.files.Resolve(path)
		if err == nil {
			err = root.WriteFile(name, []byte("written"), 0o600)
		}
		if isWritable != (err == nil) {
			t.Errorf("writing %s gave %v, want writable %t", path, err, isWritable)
		}
	}

	state.Close()
	if _, err := scratchRoot.Stat("."); err != nil {
		t.Errorf("closing the child closed the scratch handle its manager owns: %v", err)
	}
}
