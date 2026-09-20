package file

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"

	"crdx.org/oh/internal/util/pathutil"
)

var ErrReadOnly = errors.New("the filesystem is read-only")

var ErrGitDir = errors.New("refusing to touch anything inside a .git directory")

var ErrOutsideRoot = errors.New("path is outside the root")

func InGitDir(name string) bool {
	segments := strings.Split(filepath.Clean(name), string(filepath.Separator))

	return slices.Contains(segments, ".git")
}

func RefuseGitDir(name string) error {
	if InGitDir(name) {
		return ErrGitDir
	}

	return nil
}

type mountedRoot struct {
	root    *Root
	name    string
	isExact bool
}

type Root struct {
	root          *os.Root
	exactPath     string
	exactName     string
	refuse        func(name string) error
	access        func(path string) error
	excludedNames []string

	mountsMutex sync.RWMutex
	mounts      map[string]mountedRoot
}

func New(root *os.Root, refuseWrite func(name string) error) *Root {
	return &Root{root: root, refuse: refuseWrite, mounts: map[string]mountedRoot{}}
}

func NewExactFile(path string, refuseWrite func(name string) error) *Root {
	path = filepath.Clean(path)
	return &Root{
		exactPath: path,
		exactName: filepath.Base(path),
		refuse:    refuseWrite,
		mounts:    map[string]mountedRoot{},
	}
}

func (self *Root) Mount(path string, root *Root) {
	self.mountsMutex.Lock()
	defer self.mountsMutex.Unlock()
	root.setAccessRefusal(self.access)
	root.setExcludedNames(self.excludedNames)
	self.mounts[filepath.Clean(path)] = mountedRoot{root: root, name: "."}
}

func (self *Root) MountFile(path string, root *Root, name string) {
	self.mountsMutex.Lock()
	defer self.mountsMutex.Unlock()
	root.setAccessRefusal(self.access)
	root.setExcludedNames(self.excludedNames)
	self.mounts[filepath.Clean(path)] = mountedRoot{root: root, name: name, isExact: true}
}

func (self *Root) SetAccessRefusal(refuse func(path string) error) {
	self.mountsMutex.Lock()
	defer self.mountsMutex.Unlock()
	self.setAccessRefusal(refuse)
}

func (self *Root) SetExcludedNames(patterns []string) {
	self.mountsMutex.Lock()
	defer self.mountsMutex.Unlock()
	self.setExcludedNames(patterns)
}

func (self *Root) ExcludedNames() []string {
	self.mountsMutex.RLock()
	defer self.mountsMutex.RUnlock()
	return slices.Clone(self.excludedNames)
}

func (self *Root) Unmount(path string) {
	self.mountsMutex.Lock()
	defer self.mountsMutex.Unlock()
	delete(self.mounts, filepath.Clean(path))
}

func (self *Root) Resolve(path string) (*Root, string, error) {
	if path == "" {
		return self, ".", nil
	}
	if !filepath.IsAbs(path) {
		if !filepath.IsLocal(path) {
			return nil, "", ErrOutsideRoot
		}

		return self, path, nil
	}

	canonical := pathutil.Canonicalise(path)
	if canonical != path {
		self.mountsMutex.RLock()
		root, name := self.mountedAt(canonical)
		self.mountsMutex.RUnlock()
		if root != nil {
			return root, name, nil
		}
	}

	if name, ok := pathutil.RelativeTo(self.Name(), path); ok {
		return self, name, nil
	}

	self.mountsMutex.RLock()
	defer self.mountsMutex.RUnlock()

	if root, name := self.mountedAt(path); root != nil {
		return root, name, nil
	}

	return nil, "", ErrOutsideRoot
}

func (self *Root) RefuseWrite(name string) error {
	if err := self.refuseAccess(name); err != nil {
		return err
	}
	return self.refuse(name)
}

func (self *Root) Name() string {
	if self.exactPath != "" {
		return filepath.Dir(self.exactPath)
	}
	return self.root.Name()
}

func (self *Root) FS() fs.FS {
	return rootFileSystem{root: self}
}

func (self *Root) Open(name string) (*os.File, error) {
	if err := self.refuseAccess(name); err != nil {
		return nil, err
	}
	if err := self.refuseOtherExactFile(name); err != nil {
		return nil, err
	}
	if self.exactPath != "" {
		return os.Open(self.exactPath)
	}
	return self.root.Open(name)
}

func (self *Root) ReadFile(name string) ([]byte, error) {
	if err := self.refuseAccess(name); err != nil {
		return nil, err
	}
	if err := self.refuseOtherExactFile(name); err != nil {
		return nil, err
	}
	if self.exactPath != "" {
		return os.ReadFile(self.exactPath)
	}
	return self.root.ReadFile(name)
}

