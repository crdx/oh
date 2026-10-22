package file

import (
	"errors"
	"io"
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

func readRootFileNoSymlink(root *os.Root, name string) ([]byte, error) {
	file, err := openRootFileNoSymlink(root, name, syscall.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	return readOpenedFileNoSymlink(file)
}

func openExactFileNoSymlink(path string) (*os.File, error) {
	descriptor, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if errors.Is(err, syscall.ELOOP) {
		return nil, ErrOutsideRoot
	}
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(descriptor), filepath.Base(path)), nil
}

func readExactFileNoSymlink(path string) ([]byte, error) {
	file, err := openExactFileNoSymlink(path)
	if err != nil {
		return nil, err
	}
	return readOpenedFileNoSymlink(file)
}

func readOpenedFileNoSymlink(file *os.File) ([]byte, error) {
	defer func() { _ = file.Close() }()
	return io.ReadAll(file)
}
