package util

import "crdx.org/oh/internal/file"

func RootName(root *file.Root, path string) (string, error) {
	resolvedRoot, name, err := root.Resolve(path)
	if err != nil {
		return "", err
	}
	if resolvedRoot != root {
		return "", file.ErrReadOnly
	}

	return name, nil
}
