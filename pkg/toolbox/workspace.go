package toolbox

import (
	"context"
	"os"

	"crdx.org/oh/internal/file"
	"crdx.org/oh/pkg/sandbox"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/bash"
)

type Workspace struct {
	files     *file.Root
	snapshots *file.Snapshots
}

func NewWorkspace(root *os.Root, refuseWrite func(string) error) *Workspace {
	if root == nil {
		panic("a workspace needs an open root")
	}
	if refuseWrite == nil {
		panic("a workspace needs a write guard")
	}
	return &Workspace{files: file.New(root, refuseWrite), snapshots: file.NewSnapshots()}
}

func (self *Workspace) Rummage() []tool.Tool {
	return Rummage(self.files, self.snapshots)
}

func (self *Workspace) Bash(
	buildPolicy func(context.Context) (sandbox.Policy, error),
	approveNetwork func(context.Context, string, string) error,
	runner sandbox.Runner,
	hasNetworkChoice bool,
) tool.Tool {
	if buildPolicy == nil || runner == nil || hasNetworkChoice && approveNetwork == nil {
		panic("bash needs a policy, runner and network approval when host networking is offered")
	}
	return bash.New(self.files, buildPolicy, approveNetwork, runner, hasNetworkChoice)
}
