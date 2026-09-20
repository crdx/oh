package shell

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func goModuleCache() (string, error) {
	path := os.Getenv("GOMODCACHE")
	if path == "" {
		path = os.Getenv("GOPATH")
		path, _, _ = strings.Cut(path, string(os.PathListSeparator))
		if path == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			path = filepath.Join(home, "go")
		}
		path = filepath.Join(path, "pkg", "mod")
	}

	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("go module cache is not absolute: %s", path)
	}

	info, err := os.Stat(path) //nolint:gosec // inspecting the configured Go cache is intended
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", nil
	case err != nil:
		return "", err
	case !info.IsDir():
		return "", fmt.Errorf("go module cache is not a directory: %s", path)
	}

	return filepath.Clean(path), nil
}
