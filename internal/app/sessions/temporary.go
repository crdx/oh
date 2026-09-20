package sessions

import (
	"fmt"
	"os"

	"crdx.org/oh/internal/app/location"
)

func PrepareTemporaryDirectory(name string) (string, error) {
	temporaryDirectory := location.GetTmpDir(name)

	if err := os.MkdirAll(temporaryDirectory, 0o700); err != nil {
		return "", fmt.Errorf("could not prepare the tmp dir: %w", err)
	}

	return temporaryDirectory, nil
}
