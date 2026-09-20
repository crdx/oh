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

var booleanOperators = []token.Token{
	token.EQL,
	token.NEQ,
	token.LSS,
	token.GTR,
	token.LEQ,
	token.GEQ,
	token.LAND,
	token.LOR,
}

var predicatePrefixes = []string{
	"allows",
	"are",
	"can",
	"contains",
	"could",
	"did",
	"does",
	"exists",
	"has",
	"includes",
	"is",
	"knows",
	"matches",
	"may",
	"might",
	"must",
	"needs",
	"ok",
	"requires",
	"shall",
	"should",
	"supports",
	"uses",
	"was",
	"wants",
	"were",
	"will",
	"would",
}

func main() {
	runner.Main("boolname", analyse)
}

func analyse(file runner.File) []runner.Diagnostic {
	var diagnostics []runner.Diagnostic

	report := func(name *ast.Ident) {
		if name.Name == "_" || isPredicate(name.Name) {
			return
		}
		diagnostics = append(diagnostics, file.Report(name, fmt.Sprintf(
			"%s: name a boolean as a predicate, such as %q", name.Name, predicateForm(name.Name),
		)))
	}

	ast.Inspect(file.Syntax, func(node ast.Node) bool {
		switch typedNode := node.(type) {
		case *ast.FuncDecl:
			for _, field := range parameters(typedNode.Type) {
				if isBooleanType(field.Type) {
					for _, name := range field.Names {
						if !isCopularBooleanParameter(typedNode, field) {
							report(name)
						}
					}
				}
			}
		case *ast.ValueSpec:
			if isBooleanType(typedNode.Type) {
				for _, name := range typedNode.Names {
					report(name)
				}
				return true
			}
			for index, value := range typedNode.Values {
				if index < len(typedNode.Names) && isBooleanExpression(value) {
					report(typedNode.Names[index])
				}
			}
		case *ast.AssignStmt:
			if typedNode.Tok == token.DEFINE {
				reportDefinitions(typedNode, report)
			}
		}
		return true
	})
	return diagnostics
}

func reportDefinitions(statement *ast.AssignStmt, report func(name *ast.Ident)) {
	names := make([]*ast.Ident, 0, len(statement.Lhs))
	for _, target := range statement.Lhs {
		identifier, isIdentifier := target.(*ast.Ident)
		if !isIdentifier {
			return
		}
		names = append(names, identifier)
	}

	if len(names) == 2 && len(statement.Rhs) == 1 && isCommaOK(statement.Rhs[0]) {
		report(names[1])
		return
	}
	for index, value := range statement.Rhs {
		if index < len(names) && isBooleanExpression(value) {
			report(names[index])
		}
	}
}

func parameters(signature *ast.FuncType) []*ast.Field {
	var fields []*ast.Field
	if signature.Params != nil {
		fields = append(fields, signature.Params.List...)
	}
	if signature.Results != nil {
		fields = append(fields, signature.Results.List...)
	}
	return fields
}

func isCopularBooleanParameter(function *ast.FuncDecl, field *ast.Field) bool {
	parameters := function.Type.Params
	return strings.HasSuffix(function.Name.Name, "Is") &&
		parameters != nil && len(parameters.List) == 1 && parameters.List[0] == field &&
		len(field.Names) == 1
}

func isBooleanType(expression ast.Expr) bool {
	identifier, isIdentifier := expression.(*ast.Ident)
	return isIdentifier && identifier.Name == "bool"
}

func isCommaOK(expression ast.Expr) bool {
	switch typedExpression := expression.(type) {
	case *ast.IndexExpr, *ast.TypeAssertExpr:
		return true
	case *ast.UnaryExpr:
		return typedExpression.Op == token.ARROW
	}
	return false
}

func isBooleanExpression(expression ast.Expr) bool {
	switch typedExpression := expression.(type) {
	case *ast.Ident:
		return typedExpression.Name == "true" || typedExpression.Name == "false"
	case *ast.ParenExpr:
		return isBooleanExpression(typedExpression.X)
	case *ast.UnaryExpr:
		return typedExpression.Op == token.NOT
	case *ast.BinaryExpr:
		return slices.Contains(booleanOperators, typedExpression.Op)
	}
	return false
}

func isPredicate(name string) bool {
	loweredName := strings.ToLower(name[:1]) + name[1:]
	for _, prefix := range predicatePrefixes {
		if !strings.HasPrefix(loweredName, prefix) {
			continue
		}
		rest := loweredName[len(prefix):]
		if rest == "" || unicode.IsUpper(rune(rest[0])) {
			return true
		}
	}
	return false
}

func predicateForm(name string) string {
	return "is" + strings.ToUpper(name[:1]) + name[1:]
}
