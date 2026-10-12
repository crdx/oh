package shell

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"crdx.org/oh/internal/util"
	"crdx.org/oh/internal/util/pathutil"
)

var ErrDenied = errors.New("the path is denied by sandbox.deny")

type pathDenials struct {
	patterns      []string
	namePatterns  []string
	absolutePaths []string
}

func newPathDenials(entries []string) (pathDenials, error) {
	denials := pathDenials{patterns: slices.Clone(entries)}
	for _, entry := range entries {
		if err := util.ValidateDenyEntry(entry); err != nil {
			return pathDenials{}, fmt.Errorf("invalid sandbox.deny entry %q: %w", entry, err)
		}
		if filepath.IsAbs(entry) {
			if _, err := os.Stat(entry); err != nil {
				return pathDenials{}, fmt.Errorf("cannot protect denied path %s: %w", entry, err)
			}
			denials.absolutePaths = append(denials.absolutePaths, entry)
		} else {
			denials.namePatterns = append(denials.namePatterns, entry)
		}
	}
	return denials, nil
}

func (self pathDenials) Refuse(path string) error {
	if len(self.patterns) == 0 {
		return nil
	}
	path = filepath.Clean(path)
	if util.MatchNameGlobs(self.namePatterns, path) {
		return fmt.Errorf("%s: %w", path, ErrDenied)
	}
	for _, entry := range self.absolutePaths {
		if _, isDenied := pathutil.RelativeTo(entry, path); isDenied {
			return fmt.Errorf("%s: %w", path, ErrDenied)
		}
	}

	resolvedPath := pathutil.Canonicalise(path)
	if util.MatchNameGlobs(self.namePatterns, resolvedPath) {
		return fmt.Errorf("%s: %w", path, ErrDenied)
	}
	for _, entry := range self.absolutePaths {
		if _, isDenied := pathutil.RelativeTo(pathutil.Canonicalise(entry), resolvedPath); isDenied {
			return fmt.Errorf("%s: %w", path, ErrDenied)
		}
	}
	return nil
}
