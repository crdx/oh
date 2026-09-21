package sandbox

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"crdx.org/oh/internal/util"
	"crdx.org/oh/internal/util/pathutil"
)

type DenySearch func(patterns []string, root string) ([]string, error)

func (self Policy) DiscoverDenyPaths() ([]string, error) {
	return self.DiscoverDenyPathsWith(FindDenyPaths)
}

func (self Policy) DiscoverDenyPathsWith(search DenySearch) ([]string, error) {
	if len(self.Deny) == 0 {
		return nil, nil
	}

	var matches []string
	for _, root := range self.denyRoots() {
		found, err := search(self.Deny, root)
		if err != nil {
			return nil, err
		}
		matches = append(matches, found...)
	}
	if self.TmpDir == "" {
		return outermostPaths(matches), nil
	}

	scratchMatches, err := search(self.Deny, self.TmpDir)
	if err != nil {
		return nil, err
	}
	for _, match := range scratchMatches {
		relative, isScratchPath := pathutil.RelativeTo(self.TmpDir, match)
		if isScratchPath {
			matches = append(matches, filepath.Join(TmpDir, relative))
		}
	}
	return outermostPaths(matches), nil
}

func FindDenyPaths(patterns []string, root string) ([]string, error) {
	for _, pattern := range patterns {
		if err := util.ValidateNameGlob(pattern); err != nil {
			return nil, fmt.Errorf("invalid deny pattern %q: %w", pattern, err)
		}
	}

	var matches []string
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if name == root {
				return walkErr
			}
			return nil
		}
		if !util.MatchNameGlobs(patterns, name) {
			return nil
		}

		resolvedPath, err := filepath.EvalSymlinks(name)
		if err != nil {
			return err
		}
		matches = append(matches, filepath.Clean(resolvedPath))
		if entry.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("could not search for denied names beneath %s: %w", root, err)
	}

	return outermostPaths(matches), nil
}

func (self Policy) denyRoots() []string {
	var roots []string
	for _, grant := range self.grants() {
		if grant.isOptional && !pathutil.Exists(grant.path) {
			continue
		}
		if self.TmpDir != "" {
			if grant.path == TmpDir {
				continue
			}
			if grant.path != self.TmpDir {
				if _, coversScratch := pathutil.RelativeTo(grant.path, self.TmpDir); coversScratch {
					continue
				}
			}
		}
		roots = append(roots, filepath.Clean(grant.path))
		resolvedPath, err := filepath.EvalSymlinks(grant.path)
		if err == nil && resolvedPath != grant.path {
			roots = append(roots, filepath.Clean(resolvedPath))
		}
	}
	return outermostPaths(roots)
}

func outermostPaths(paths []string) []string {
	slices.SortFunc(paths, func(left string, right string) int {
		if difference := len(left) - len(right); difference != 0 {
			return difference
		}
		return strings.Compare(left, right)
	})
	paths = slices.Compact(paths)

	outerPaths := make([]string, 0, len(paths))
	for _, name := range paths {
		isCovered := false
		for _, earlier := range outerPaths {
			info, err := filepath.Rel(earlier, name)
			if err == nil && info != ".." && !strings.HasPrefix(info, ".."+string(filepath.Separator)) {
				isCovered = true
				break
			}
		}
		if !isCovered {
			outerPaths = append(outerPaths, name)
		}
	}
	return outerPaths
}
