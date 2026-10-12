package file

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func writeRootFileSafely(root *os.Root, name string, data []byte, permission os.FileMode) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE, permission.Perm())
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	return writeOpenedFileSafely(file, data)
}

func writeExactFileSafely(path string, data []byte, permission os.FileMode) error {
	directory, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	return writeRootFileSafely(directory, filepath.Base(path), data, permission)
}

func writeOpenedFileSafely(file *os.File, data []byte) error {
	var info syscall.Stat_t
	if err := syscall.Fstat(int(file.Fd()), &info); err != nil {
		return err
	}
	if info.Nlink > 1 {
		return ErrMultipleHardLinks
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	_, err := file.Write(data)
	return err
}

func writeWithoutSymlinks(root *os.Root, name string, data []byte, permission os.FileMode) error {
	if !filepath.IsLocal(name) {
		return ErrOutsideRoot
	}
	directory, err := openDirectoryWithoutSymlinks(root, filepath.Dir(name))
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()

	descriptor, err := syscall.Openat(int(directory.Fd()), filepath.Base(name), syscall.O_WRONLY|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, uint32(permission.Perm()))
	if errors.Is(err, syscall.ELOOP) {
		return ErrGitDir
	}
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(descriptor), name)
	defer func() { _ = file.Close() }()
	return writeOpenedFileSafely(file, data)
}

func openDirectoryWithoutSymlinks(root *os.Root, name string) (*os.File, error) {
	directory, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	if name == "." {
		return directory, nil
	}
	for part := range strings.SplitSeq(name, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		descriptor, openError := syscall.Openat(int(directory.Fd()), part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		_ = directory.Close()
		if openError != nil {
			if errors.Is(openError, syscall.ELOOP) || errors.Is(openError, syscall.ENOTDIR) {
				return nil, ErrGitDir
			}
			return nil, openError
		}
		directory = os.NewFile(uintptr(descriptor), part)
	}
	return directory, nil
}

func (self *Root) mkdirWithoutSymlinks(root *os.Root, name string, permission os.FileMode) error {
	if !filepath.IsLocal(name) {
		return ErrOutsideRoot
	}
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	prefix := "."
	for part := range strings.SplitSeq(name, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		prefix = filepath.Join(prefix, part)
		descriptor, openError := syscall.Openat(int(directory.Fd()), part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if errors.Is(openError, syscall.ENOENT) {
			if err := self.refuseWrite(prefix); err != nil {
				return err
			}
			if err := syscall.Mkdirat(int(directory.Fd()), part, uint32(permission.Perm())); err != nil && !errors.Is(err, fs.ErrExist) {
				return err
			}
			descriptor, openError = syscall.Openat(int(directory.Fd()), part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		}
		if openError != nil {
			if errors.Is(openError, syscall.ELOOP) || errors.Is(openError, syscall.ENOTDIR) {
				return ErrGitDir
			}
			return openError
		}
		_ = directory.Close()
		directory = os.NewFile(uintptr(descriptor), part)
	}
	return nil
}

func (self *Root) mkdirWithRefusals(root *os.Root, name string, permission os.FileMode) error {
	prefix := "."
	for part := range strings.SplitSeq(filepath.Clean(name), string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		prefix = filepath.Join(prefix, part)
		if _, err := root.Lstat(prefix); errors.Is(err, fs.ErrNotExist) {
			if err := self.refuseWrite(prefix); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	return root.MkdirAll(name, permission)
}
