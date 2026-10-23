package harness

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"crdx.org/oh/internal/file"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/edit"
	"crdx.org/oh/pkg/toolbox/find"
	"crdx.org/oh/pkg/toolbox/grep"
)

type sessionGoldenSearchedFiles struct {
	root      *file.Root
	snapshots *file.Snapshots
}

func newSessionGoldenSearchedFiles(t *testing.T) *sessionGoldenSearchedFiles {
	t.Helper()

	searched := t.TempDir()
	for name, content := range map[string]string{
		"main.go":    "package main\n",
		"server.log": "ERROR 10:30:00 disk full\n",
	} {
		if err := os.WriteFile(filepath.Join(searched, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rootHandle, err := os.OpenRoot(searched)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rootHandle.Close() })

	return &sessionGoldenSearchedFiles{
		root:      file.New(rootHandle, func(string) error { return nil }),
		snapshots: file.NewSnapshots(),
	}
}

const (
	editToolName = "edit"
	findToolName = "find"
)

func isSearchedFilesTool(name string) bool {
	return slices.Contains([]string{grepToolName, editToolName, findToolName}, name)
}

func (self *sessionGoldenSearchedFiles) tool(t *testing.T, specification sessionGoldenTool) tool.Tool {
	t.Helper()

	switch specification.Name {
	case editToolName:
		return edit.New(self.root, self.snapshots)
	case findToolName:
		return find.New(self.root)
	default:
		return grep.NewWithRunner(self.root, self.snapshots, sessionGoldenSearchRunner(t, specification.Runner))
	}
}
