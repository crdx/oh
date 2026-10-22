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

var (
	ErrReadOnly          = errors.New("the filesystem is read-only")
	ErrGitDir            = errors.New("refusing to touch anything inside a .git directory")
	ErrMultipleHardLinks = errors.New("refusing to overwrite a file with multiple hard links")
	ErrMountChanged      = errors.New("mounted directory changed since it was granted")
	ErrOutsideRoot       = errors.New("path is outside the root")
)

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
	root      *Root
	name      string
	isExact   bool
	isVirtual bool
}

type Root struct {
	root                  *os.Root
	lazyPath              string
	lazyOpen              sync.Once
	lazyError             error
	expectedDirectoryInfo os.FileInfo
	exactPath             string
	exactName             string
	refuse                func(name string) error
	access                func(path string) error
	excludedNames         []string
	gitWriteAllowed       func() bool
	gitOnly               func() bool
	writeGrantAllows      func(name string) bool
	governor              func(path string, fallback func() error) error
	defersWrites          bool

	mountsMutex sync.RWMutex
	mounts      map[string]mountedRoot
}

func New(root *os.Root, refuseWrite func(name string) error) *Root {
	return &Root{root: root, refuse: refuseWrite, mounts: map[string]mountedRoot{}}
}

func NewLazy(path string, refuseWrite func(name string) error) *Root {
	return &Root{lazyPath: filepath.Clean(path), refuse: refuseWrite, mounts: map[string]mountedRoot{}}
}

func NewLazyWithIdentity(path string, expectedDirectoryInfo os.FileInfo, refuseWrite func(name string) error) *Root {
	if expectedDirectoryInfo == nil {
		panic("a lazy mount needs its original directory identity")
	}
	root := NewLazy(path, refuseWrite)
	root.expectedDirectoryInfo = expectedDirectoryInfo
	return root
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
	self.mount(path, root, false)
}

func (self *Root) MountVirtual(path string, root *Root) {
	self.mount(path, root, true)
}

func (self *Root) MountFile(path string, root *Root, name string) {
	self.mountsMutex.Lock()
	defer self.mountsMutex.Unlock()
	root.setAccessRefusal(self.access)
	root.setExcludedNames(self.excludedNames)
	root.governor = self.governWrite
	if root != self {
		root.SetGitWriteAllowed(self.gitWriteAllowed)
	}
	if root.exactPath == "" {
		root.exactName = name
	}
	self.mounts[filepath.Clean(path)] = mountedRoot{root: root, name: name, isExact: true}
}

func (self *Root) SetAccessRefusal(refuse func(path string) error) {
	self.mountsMutex.Lock()
	defer self.mountsMutex.Unlock()
	self.setAccessRefusal(refuse)
}

func (self *Root) SetGitOnly(gitOnly func() bool) {
	self.gitOnly = gitOnly
}

func (self *Root) SetWriteGrant(writeGrantAllows func(name string) bool) {
	self.writeGrantAllows = writeGrantAllows
}

func (self *Root) DeferWrites() {
	self.defersWrites = true
}

func (self *Root) SetGitWriteAllowed(isGitWriteAllowed func() bool) {
	self.mountsMutex.Lock()
	defer self.mountsMutex.Unlock()
	self.gitWriteAllowed = isGitWriteAllowed
	for _, mount := range self.mounts {
		if mount.root != self {
			mount.root.SetGitWriteAllowed(isGitWriteAllowed)
		}
	}
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

	self.mountsMutex.RLock()
	directRoot, directName, isVirtual := self.mountedAt(path)
	self.mountsMutex.RUnlock()
	if isVirtual {
		return directRoot, directName, nil
	}
	workspaceName, isWithinWorkspace := pathutil.RelativeTo(self.Name(), path)
	canonical := pathutil.Canonicalise(path)
	if canonical != path {
		self.mountsMutex.RLock()
		resolvedRoot, resolvedName, isResolvedVirtual := self.mountedAt(canonical)
		self.mountsMutex.RUnlock()
		if resolvedRoot != nil && (!isResolvedVirtual || isWithinWorkspace || directRoot != nil) {
			return resolvedRoot, resolvedName, nil
		}
	}
	if isWithinWorkspace {
		return self, workspaceName, nil
	}
	if directRoot != nil {
		return directRoot, directName, nil
	}
	return nil, "", ErrOutsideRoot
}

func (self *Root) RefuseWrite(name string) error {
	if err := self.refuseAccess(name); err != nil {
		return err
	}
	return self.refuseWrite(name)
}

