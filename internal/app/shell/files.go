package shell

import (
	"path/filepath"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/sandbox"
)

func HomeRoot(homeDirectory string, mode *caps.Mode) *file.Root {
	return file.NewLazy(homeDirectory, refuseHomeWrite(mode))
}

func refuseHomeWrite(mode *caps.Mode) func(name string) error {
	return func(name string) error {
		if !mode.Current().Has(caps.Write) {
			return file.ErrReadOnly
		}
		return caps.RefuseWrite(mode)(name)
	}
}

func MountHomeDirectory(files *file.Root, homeDirectory string, mode *caps.Mode) *file.Root {
	homeRoot := HomeRoot(homeDirectory, mode)
	files.Mount(homeDirectory, homeRoot)
	return homeRoot
}

const homeCacheName = ".cache"

func HomeCacheDirectory(homeDirectory string) string {
	return filepath.Join(homeDirectory, homeCacheName)
}

func HomeCacheRoot(homeDirectory string) *file.Root {
	return file.NewLazy(HomeCacheDirectory(homeDirectory), func(string) error { return nil })
}

func MountHomeCache(files *file.Root, homeDirectory string) *file.Root {
	cacheRoot := HomeCacheRoot(homeDirectory)
	files.Mount(HomeCacheDirectory(homeDirectory), cacheRoot)
	return cacheRoot
}

func ScratchRoot(scratchDirectory string) *file.Root {
	return file.NewLazy(scratchDirectory, func(string) error { return nil })
}

func MountScratch(files *file.Root, scratchDirectory string, scratchRoot *file.Root, isUnconfined bool) []*file.Root {
	if !isUnconfined {
		files.MountVirtual(sandbox.TmpDir, scratchRoot)
		return []*file.Root{scratchRoot}
	}

	hostRoot := file.NewLazy(sandbox.TmpDir, func(string) error { return nil })
	files.Mount(sandbox.TmpDir, hostRoot)
	files.Mount(scratchDirectory, scratchRoot)
	return []*file.Root{hostRoot, scratchRoot}
}

func MountTemporaryDirectory(files *file.Root, temporaryDirectory string) *file.Root {
	return MountScratch(files, temporaryDirectory, ScratchRoot(temporaryDirectory), false)[0]
}

func MountHostTemporaryDirectory(files *file.Root, scratchDirectory string) []*file.Root {
	return MountScratch(files, scratchDirectory, ScratchRoot(scratchDirectory), true)
}
