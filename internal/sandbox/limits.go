package sandbox

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"crdx.org/oh/internal/util"
	"crdx.org/oh/internal/util/pathutil"

	"golang.org/x/sys/unix"
)

func applyLimits(policy Policy) error {
	limits := []struct {
		resource    int
		value       uint64
		shouldApply bool
	}{
		{unix.RLIMIT_CORE, 0, true},
		{unix.RLIMIT_CPU, uint64(policy.MaxCPUTime.Seconds()), policy.MaxCPUTime > 0},
		{unix.RLIMIT_FSIZE, uint64(policy.MaxFileSize), policy.MaxFileSize > 0},    //nolint:gosec // sane rejects a negative
		{unix.RLIMIT_NOFILE, uint64(policy.MaxOpenFiles), policy.MaxOpenFiles > 0}, //nolint:gosec // sane rejects a negative
		{unix.RLIMIT_NPROC, uint64(policy.MaxProcesses), policy.MaxProcesses > 0},  //nolint:gosec // sane rejects a negative
	}

	for _, limit := range limits {
		if !limit.shouldApply {
			continue
		}

		value := &unix.Rlimit{Cur: limit.value, Max: limit.value}

		var ceiling unix.Rlimit
		if err := unix.Getrlimit(limit.resource, &ceiling); err == nil && ceiling.Max < limit.value {
			value = &unix.Rlimit{Cur: ceiling.Max, Max: ceiling.Max}
		}

		if err := unix.Setrlimit(limit.resource, value); err != nil {
			return fmt.Errorf("could not limit resource %d: %w", limit.resource, err)
		}
	}

	return nil
}

func namedPathSane(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%q is not an absolute path, so it names nothing to grant", path)
	}

	if strings.ContainsRune(path, 0) {
		return fmt.Errorf("%q carries a null byte, so no command can be told about it", path)
	}

	if !utf8.ValidString(path) {
		return fmt.Errorf("%q is not valid UTF-8, so the command would be confined to another path", path)
	}

	return nil
}

func (self Policy) namedPathsSane() error {
	paths := slices.Concat(self.DenyPaths, self.Read, self.Write, self.Exec, self.Sockets)

	if self.TmpDir != "" {
		paths = append(paths, self.TmpDir)
	}

	for _, path := range paths {
		if err := namedPathSane(path); err != nil {
			return err
		}
	}

	return nil
}

func (self Policy) sane() error {
	if err := self.namedPathsSane(); err != nil {
		return err
	}
	for _, pattern := range self.Deny {
		if err := util.ValidateNameGlob(pattern); err != nil {
			return fmt.Errorf("invalid deny pattern %q: %w", pattern, err)
		}
	}

	if self.MaxFileSize < 0 {
		return fmt.Errorf("a file size limit of %d is not a size", self.MaxFileSize)
	}

	if self.MaxOpenFiles < 0 {
		return fmt.Errorf("an open file limit of %d is not a count", self.MaxOpenFiles)
	}

	if self.MaxProcesses < 0 {
		return fmt.Errorf("a process limit of %d is not a count", self.MaxProcesses)
	}

	if self.MaxCPUTime > 0 && self.MaxCPUTime < time.Second {
		return fmt.Errorf("a cpu limit of %s rounds down to no time at all", self.MaxCPUTime)
	}

	for _, path := range self.Sockets {
		if !slices.Contains(self.Write, path) {
			return fmt.Errorf("%s may resolve Unix sockets but is not writable", path)
		}
	}

	grantedPaths := slices.Concat(self.Read, self.Write, self.Exec)
	for _, path := range self.OptionalPaths {
		if !slices.Contains(grantedPaths, path) {
			return fmt.Errorf("%s is optional but is not granted", path)
		}
	}

	return self.reachable()
}

func (self Policy) reachable() error {
	if self.TmpDir == "" {
		return nil
	}

	for _, path := range slices.Concat(self.Read, self.Write, self.Exec) {
		if path == TmpDir {
			continue
		}

		if _, isCovered := pathutil.RelativeTo(TmpDir, path); isCovered {
			return fmt.Errorf("a scratch at %s covers %s, which is granted but unreachable", TmpDir, path)
		}
	}

	return nil
}
