package main

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"crdx.org/oh/internal/lint/runner"
)

const message = "paint through the style palette, so every terminal background gets a colour made for it"

var paletteOwners = []string{
	"internal/app/style/",
	"internal/app/graphics/",
}

var colourLiteral = regexp.MustCompile(
	`#[0-9a-fA-F]{6}\b|\x1b\[(?:[0-9;]*;)?(?:[34]8;[25];|(?:3|4|9|10)[0-7](?:;|m))`,
)

func main() {
	runner.Main("palette", analyse)
}

func isPaletteOwner(filename string) bool {
	path := filepath.ToSlash(filename)
	for _, owner := range paletteOwners {
		if strings.HasPrefix(path, owner) || strings.Contains(path, "/"+owner) {
			return true
		}
	}

	return false
}

func analyse(file runner.File) []runner.Diagnostic {
	if isPaletteOwner(file.Name) {
		return nil
	}

	var diagnostics []runner.Diagnostic
	ast.Inspect(file.Syntax, func(node ast.Node) bool {
		literal, isLiteral := node.(*ast.BasicLit)
		if !isLiteral || literal.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(literal.Value)
		if err != nil {
			return true
		}
		if colourLiteral.MatchString(value) {
			diagnostics = append(diagnostics, file.Report(literal, message))
		}

		return true
	})

	return diagnostics
}
