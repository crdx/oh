package diskutil

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

const (
	blockBytes = 512
	ownerPerm  = 0o700
)

func Occupied(root string) (int64, error) {
	var totalBytes int64

	err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return skip(entry)
		}

		info, err := entry.Info()
		if err != nil {
			return skip(entry)
		}
		totalBytes += occupiedBytes(info)

		return nil
	})

	return totalBytes, err
}

func RemoveAll(path string) error {
	err := os.RemoveAll(path)
	if err == nil || !os.IsPermission(err) {
		return err
	}

	if err := unlock(path); err != nil {
		return err
	}

	return os.RemoveAll(path)
}

func unlock(root string) error {
	var lockedDirectories []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return skip(entry)
		}
		if !entry.IsDir() {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return skip(entry)
		}
		if info.Mode().Perm()&ownerPerm != ownerPerm {
			lockedDirectories = append(lockedDirectories, path)
		}

		return nil
	})
	if err != nil {
		return err
	}

	for _, path := range lockedDirectories {
		if err := os.Chmod(path, ownerPerm); err != nil {
			return err
		}
	}

	return nil
}

func skip(entry fs.DirEntry) error {
	if entry != nil && entry.IsDir() {
		return fs.SkipDir
	}

	return nil
}

func occupiedBytes(info fs.FileInfo) int64 {
	stat, isSystem := info.Sys().(*syscall.Stat_t)
	if !isSystem || !info.Mode().IsRegular() {
		return 0
	}

	return stat.Blocks * blockBytes
}
