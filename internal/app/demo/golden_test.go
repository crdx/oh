package demo

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/util"
	"crdx.org/oh/pkg/toolbox/ls"
)

var updateGoldens = flag.Bool("update", false, "update golden files")

func TestGoldenRepliesMatchGolden(t *testing.T) {
	var got strings.Builder
	fmt.Fprintf(&got, "abilities\n\n%s\n\nmissing path\n\n%s\n", abilities, readReply("read").Say)
	for _, toolReport := range []struct {
		name     string
		toolName string
		output   string
	}{
		{"one line read", readTool, "main.go"},
		{"one entry listed", listTool, "main.go"},
		{"several entries listed", listTool, "go.mod\nmain.go"},
		{"nothing listed", listTool, ls.EmptyListing},
		{"one match", grepTool, "main.go:1:package main"},
		{"several matches", grepTool, "main.go:1:package main\nmain_test.go:1:package main"},
		{"no matches", grepTool, util.NoMatches},
	} {
		turn := report(request{toolName: toolReport.toolName, toolOutput: toolReport.output, hasOutput: true})
		fmt.Fprintf(&got, "\n%s\n\n%s\n", toolReport.name, turn.Say)
	}
	goldenPath := filepath.Join("testdata", "replies.txt")

	if *updateGoldens {
		if err := os.WriteFile(goldenPath, []byte(got.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, got.String(), want)
	}
}