func (self *Root) Stat(name string) (os.FileInfo, error) {
	if err := self.refuseAccess(name); err != nil {
		return nil, err
	}
	if err := self.refuseOtherExactFile(name); err != nil {
		return nil, err
	}
	if self.exactPath != "" {
		return os.Stat(self.exactPath)
	}
	return self.root.Stat(name)
}

func (self *Root) ReadDir(name string) ([]os.DirEntry, error) {
	directory, err := self.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = directory.Close() }()

	entries, err := directory.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(entries, func(left os.DirEntry, right os.DirEntry) int {
		return strings.Compare(left.Name(), right.Name())
	})
	return slices.DeleteFunc(entries, func(entry os.DirEntry) bool {
		return self.refuseAccess(filepath.Join(name, entry.Name())) != nil
	}), nil
}

func (self *Root) WriteFile(name string, data []byte, perm os.FileMode) error {
	if err := self.refuseWrite(name); err != nil {
		return err
	}
	if err := self.refuseOtherExactFile(name); err != nil {
		return err
	}
	if self.exactPath != "" {
		return os.WriteFile(self.exactPath, data, perm)
	}
	return self.root.WriteFile(name, data, perm)
}

func (self *Root) MkdirAll(name string, perm os.FileMode) error {
	if err := self.refuseWrite(name); err != nil {
		return err
	}
	if self.exactPath != "" {
		return ErrOutsideRoot
	}
	return self.root.MkdirAll(name, perm)
}

func (self *Root) setAccessRefusal(refuse func(path string) error) {
	self.access = refuse
	for _, mount := range self.mounts {
		mount.root.SetAccessRefusal(refuse)
	}
}

func (self *Root) setExcludedNames(patterns []string) {
	self.excludedNames = slices.Clone(patterns)
	for _, mount := range self.mounts {
		mount.root.SetExcludedNames(patterns)
	}
}

func (self *Root) refuseAccess(name string) error {
	self.mountsMutex.RLock()
	refuse := self.access
	self.mountsMutex.RUnlock()
	if refuse == nil {
		return nil
	}

	path := filepath.Join(self.Name(), name)
	if self.exactPath != "" && filepath.Clean(name) == self.exactName {
		path = self.exactPath
	}
	return refuse(path)
}

func (self *Root) refuseOtherExactFile(name string) error {
	if self.exactPath != "" && filepath.Clean(name) != self.exactName {
		return ErrOutsideRoot
	}
	return nil
}

type rootFileSystem struct {
	root *Root
}

func (self rootFileSystem) Open(name string) (fs.File, error) {
	return self.root.Open(name)
}

func (self rootFileSystem) ReadDir(name string) ([]fs.DirEntry, error) {
	return self.root.ReadDir(name)
}

func (self *Root) mountedAt(path string) (*Root, string) {
	var resolvedRoot *Root
	resolvedName := ""
	resolvedAt := ""

	for at, mountedRoot := range self.mounts {
		name, below := pathutil.RelativeTo(at, path)
		if !below || len(at) <= len(resolvedAt) || (mountedRoot.isExact && name != ".") {
			continue
		}

		resolvedRoot = mountedRoot.root
		resolvedName = filepath.Join(mountedRoot.name, name)
		resolvedAt = at
	}

	return resolvedRoot, resolvedName
}

func (self *Root) refuseWrite(name string) error {
	if err := self.refuseAccess(name); err != nil {
		return err
	}
	if err := self.refuse(name); err != nil {
		return err
	}

	if err := self.refuseOtherExactFile(name); err != nil {
		return err
	}
	if self.exactPath != "" {
		return nil
	}

	resolvedName := filepath.Clean(name)

	for range 255 {
		parts := strings.Split(resolvedName, string(filepath.Separator))
		didFollowSymlink := false

		for i := range parts {
			prefix := filepath.Join(parts[:i+1]...)
			info, err := self.root.Lstat(prefix)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if info.Mode()&os.ModeSymlink == 0 {
				continue
			}

			target, err := self.root.Readlink(prefix)
			if err != nil {
				return err
			}

			remainingPath := strings.Join(parts[i+1:], string(filepath.Separator))
			resolvedName = filepath.Clean(filepath.Join(filepath.Dir(prefix), target, remainingPath))
			if err := self.refuse(resolvedName); err != nil {
				return err
			}

			didFollowSymlink = true
			break
		}

		if !didFollowSymlink {
			return nil
		}
	}

	return &os.PathError{Op: "write", Path: name, Err: syscall.ELOOP}
}
