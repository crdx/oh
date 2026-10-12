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

type DenySearch func(patterns []string, root string, skip string) ([]string, error)

func (self Policy) DiscoverDenyPaths() ([]string, error) {
	return self.DiscoverDenyPathsWith(FindDenyPathsSkipping)
}

func (self Policy) DiscoverDenyPathsWith(search DenySearch) ([]string, error) {
	if len(self.Deny) == 0 {
		return nil, nil
	}

	var matches []string
	roots := self.denyRoots()
	for _, entry := range self.Deny {
		if !filepath.IsAbs(entry) {
			continue
		}
		resolvedPath, err := filepath.EvalSymlinks(entry)
		if err != nil {
			return nil, fmt.Errorf("cannot protect denied path %s: %w", entry, err)
		}
		for _, root := range roots {
			if _, isReachable := pathutil.RelativeTo(root, resolvedPath); isReachable {
				matches = append(matches, resolvedPath)
				break
			}
		}
		if self.TmpDir != "" {
			if relative, isScratch := pathutil.RelativeTo(self.TmpDir, resolvedPath); isScratch {
				matches = append(matches, filepath.Join(TmpDir, relative))
			}
		}
	}
	patterns := util.DenyNamePatterns(self.Deny)
	if len(patterns) == 0 {
		return outermostPaths(matches), nil
	}
	isScratchReachableAtHostPath := false
	for _, root := range roots {
		skip := ""
		if self.TmpDir != "" {
			if relative, isCovered := pathutil.RelativeTo(root, self.TmpDir); isCovered && relative != "." {
				skip = self.TmpDir
				isScratchReachableAtHostPath = true
			}
		}
		found, err := search(patterns, root, skip)
		if err != nil {
			return nil, err
		}
		matches = append(matches, found...)
	}
	if self.TmpDir == "" {
		return outermostPaths(matches), nil
	}

	scratchMatches, err := search(patterns, self.TmpDir, "")
	if err != nil {
		return nil, err
	}
	for _, match := range scratchMatches {
		relative, isScratchPath := pathutil.RelativeTo(self.TmpDir, match)
		if isScratchPath {
			matches = append(matches, filepath.Join(TmpDir, relative))
			if isScratchReachableAtHostPath {
				matches = append(matches, match)
			}
		}
	}
	return outermostPaths(matches), nil
}

func FindDenyPaths(patterns []string, root string) ([]string, error) {
	return FindDenyPathsSkipping(patterns, root, "")
}

func FindDenyPathsSkipping(patterns []string, root string, skip string) ([]string, error) {
	return findDenyPathsWithWalk(patterns, root, skip, filepath.WalkDir)
}

func findDenyPathsWithWalk(patterns []string, root string, skip string, walk func(string, fs.WalkDirFunc) error) ([]string, error) {
	for _, pattern := range patterns {
		if err := util.ValidateNameGlob(pattern); err != nil {
			return nil, fmt.Errorf("invalid deny pattern %q: %w", pattern, err)
		}
	}

	var matches []string
	err := walk(root, func(name string, entry fs.DirEntry, walkErr error) error {
		if name == skip {
			return filepath.SkipDir
		}
		if walkErr != nil {
			if entry != nil && entry.IsDir() && errors.Is(walkErr, fs.ErrPermission) {
				matches = append(matches, pathutil.Canonicalise(name))
				return filepath.SkipDir
			}
			return walkErr
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
	if err != nil {
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
		if grant.path == SharedMemoryPath || grant.path == processFilesystemPath {
			continue
		}
		if self.TmpDir != "" && grant.path == TmpDir {
			continue
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
