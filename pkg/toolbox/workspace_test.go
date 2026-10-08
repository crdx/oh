package toolbox_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"crdx.org/oh/pkg/sandbox"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox"
)

func testWorkspace(t *testing.T, refuseWrite func(string) error) (*toolbox.Workspace, string) {
	t.Helper()

	directory := t.TempDir()
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })

	return toolbox.NewWorkspace(root, refuseWrite), directory
}

func workspaceTool(t *testing.T, tools []tool.Tool, name string) tool.Tool {
	t.Helper()

	for _, candidate := range tools {
		if candidate.Name() == name {
			return candidate
		}
	}

	t.Fatalf("no %s tool", name)
	return nil
}

func callWorkspaceTool(t *testing.T, selected tool.Tool, arguments string) error {
	t.Helper()

	call, err := selected.Parse(arguments)
	if err != nil {
		return err
	}
	_, err = call.Exec(t.Context())
	return err
}

func TestPublicWorkspaceSharesReadStateBetweenFileTools(t *testing.T) {
	workspace, directory := testWorkspace(t, func(string) error { return nil })
	if err := os.WriteFile(filepath.Join(directory, "note.txt"), []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tools := workspace.Rummage()
	var names []string
	for _, selected := range tools {
		names = append(names, selected.Name())
	}
	for _, name := range []string{"read", "ls", "find", "grep", "write", "edit"} {
		if !slices.Contains(names, name) {
			t.Errorf("missing %s from %v", name, names)
		}
	}

	edit := workspaceTool(t, tools, "edit")
	arguments := `{"path":"note.txt","old_text":"before","new_text":"after"}`
	if err := callWorkspaceTool(t, edit, arguments); err == nil {
		t.Fatal("edit succeeded before the file was read")
	}
	read := workspaceTool(t, tools, "read")
	readCall, err := read.Parse(`{"path":"note.txt"}`)
	if err != nil {
		t.Fatal(err)
	}
	readResult, err := readCall.Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Restore(readResult.State); err != nil {
		t.Fatal(err)
	}
	if err := callWorkspaceTool(t, edit, arguments); err != nil {
		t.Fatal(err)
	}

	contents, err := os.ReadFile(filepath.Join(directory, "note.txt")) //nolint:gosec // the test's own temporary directory
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "after\n" {
		t.Errorf("got %q, want after", contents)
	}
}

func TestPublicWorkspaceAppliesWriteGuard(t *testing.T) {
	denied := errors.New("writes refused")
	workspace, _ := testWorkspace(t, func(string) error { return denied })

	err := callWorkspaceTool(t, workspaceTool(t, workspace.Rummage(), "write"), `{"path":"note.txt","content":"hello"}`)
	if !errors.Is(err, denied) {
		t.Errorf("got %v, want write refusal", err)
	}
}

type recordingRunner struct {
	policy    sandbox.Policy
	directory string
	calls     int
}

func (self *recordingRunner) Run(
	_ context.Context,
	directory string,
	_ string,
	policy sandbox.Policy,
) (sandbox.Result, error) {
	self.policy = policy
	self.directory = directory
	self.calls++
	return sandbox.Result{Output: "ran\n"}, nil
}

func (self *recordingRunner) Start(
	context.Context,
	string,
	string,
	sandbox.Policy,
	sandbox.Output,
) (sandbox.Command, error) {
	return nil, errors.New("unexpected Start call")
}

func TestPublicWorkspaceSharesItsRootWithBash(t *testing.T) {
	workspace, directory := testWorkspace(t, func(string) error { return nil })
	runner := &recordingRunner{}
	buildPolicy := func(context.Context) (sandbox.Policy, error) {
		return sandbox.Policy{Write: []string{directory}, TmpDir: directory}, nil
	}
	approval := func(context.Context, string, string) error { return nil }
	bash := workspace.Bash(buildPolicy, approval, runner, false)

	if err := callWorkspaceTool(t, bash, `{"command":"pwd","intent":"Checking the workspace directory"}`); err != nil {
		t.Fatal(err)
	}
	if runner.calls != 1 || runner.directory != directory || !slices.Contains(runner.policy.Write, directory) || runner.policy.Network {
		t.Errorf("runner got %d calls in %s with policy %+v", runner.calls, runner.directory, runner.policy)
	}
}

func TestPublicWorkspaceRequiresApprovalForHostNetworking(t *testing.T) {
	workspace, _ := testWorkspace(t, func(string) error { return nil })
	runner := &recordingRunner{}
	denied := errors.New("host network refused")
	bash := workspace.Bash(
		func(context.Context) (sandbox.Policy, error) { return sandbox.Policy{}, nil },
		func(context.Context, string, string) error { return denied },
		runner,
		true,
	)

	err := callWorkspaceTool(t, bash, `{"command":"true","intent":"Checking host network approval","network":"host"}`)
	if !errors.Is(err, denied) || runner.calls != 0 {
		t.Errorf("got %v and %d runs, want refusal before run", err, runner.calls)
	}
}

func TestPublicWorkspaceRejectsMissingWriteGuardAtConstruction(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	defer func() {
		if recover() == nil {
			t.Error("a nil guard was accepted")
		}
	}()
	toolbox.NewWorkspace(root, nil)
}

func TestPublicWorkspaceRejectsMissingRootAtConstruction(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a nil root was accepted")
		}
	}()
	toolbox.NewWorkspace(nil, func(string) error { return nil })
}

func TestPublicWorkspaceRejectsHostNetworkingWithoutApproval(t *testing.T) {
	workspace, _ := testWorkspace(t, func(string) error { return nil })
	defer func() {
		if recover() == nil {
			t.Error("host networking without approval was accepted")
		}
	}()
	workspace.Bash(
		func(context.Context) (sandbox.Policy, error) { return sandbox.Policy{}, nil },
		nil,
		&recordingRunner{},
		true,
	)
}
