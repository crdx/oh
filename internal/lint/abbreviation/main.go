package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strings"
	"unicode"

	"crdx.org/oh/internal/lint/runner"
)

var abbreviations = map[string]string{
	"cfg":   "configuration",
	"conn":  "connection",
	"curr":  "current",
	"decl":  "declaration",
	"dst":   "destination",
	"elem":  "element",
	"ident": "identifier",
	"idx":   "index",
	"med":   "medium",
	"pos":   "position",
	"prev":  "previous",
	"resp":  "response",
	"stmt":  "statement",
	"txt":   "text",
	"val":   "value",
	"vol":   "volume",
}

func main() {
	runner.Main("abbreviation", analyse)
}

func spelling(word string) (string, bool) {
	if full, isAbbreviation := abbreviations[word]; isAbbreviation {
		return full, true
	}
	if singular, isPlural := strings.CutSuffix(word, "s"); isPlural {
		if full, isAbbreviation := abbreviations[singular]; isAbbreviation {
			return full + "s", true
		}
	}
	return "", false
}

func analyse(file runner.File) []runner.Diagnostic {
	var diagnostics []runner.Diagnostic
	for _, name := range declaredNames(file.Syntax) {
		for _, word := range words(name.Name) {
			if full, isAbbreviation := spelling(word); isAbbreviation {
				diagnostics = append(diagnostics, file.Report(name, fmt.Sprintf(
					"%s: write %q in full as %q", name.Name, word, full,
				)))
			}
		}
	}
	slices.SortStableFunc(diagnostics, func(left runner.Diagnostic, right runner.Diagnostic) int {
		return left.Position.Offset - right.Position.Offset
	})
	return diagnostics
}

func declaredNames(file *ast.File) []*ast.Ident {
	var names []*ast.Ident
	add := func(candidates ...*ast.Ident) {
		for _, name := range candidates {
			if name != nil && name.Name != "_" {
				names = append(names, name)
			}
		}
	}

	for _, group := range file.Imports {
		add(group.Name)
	}

	ast.Inspect(file, func(node ast.Node) bool {
		switch typedNode := node.(type) {
		case *ast.FuncDecl:
			add(typedNode.Name)
		case *ast.TypeSpec:
			add(typedNode.Name)
		case *ast.ValueSpec:
			add(typedNode.Names...)
		case *ast.Field:
			add(typedNode.Names...)
		case *ast.LabeledStmt:
			add(typedNode.Label)
		case *ast.AssignStmt:
			if typedNode.Tok != token.DEFINE {
				return true
			}
			for _, target := range typedNode.Lhs {
				if identifier, isIdentifier := target.(*ast.Ident); isIdentifier {
					add(identifier)
				}
			}
		case *ast.RangeStmt:
			for _, target := range []ast.Expr{typedNode.Key, typedNode.Value} {
				if identifier, isIdentifier := target.(*ast.Ident); isIdentifier {
					add(identifier)
				}
			}
		}
		return true
	})
	return names
}

func words(name string) []string {
	var found []string
	var current []rune

	for _, letter := range name {
		if unicode.IsUpper(letter) && len(current) > 0 {
			found = append(found, strings.ToLower(string(current)))
			current = nil
		}
		current = append(current, letter)
	}
	if len(current) > 0 {
		found = append(found, strings.ToLower(string(current)))
	}
	return found
}
