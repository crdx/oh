package file

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

func openRootFileNoSymlink(root *os.Root, name string, flag int, permission os.FileMode) (*os.File, error) {
	if !filepath.IsLocal(name) {
		return nil, ErrOutsideRoot
	}
	directory, err := openDirectoryWithoutSymlinks(root, filepath.Dir(name))
	if err != nil {
		return nil, err
	}
	defer func() { _ = directory.Close() }()

	descriptor, err := syscall.Openat(
		int(directory.Fd()),
		filepath.Base(name),
		flag|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,
		uint32(permission.Perm()),
	)
	if errors.Is(err, syscall.ELOOP) {
		return nil, ErrOutsideRoot
	}
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(descriptor), name), nil
}

func openExactFileNoSymlink(path string) (*os.File, error) {
	descriptor, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if errors.Is(err, syscall.ELOOP) {
		return nil, ErrOutsideRoot
	}
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(descriptor), filepath.Base(path)), nil
}

func refuseSpecialFile(openedFile *os.File, name string) (*os.File, error) {
	info, err := openedFile.Stat()
	if err != nil {
		_ = openedFile.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		_ = openedFile.Close()
		return nil, &fs.PathError{Op: "open", Path: name, Err: ErrSpecialFile}
	}
	return openedFile, nil
}

func readOpenedFile(openedFile *os.File) ([]byte, error) {
	defer func() { _ = openedFile.Close() }()

	var content bytes.Buffer
	if info, err := openedFile.Stat(); err == nil && info.Mode().IsRegular() {
		content.Grow(int(info.Size()) + bytes.MinRead)
	}
	if _, err := content.ReadFrom(openedFile); err != nil {
		return nil, err
	}
	return content.Bytes(), nil
}
