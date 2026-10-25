package location

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"

	"crdx.org/oh/internal/state"
)

const dataLayoutLockName = "layout"

var dataFileNames = [...]string{
	"analysis.json",
	"history",
	"model-round-robin.json",
	"models.json",
	"rates.json",
	"seen_models.json",
	"subagent-round-robin.json",
}

func RequireCurrentDataLayout() error {
	return state.Locked(GetDataDir(dataLayoutLockName), func() error {
		isNeeded, err := isDataLayoutMigrationNeeded()
		if err != nil {
			return err
		}
		if isNeeded {
			return errors.New("state data needs migrating: run `oh --ctl migrate`")
		}

		return nil
	})
}

func IsDataLayoutMigrationNeeded() (bool, error) {
	return isDataLayoutMigrationNeeded()
}

func MigrateDataLayout(isDryRun bool) (int, error) {
	if isDryRun {
		return migrateDataLayout(true)
	}

	movedFileCount := 0
	err := state.Locked(GetDataDir(dataLayoutLockName), func() error {
		var err error
		movedFileCount, err = migrateDataLayout(false)
		return err
	})
	return movedFileCount, err
}

func migrateDataLayout(isDryRun bool) (int, error) {
	files, err := legacyDataFiles()
	if err != nil || len(files) == 0 {
		return 0, err
	}

	for _, name := range files {
		to := GetDataDir(name)
		if _, err := os.Lstat(to); err == nil {
			return 0, fmt.Errorf("state data exists at both %s and %s", GetStateDir(name), to)
		} else if !os.IsNotExist(err) {
			return 0, fmt.Errorf("inspect state data destination: %w", err)
		}
	}

	if isDryRun {
		return len(files), nil
	}
	if err := os.MkdirAll(GetDataDir(), 0o700); err != nil {
		return 0, fmt.Errorf("create state data directory: %w", err)
	}
	for _, name := range files {
		from := GetStateDir(name)
		to := GetDataDir(name)
		if err := unix.Renameat2(unix.AT_FDCWD, from, unix.AT_FDCWD, to, unix.RENAME_NOREPLACE); err != nil {
			if errors.Is(err, unix.EEXIST) {
				return 0, fmt.Errorf("state data exists at both %s and %s", from, to)
			}
			return 0, fmt.Errorf("move state data %s: %w", name, err)
		}
	}

	return len(files), nil
}

func isDataLayoutMigrationNeeded() (bool, error) {
	files, err := legacyDataFiles()
	return len(files) > 0, err
}

func legacyDataFiles() ([]string, error) {
	var files []string
	for _, name := range dataFileNames {
		for _, candidate := range []string{name, name + ".lock"} {
			info, err := os.Lstat(GetStateDir(candidate))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("inspect state data %s: %w", candidate, err)
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("state data %s is not a regular file", GetStateDir(candidate))
			}
			files = append(files, candidate)
		}
	}

	return files, nil
}