func (self *Root) Name() string {
	if self.exactPath != "" {
		return filepath.Dir(self.exactPath)
	}
	if self.lazyPath != "" {
		return self.lazyPath
	}
	return self.root.Name()
}

func (self *Root) Close() error {
	if self.lazyPath == "" {
		return nil
	}

	wasOpened := true
	self.lazyOpen.Do(func() {
		wasOpened = false
		self.lazyError = os.ErrClosed
	})
	if !wasOpened || self.root == nil {
		return nil
	}

	return self.root.Close()
}

func (self *Root) Prepare() error {
	_, err := self.getRoot()
	return err
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
		return openExactFileNoSymlink(self.exactPath)
	}
	root, err := self.getRoot()
	if err != nil {
		return nil, err
	}
	if self.exactName != "" {
		return openRootFileNoSymlink(root, name, syscall.O_RDONLY, 0)
	}
	return root.Open(name)
}

func (self *Root) ReadFile(name string) ([]byte, error) {
	if err := self.refuseAccess(name); err != nil {
		return nil, err
	}
	if err := self.refuseOtherExactFile(name); err != nil {
		return nil, err
	}
	if self.exactPath != "" {
		return readExactFileNoSymlink(self.exactPath)
	}
	root, err := self.getRoot()
	if err != nil {
		return nil, err
	}
	if self.exactName != "" {
		return readRootFileNoSymlink(root, name)
	}
	return root.ReadFile(name)
}

func (self *Root) Stat(name string) (os.FileInfo, error) {
	if err := self.refuseAccess(name); err != nil {
		return nil, err
	}
	if err := self.refuseOtherExactFile(name); err != nil {
		return nil, err
	}
	if self.exactPath != "" {
		return os.Lstat(self.exactPath)
	}
	root, err := self.getRoot()
	if err != nil {
		return nil, err
	}
	if self.exactName != "" {
		return root.Lstat(name)
	}
	return root.Stat(name)
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
		return writeExactFileSafely(self.exactPath, data, perm)
	}
	root, err := self.getRoot()
	if err != nil {
		return err
	}
	if self.exactName != "" {
		return writeRootFileNoSymlink(root, name, data, perm)
	}
	if (self.gitWriteAllowed != nil && !self.gitWriteAllowed()) || (self.gitOnly != nil && self.gitOnly()) {
		return writeWithoutSymlinks(root, name, data, perm)
	}
	return writeRootFileSafely(root, name, data, perm)
}

func (self *Root) MkdirAll(name string, perm os.FileMode) error {
	if err := self.refuseWrite(name); err != nil {
		return err
	}
	if self.exactPath != "" {
		return ErrOutsideRoot
	}
	root, err := self.getRoot()
	if err != nil {
		return err
	}
	if (self.gitWriteAllowed != nil && !self.gitWriteAllowed()) || (self.gitOnly != nil && self.gitOnly()) {
		return self.mkdirWithoutSymlinks(root, name, perm)
	}
	return self.mkdirWithRefusals(root, name, perm)
}

func (self *Root) mount(path string, root *Root, isVirtual bool) {
	self.mountsMutex.Lock()
	defer self.mountsMutex.Unlock()
	root.setAccessRefusal(self.access)
	root.setExcludedNames(self.excludedNames)
	if !isVirtual {
		root.governor = self.governWrite
	}
	if root != self {
		root.SetGitWriteAllowed(self.gitWriteAllowed)
	}
	self.mounts[filepath.Clean(path)] = mountedRoot{root: root, name: ".", isVirtual: isVirtual}
}

func (self *Root) refuseAt(name string) error {
	fallback := func() error {
		err := self.refuse(name)
		if errors.Is(err, ErrReadOnly) && self.writeGrantAllows != nil && self.writeGrantAllows(name) {
			return nil
		}
		return err
	}
	path := filepath.Join(self.Name(), name)
	if self.exactPath != "" && filepath.Clean(name) == self.exactName {
		path = self.exactPath
	}
	if self.governor != nil {
		return self.governor(path, fallback)
	}
	return self.governWrite(path, fallback)
}

