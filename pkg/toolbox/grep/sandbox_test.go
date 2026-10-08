package grep

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/sandbox/testexec"
	"crdx.org/oh/pkg/sandbox"
)

type observedRunner struct {
	inner  sandbox.ArgvRunner
	policy sandbox.Policy
}

func (self *observedRunner) RunArgv(
	ctx context.Context,
	directory string,
	arguments []string,
	policy sandbox.Policy,
) (sandbox.Result, error) {
	started, err := self.StartArgv(ctx, directory, arguments, policy, &bytes.Buffer{})
	if err != nil {
		return sandbox.Result{}, err
	}
	return started.Wait()
}

func (self *observedRunner) StartArgv(
	ctx context.Context,
	directory string,
	arguments []string,
	policy sandbox.Policy,
	output sandbox.Output,
) (sandbox.Command, error) {
	self.policy = policy
	return self.inner.StartArgv(ctx, directory, arguments, policy, output)
}

func TestGrepUsesAReadOnlyWorkspacePolicy(t *testing.T) {
	root := testRoot(t, map[string]string{"main.go": "hello\n"})
	runner := &observedRunner{inner: testexec.New()}
	call, err := NewWithRunner(root, file.NewSnapshots(), runner).Parse(`{"pattern":"hello"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := call.Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	policy := runner.policy
	if !slices.Equal(policy.Read, []string{root.Name()}) || len(policy.Write) != 0 || policy.Network || policy.Yolo || policy.TmpDir != "" || policy.Timeout <= 0 {
		t.Errorf("grep used an unsafe policy: %+v", policy)
	}
}
