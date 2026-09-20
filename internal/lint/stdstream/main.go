package main

import (
	"go/ast"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"crdx.org/oh/internal/lint/runner"
)

const message = "a library package minds its own business"

var (
	applicationRoots = []string{"cmd", "internal"}
	standardStreams  = []string{"Stdin", "Stdout", "Stderr"}
)

func main() {
	runner.Main("stdstream", analyse)
}

func isApplication(filename string, packageName string) bool {
	if packageName == "main" {
		return true
	}

	root, _, _ := strings.Cut(filepath.ToSlash(filename), "/")
	return slices.Contains(applicationRoots, root)
}

func analyse(file runner.File) []runner.Diagnostic {
	if isApplication(file.Name, file.Syntax.Name.Name) {
		return nil
	}

	packageName, isImported := importedName(file.Syntax)
	if !isImported {
		return nil
	}

	var diagnostics []runner.Diagnostic
	ast.Inspect(file.Syntax, func(node ast.Node) bool {
		selector, isSelector := node.(*ast.SelectorExpr)
		if !isSelector || !slices.Contains(standardStreams, selector.Sel.Name) {
			return true
		}
		if identifier, isIdentifier := selector.X.(*ast.Ident); isIdentifier && identifier.Name == packageName {
			diagnostics = append(diagnostics, file.Report(selector, message))
		}

		return true
	})

	return diagnostics
}

func importedName(file *ast.File) (string, bool) {
	for _, specification := range file.Imports {
		path, err := strconv.Unquote(specification.Path.Value)
		if err != nil || path != "os" {
			continue
		}
		if specification.Name != nil {
			return specification.Name.Name, specification.Name.Name != "_"
		}

		return "os", true
	}

	return "", false
}