func (self *Root) governWrite(path string, fallback func() error) error {
	type candidate struct {
		root *Root
		name string
	}
	canonical := pathutil.Canonicalise(path)
	var bestCandidate candidate
	var peers []candidate
	chosenLength := 0
	if self.exactPath == "" && !self.defersWrites {
		at := pathutil.Canonicalise(self.Name())
		if name, isWithin := pathutil.RelativeTo(at, canonical); isWithin {
			bestCandidate = candidate{root: self, name: name}
			chosenLength = len(at)
		}
	}

	self.mountsMutex.RLock()
	for mountPath, mount := range self.mounts {
		if mount.root.defersWrites {
			continue
		}
		at := pathutil.Canonicalise(mountPath)
		name, isWithin := pathutil.RelativeTo(at, canonical)
		if !isWithin || len(at) < chosenLength || (mount.isExact && name != ".") {
			continue
		}
		option := candidate{root: mount.root, name: filepath.Join(mount.name, name)}
		if len(at) > chosenLength {
			bestCandidate, peers, chosenLength = option, nil, len(at)
		} else {
			peers = append(peers, option)
		}
	}
	self.mountsMutex.RUnlock()

	decide := func(option candidate) error {
		if option.root == nil || option.root == self {
			return fallback()
		}
		return option.root.refuse(option.name)
	}
	if len(peers) == 0 {
		return decide(bestCandidate)
	}
	isWritable := false
	var hardRefusal error
	for _, option := range append([]candidate{bestCandidate}, peers...) {
		err := decide(option)
		switch {
		case err == nil:
			isWritable = true
		case !errors.Is(err, ErrReadOnly) && hardRefusal == nil:
			hardRefusal = err
		}
	}
	if hardRefusal != nil {
		return hardRefusal
	}
	if isWritable {
		return nil
	}
	return ErrReadOnly
}

func (self *Root) getRoot() (*os.Root, error) {
	if self.lazyPath == "" {
		return self.root, nil
	}

	self.lazyOpen.Do(func() {
		self.root, self.lazyError = os.OpenRoot(self.lazyPath)
		if self.lazyError != nil || self.expectedDirectoryInfo == nil {
			return
		}
		openedInfo, err := self.root.Stat(".")
		switch {
		case err != nil:
			self.lazyError = err
		case !os.SameFile(openedInfo, self.expectedDirectoryInfo):
			self.lazyError = &os.PathError{Op: "open", Path: self.lazyPath, Err: ErrMountChanged}
		}
		if self.lazyError != nil {
			_ = self.root.Close()
			self.root = nil
		}
	})

	return self.root, self.lazyError
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

func (self *Root) mountedAt(path string) (*Root, string, bool) {
	var resolvedRoot *Root
	resolvedName := ""
	resolvedAt := ""
	isVirtual := false

	for at, mountedRoot := range self.mounts {
		name, below := pathutil.RelativeTo(at, path)
		if !below || len(at) <= len(resolvedAt) || (mountedRoot.isExact && name != ".") {
			continue
		}

		resolvedRoot = mountedRoot.root
		resolvedName = filepath.Join(mountedRoot.name, name)
		resolvedAt = at
		isVirtual = mountedRoot.isVirtual
	}

	return resolvedRoot, resolvedName, isVirtual
}

func (self *Root) refuseWrite(name string) error {
	if err := self.refuseAccess(name); err != nil {
		return err
	}
	if err := self.refuseAt(name); err != nil {
		return err
	}
	if self.gitOnly != nil && self.gitOnly() && InGitDir(name) {
		root, err := self.getRoot()
		if err != nil {
			return err
		}
		info, err := root.Lstat(".git")
		if errors.Is(err, fs.ErrNotExist) || (err == nil && !info.IsDir()) {
			return ErrReadOnly
		}
		if err != nil {
			return err
		}
	}

	if err := self.refuseOtherExactFile(name); err != nil {
		return err
	}
	if self.exactPath != "" {
		return nil
	}

	root, err := self.getRoot()
	if err != nil {
		return err
	}
	resolvedName := filepath.Clean(name)

	for range 255 {
		parts := strings.Split(resolvedName, string(filepath.Separator))
		didFollowSymlink := false

		for i := range parts {
			prefix := filepath.Join(parts[:i+1]...)
			info, err := root.Lstat(prefix)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if info.Mode()&os.ModeSymlink == 0 {
				continue
			}

			target, err := root.Readlink(prefix)
			if err != nil {
				return err
			}

			remainingPath := strings.Join(parts[i+1:], string(filepath.Separator))
			resolvedName = filepath.Clean(filepath.Join(filepath.Dir(prefix), target, remainingPath))
			if err := self.refuseAccess(resolvedName); err != nil {
				return err
			}
			if err := self.refuseAt(resolvedName); err != nil {
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
