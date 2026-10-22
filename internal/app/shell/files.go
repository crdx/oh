package shell

import (
	"os"
	"path/filepath"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/sandbox"
)

func RefuseHomeWrite(mode *caps.Mode) func(name string) error {
	return func(name string) error {
		if !mode.Current().Has(caps.Write) {
			return file.ErrReadOnly
		}
		return caps.RefuseWrite(mode)(name)
	}
}

const homeCacheName = ".cache"

func HomeCacheDirectory(homeDirectory string) string {
	return filepath.Join(homeDirectory, homeCacheName)
}

func OpenHomeCache(roots Roots) (*os.Root, error) {
	return roots.Home.OpenRoot(homeCacheName)
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
