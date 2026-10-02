package runner

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"slices"
)

type Diagnostic struct {
	Position token.Position
	Message  string
}

type File struct {
	Name    string
	FileSet *token.FileSet
	Syntax  *ast.File
}

func (self File) Report(node ast.Node, message string) Diagnostic {
	return Diagnostic{Position: self.FileSet.Position(node.Pos()), Message: message}
}

type Analyser func(file File) []Diagnostic

func Main(name string, analyse Analyser) {
	if !Run(os.Stdout, os.Stderr, name, analyse, os.Args[1:]) {
		os.Exit(1)
	}
}

func Run(output io.Writer, errorOutput io.Writer, name string, analyse Analyser, filenames []string) bool {
	slices.Sort(filenames)

	isClean := true
	for _, filename := range filenames {
		diagnostics, err := Check(analyse, filename)
		if err != nil {
			_, _ = fmt.Fprintln(errorOutput, err)
			isClean = false
			continue
		}
		for _, diagnostic := range diagnostics {
			_, _ = fmt.Fprintf(output, "%s: %s (%s)\n", diagnostic.Position, diagnostic.Message, name)
			isClean = false
		}
	}
	return isClean
}

func Check(analyse Analyser, filename string) ([]Diagnostic, error) {
	source, err := os.ReadFile(filepath.Clean(filename)) //nolint:gosec // a linter reads the files it is named
	if err != nil {
		return nil, err
	}
	return CheckSource(analyse, filepath.Clean(filename), source)
}

func CheckSource(analyse Analyser, filename string, source []byte) ([]Diagnostic, error) {
	fileSet := token.NewFileSet()
	syntax, err := parser.ParseFile(fileSet, filename, source, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	return analyse(File{Name: filename, FileSet: fileSet, Syntax: syntax}), nil
}
