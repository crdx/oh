package markdown

import (
	"crdx.org/col"

	"github.com/yuin/goldmark/extension/ast"

	"crdx.org/oh/internal/app/style"
)

func isTable(node any) bool {
	_, is := node.(*ast.Table)
	return is
}

func (self *renderer) extensionBlock(node any) bool {
	table, is := node.(*ast.Table)
	if !is {
		return false
	}

	self.add(self.table(table)...)
	return true
}

func (self *renderer) extensionInline(node any) (string, bool) {
	switch node := node.(type) {
	case *ast.Strikethrough:
		return over(col.Strikethrough, self.inline(node)), true

	case *ast.TaskCheckBox:
		if node.IsChecked {
			return style.Success("[x] "), true
		}

		return style.Subtle("[ ] "), true
	}

	return "", false
}
