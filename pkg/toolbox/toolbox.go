package toolbox

import (
	"crdx.org/oh/internal/file"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/edit"
	"crdx.org/oh/pkg/toolbox/find"
	"crdx.org/oh/pkg/toolbox/grep"
	"crdx.org/oh/pkg/toolbox/ls"
	"crdx.org/oh/pkg/toolbox/read"
	"crdx.org/oh/pkg/toolbox/write"
)

var PathToolNames = []string{"read", "ls", "find", "grep", "write", "edit"}

func Rummage(root *file.Root, snapshots *file.Snapshots) []tool.Tool {
	return []tool.Tool{
		read.New(root, snapshots),
		ls.New(root),
		find.New(root),
		grep.New(root, snapshots),
		write.New(root, snapshots),
		edit.New(root, snapshots),
	}
}
