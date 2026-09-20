package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"crdx.org/oh/internal/util/pathutil"

	"golang.org/x/sys/unix"
)

const (
	separator          = string(os.PathSeparator)
	maxResolutionSteps = 128
)

func pathComponents(path string) []string {
	var parts []string

	for part := range strings.SplitSeq(path, separator) {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}

	return parts
}

func resolvedRoots(roots []string) []string {
	paths := make([]string, 0, len(roots))

	for _, root := range roots {
		paths = append(paths, pathutil.Canonicalise(root))
	}

	return paths
}

func openGrantPath(path string, writableRoots []string) (int, error) {
	if !filepath.IsAbs(path) {
		return -1, fmt.Errorf("%s is not an absolute path", path)
	}

	fd, err := unix.Open(separator, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}

	roots := resolvedRoots(writableRoots)
	currentDir := separator
	remainingComponents := pathComponents(filepath.Clean(path))

	for steps := 0; len(remainingComponents) > 0; steps++ {
		if steps > maxResolutionSteps {
			_ = unix.Close(fd)
			return -1, fmt.Errorf("%s passes through too many symbolic links", path)
		}

		part := remainingComponents[0]
		remainingComponents = remainingComponents[1:]

		next, err := unix.Openat(fd, part, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			_ = unix.Close(fd)
			return -1, err
		}

		if !isSymbolicLink(next) {
			_ = unix.Close(fd)
			fd = next
			currentDir = filepath.Join(currentDir, part)
			continue
		}

		_ = unix.Close(next)

		if isBeneathAny(currentDir, roots) {
			_ = unix.Close(fd)
			return -1, fmt.Errorf("%s is a symbolic link", filepath.Join(currentDir, part))
		}

		target, err := readLinkAt(fd, part)
		if err != nil {
			_ = unix.Close(fd)
			return -1, err
		}

		if filepath.IsAbs(target) {
			root, err := unix.Open(separator, unix.O_PATH|unix.O_CLOEXEC, 0)
			if err != nil {
				_ = unix.Close(fd)
				return -1, err
			}

			_ = unix.Close(fd)
			fd = root
			currentDir = separator
		}

		remainingComponents = append(pathComponents(target), remainingComponents...)
	}

	return fd, nil
}

func readLinkAt(directory int, name string) (string, error) {
	buffer := make([]byte, unix.PathMax)

	length, err := unix.Readlinkat(directory, name, buffer)
	if err != nil {
		return "", err
	}

	return string(buffer[:length]), nil
}

func isSymbolicLink(fd int) bool {
	var stat unix.Stat_t

	if err := unix.Fstat(fd, &stat); err != nil {
		return false
	}

	return stat.Mode&unix.S_IFMT == unix.S_IFLNK
}

func isBeneathAny(path string, roots []string) bool {
	path = filepath.Clean(path)

	for _, root := range roots {
		root = filepath.Clean(root)
		if path == root || root == separator || strings.HasPrefix(path, root+separator) {
			return true
		}
	}

	return false
}

func FirstSymlinkBeneath(path string, writableRoots []string) (string, bool) {
	roots := resolvedRoots(writableRoots)
	current := separator
	remainingComponents := pathComponents(filepath.Clean(path))

	for steps := 0; len(remainingComponents) > 0; steps++ {
		if steps > maxResolutionSteps {
			return "", false
		}

		part := remainingComponents[0]
		remainingComponents = remainingComponents[1:]
		candidate := filepath.Join(current, part)

		info, err := os.Lstat(candidate)
		if err != nil {
			return "", false
		}

		if info.Mode()&os.ModeSymlink == 0 {
			if len(remainingComponents) > 0 && !info.IsDir() {
				return "", false
			}

			current = candidate
			continue
		}

		if isBeneathAny(current, roots) {
			return candidate, true
		}

		target, err := os.Readlink(candidate)
		if err != nil {
			return "", false
		}

		if filepath.IsAbs(target) {
			current = separator
		}

		remainingComponents = append(pathComponents(target), remainingComponents...)
	}

	return "", false
}

func (self Policy) grantPathsSafe() error {
	for _, grant := range self.grants() {
		if grant.isOptional && !pathutil.Exists(grant.path) {
			continue
		}

		if link, redirects := FirstSymlinkBeneath(grant.path, self.Write); redirects {
			return fmt.Errorf("a grant may not pass through %s, a symbolic link", link)
		}
	}

	return nil
}
