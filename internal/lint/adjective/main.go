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

var irregularParticiples = []string{
	"born",
	"broken",
	"built",
	"chosen",
	"dealt",
	"drawn",
	"driven",
	"fallen",
	"felt",
	"forgotten",
	"frozen",
	"given",
	"grown",
	"held",
	"hidden",
	"kept",
	"known",
	"lost",
	"meant",
	"paid",
	"said",
	"sent",
	"shown",
	"sold",
	"spent",
	"spoken",
	"stolen",
	"swept",
	"taken",
	"thrown",
	"told",
	"torn",
	"woken",
	"worn",
	"written",
}

var wordsEndingInEd = []string{
	"bed",
	"bled",
	"bred",
	"breed",
	"creed",
	"deed",
	"embed",
	"exceed",
	"fed",
	"feed",
	"freed",
	"greed",
	"indeed",
	"led",
	"need",
	"proceed",
	"red",
	"seed",
	"shed",
	"sled",
	"sped",
	"speed",
	"succeed",
	"tweed",
	"weed",
	"wed",
}

var nounsEndingInIng = []string{
	"building",
	"ceiling",
	"clothing",
	"crossing",
	"drawing",
	"evening",
	"greeting",
	"grouping",
	"heading",
	"hearing",
	"landing",
	"listing",
	"meaning",
	"meeting",
	"morning",
	"offering",
	"padding",
	"painting",
	"reasoning",
	"rendering",
	"setting",
	"sibling",
	"spring",
	"streaming",
	"string",
	"thing",
	"thinking",
	"timing",
	"training",
	"warning",
	"wedding",
	"wording",
}

const (
	pastTense    = "was"
	presentTense = "is"
)

func main() {
	runner.Main("adjective", analyse)
}

func analyse(file runner.File) []runner.Diagnostic {
	var diagnostics []runner.Diagnostic
	for _, name := range declaredNames(file.Syntax) {
		parts := words(name.Name)
		if len(parts) != 1 {
			continue
		}
		tense, isParticiple := participleTense(parts[0])
		if !isParticiple {
			continue
		}
		diagnostics = append(diagnostics, file.Report(name, fmt.Sprintf(
			"%s: say what %s %s, since a name is a noun rather than an adjective", name.Name, tense, parts[0],
		)))
	}
	return diagnostics
}

func participleTense(word string) (string, bool) {
	if slices.Contains(irregularParticiples, word) {
		return pastTense, true
	}
	if strings.HasSuffix(word, "ed") && len(word) >= 4 {
		return pastTense, !slices.Contains(wordsEndingInEd, word)
	}
	if strings.HasSuffix(word, "ing") && len(word) >= 5 {
		return presentTense, !slices.Contains(nounsEndingInIng, word)
	}
	return "", false
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

	ast.Inspect(file, func(node ast.Node) bool {
		switch typedNode := node.(type) {
		case *ast.GenDecl:
			return typedNode.Tok != token.CONST
		case *ast.InterfaceType:
			return false
		case *ast.TypeSpec:
			add(typedNode.Name)
		case *ast.ValueSpec:
			if !isBooleanType(typedNode.Type) {
				add(typedNode.Names...)
			}
		case *ast.Field:
			if !isBooleanType(typedNode.Type) {
				add(typedNode.Names...)
			}
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

func isBooleanType(expression ast.Expr) bool {
	identifier, isIdentifier := expression.(*ast.Ident)
	return isIdentifier && identifier.Name == "bool"
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
