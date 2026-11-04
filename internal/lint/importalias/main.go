package main

import (
	"go/ast"

	"crdx.org/oh/internal/lint/runner"
)

const (
	blankName = "_"
	message   = "imports must not be renamed"
)

func main() {
	runner.Main("importalias", analyse)
}

func analyse(file runner.File) []runner.Diagnostic {
	var diagnostics []runner.Diagnostic
	for _, specification := range file.Syntax.Imports {
		if isRenamed(specification) {
			diagnostics = append(diagnostics, file.Report(specification.Name, message))
		}
	}
	return diagnostics
}

func isRenamed(specification *ast.ImportSpec) bool {
	return specification.Name != nil && specification.Name.Name != blankName
}
