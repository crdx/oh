package toolbox

import (
	"crdx.org/oh/internal/file"
	"crdx.org/oh/pkg/sandbox"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/edit"
	"crdx.org/oh/pkg/toolbox/find"
	"crdx.org/oh/pkg/toolbox/grep"
	"crdx.org/oh/pkg/toolbox/ls"
	"crdx.org/oh/pkg/toolbox/read"
	"crdx.org/oh/pkg/toolbox/write"
)

func Rummage(root *file.Root, snapshots *file.Snapshots) []tool.Tool {
	return RummageWithRunner(root, snapshots, sandbox.DirectArgv())
}

func RummageWithRunner(root *file.Root, snapshots *file.Snapshots, runner sandbox.ArgvRunner) []tool.Tool {
	return []tool.Tool{
		read.New(root, snapshots),
		ls.New(root),
		find.New(root),
		grep.NewWithRunner(root, snapshots, runner),
		write.New(root, snapshots),
		edit.New(root, snapshots),
	}
}
