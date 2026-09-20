package toolbox

import (
	"crdx.org/io/internal/file"
	"crdx.org/io/pkg/tool"
	"crdx.org/io/pkg/toolbox/edit"
	"crdx.org/io/pkg/toolbox/find"
	"crdx.org/io/pkg/toolbox/grep"
	"crdx.org/io/pkg/toolbox/ls"
	"crdx.org/io/pkg/toolbox/read"
	"crdx.org/io/pkg/toolbox/write"
)

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
