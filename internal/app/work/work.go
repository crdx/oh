package work

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"crdx.org/io/internal/sandbox"
	"crdx.org/io/internal/util/pathutil"
)

var ErrShadowed = errors.New("workspace cannot use /tmp because the sandbox shadows it with private scratch space")

type Space struct {
	dir          string
	resolvedDir  string
	resolveError error
	name         string
	shortDir     string
	root         *os.Root
}

func Current() (*Space, error) {
	dir, err := filepath.Abs(".")
	if err != nil {
		return nil, fmt.Errorf("could not resolve the workspace path: %w", err)
	}

	return At(dir), nil
}

func At(dir string) *Space {
	resolvedDir, resolveError := filepath.EvalSymlinks(dir)
	if resolveError != nil {
		resolvedDir = dir
	}

	return &Space{
		dir:          dir,
		resolvedDir:  resolvedDir,
		resolveError: resolveError,
		name:         filepath.Base(dir),
		shortDir:     pathutil.Shorten(dir),
	}
}

func (self *Space) Validate() error {
	if IsShadowed(self.dir) {
		return ErrShadowed
	}

	if self.resolveError != nil {
		return fmt.Errorf("could not resolve workspace links: %w", self.resolveError)
	}

	if IsShadowed(self.resolvedDir) {
		return ErrShadowed
	}

	return nil
}

func (self *Space) IsAt(dir string) bool {
	if self == nil {
		return false
	}

	dir = filepath.Clean(dir)
	if dir == filepath.Clean(self.dir) {
		return true
	}

	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}

	return filepath.Clean(resolvedDir) == filepath.Clean(self.resolvedDir)
}

func (self *Space) Open() error {
	root, err := os.OpenRoot(self.dir)
	if err != nil {
		return err
	}

	self.root = root

	return nil
}

func (self *Space) Close() error {
	if self == nil || self.root == nil {
		return nil
	}

	return self.root.Close()
}

func (self *Space) GetDir() string {
	if self == nil {
		return ""
	}

	return self.dir
}

func (self *Space) GetResolvedDir() string {
	if self == nil {
		return ""
	}

	return self.resolvedDir
}

func (self *Space) GetName() string {
	if self == nil {
		return ""
	}

	return self.name
}

func (self *Space) GetShortDir() string {
	if self == nil {
		return ""
	}

	return self.shortDir
}

func (self *Space) GetRoot() *os.Root {
	if self == nil {
		return nil
	}

	return self.root
}

func IsShadowed(dir string) bool {
	_, isShadowed := pathutil.RelativeTo(sandbox.TmpDir, dir)
	return isShadowed
}
