package shell

import (
	"path/filepath"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/sandbox"
)

func MountHomeDirectory(files *file.Root, homeDirectory string, mode *caps.Mode) *file.Root {
	homeRoot := file.NewLazy(homeDirectory, func(name string) error {
		if !mode.Current().Has(caps.Write) {
			return file.ErrReadOnly
		}
		return caps.RefuseWrite(mode)(name)
	})
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
	files.MountVirtual(sandbox.TmpDir, temporaryRoot)
	return temporaryRoot
}

func MountHostTemporaryDirectory(files *file.Root, scratchDirectory string) []*file.Root {
	hostRoot := file.NewLazy(sandbox.TmpDir, func(string) error { return nil })
	files.Mount(sandbox.TmpDir, hostRoot)
	scratchRoot := file.NewLazy(scratchDirectory, func(string) error { return nil })
	files.Mount(scratchDirectory, scratchRoot)
	return []*file.Root{hostRoot, scratchRoot}
}
