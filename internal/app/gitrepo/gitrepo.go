package gitrepo

import (
	"os"
	"path/filepath"
	"strings"
)

func Dir(workspaceDir string) string {
	gitPath := filepath.Join(workspaceDir, ".git")

	info, err := os.Stat(gitPath)
	if err != nil {
		return ""
	}

	if info.IsDir() {
		return gitPath
	}

	pointer, err := os.ReadFile(gitPath) //nolint:gosec // the .git of the workspace
	if err != nil {
		return ""
	}

	elsewhere, ok := strings.CutPrefix(strings.TrimSpace(string(pointer)), "gitdir: ")
	if !ok {
		return ""
	}

	if filepath.IsAbs(elsewhere) {
		return elsewhere
	}

	return filepath.Join(workspaceDir, elsewhere)
}
