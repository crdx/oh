package diskutil

import (
	"io/fs"
	"path/filepath"
	"syscall"
)

const blockBytes = 512

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
