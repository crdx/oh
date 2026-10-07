package shell

import (
	"path/filepath"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/sandbox"
)

func MountHomeDirectory(files *file.Root, homeDirectory string, mode *caps.Mode) *file.Root {
	homeRoot := file.NewLazy(homeDirectory, caps.RefuseWrite(mode))
	files.Mount(homeDirectory, homeRoot)
	return homeRoot
}

func MountHomeCache(files *file.Root, homeDirectory string) *file.Root {
	cacheDirectory := filepath.Join(homeDirectory, ".cache")
	cacheRoot := file.NewLazy(cacheDirectory, func(string) error { return nil })
	files.Mount(cacheDirectory, cacheRoot)
	return cacheRoot
}

func MountTemporaryDirectory(files *file.Root, temporaryDirectory string) *file.Root {
	temporaryRoot := file.NewLazy(temporaryDirectory, func(string) error { return nil })
	files.Mount(sandbox.TmpDir, temporaryRoot)
	return temporaryRoot
}
