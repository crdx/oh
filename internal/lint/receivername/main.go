package main

import (
	"go/ast"

	"crdx.org/oh/internal/lint/runner"
)

const expectedReceiverName = "self"

func main() {
	runner.Main("receivername", analyse)
}

func analyse(file runner.File) []runner.Diagnostic {
	var diagnostics []runner.Diagnostic
	for _, declaration := range file.Syntax.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || function.Recv == nil {
			continue
		}
		for _, field := range function.Recv.List {
			for _, receiver := range field.Names {
				if receiver.Name != expectedReceiverName {
					diagnostics = append(diagnostics, file.Report(receiver, "method receiver must be named self"))
				}
			}
		}
	}
	return diagnostics
}
