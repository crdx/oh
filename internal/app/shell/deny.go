package shell

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"crdx.org/oh/internal/util"
)

var ErrDenied = errors.New("the path is denied by sandbox.deny")

type pathDenials struct {
	patterns []string
}

func newPathDenials(patterns []string) (pathDenials, error) {
	for _, pattern := range patterns {
		if err := util.ValidateNameGlob(pattern); err != nil {
			return pathDenials{}, fmt.Errorf("invalid sandbox.deny pattern %q: %w", pattern, err)
		}
	}
	return pathDenials{patterns: slices.Clone(patterns)}, nil
}

func (self pathDenials) Refuse(path string) error {
	if len(self.patterns) == 0 {
		return nil
	}
	path = filepath.Clean(path)
	if util.MatchNameGlobs(self.patterns, path) {
		return fmt.Errorf("%s: %w", path, ErrDenied)
	}

	resolvedPath, err := filepath.EvalSymlinks(path)
	if err == nil && resolvedPath != path && util.MatchNameGlobs(self.patterns, resolvedPath) {
		return fmt.Errorf("%s resolves to %s: %w", path, resolvedPath, ErrDenied)
	}
	return nil
}
