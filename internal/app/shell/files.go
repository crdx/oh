package shell

import (
	"fmt"
	"os"
	"path/filepath"

	"crdx.org/io/internal/app/caps"
	"crdx.org/io/internal/file"
	"crdx.org/io/internal/sandbox"
)

func MountHomeDirectory(files *file.Root, homeDirectory string, mode *caps.Mode) (*os.Root, error) {
	homeRoot, err := os.OpenRoot(homeDirectory)
	if err != nil {
		return nil, fmt.Errorf("could not open the shell home: %w", err)
	}

	files.Mount(homeDirectory, file.New(homeRoot, caps.RefuseWrite(mode)))
	return homeRoot, nil
}

func MountHomeCache(files *file.Root, homeDirectory string) (*os.Root, error) {
	cacheDirectory := filepath.Join(homeDirectory, ".cache")
	cacheRoot, err := os.OpenRoot(cacheDirectory)
	if err != nil {
		return nil, fmt.Errorf("could not open the shared cache: %w", err)
	}

	files.Mount(cacheDirectory, file.New(cacheRoot, func(string) error { return nil }))
	return cacheRoot, nil
}

func MountTemporaryDirectory(files *file.Root, temporaryDirectory string) (*os.Root, error) {
	temporaryRoot, err := os.OpenRoot(temporaryDirectory)
	if err != nil {
		return nil, fmt.Errorf("could not open the tmp dir: %w", err)
	}

	files.Mount(sandbox.TmpDir, file.New(temporaryRoot, func(string) error { return nil }))
	return temporaryRoot, nil
}
