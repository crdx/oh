package startup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"crdx.org/oh/internal/util"
)

const (
	MaxDenyWorkspaceEntries = 100_000
	denyDirectoryBatchSize  = 256
)

func CheckDenyWorkspace(ctx context.Context, root string, denies []string, hasShell bool, isForced bool) error {
	if isForced || !hasShell || len(util.DenyNamePatterns(denies)) == 0 {
		return nil
	}
	return checkDenyWorkspace(ctx, root, MaxDenyWorkspaceEntries)
}

func checkDenyWorkspace(ctx context.Context, root string, limit int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	count, err := countDenyWorkspace(ctx, root, limit)
	if err == nil && count > limit {
		err = fmt.Errorf("workspace %s has more than %d filesystem entries; non-absolute deny patterns with capability x require a recursive search before every command", root, limit)
	}
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("workspace deny-search check: %w; use absolute deny paths, disable x, or restart with -f/--force", err)
}

func countDenyWorkspace(ctx context.Context, directory string, limit int) (int, error) {
	root, err := os.OpenRoot(directory)
	if errors.Is(err, fs.ErrPermission) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer func() { _ = root.Close() }()

	count := 0
	directories := []string{"."}
	for len(directories) > 0 {
		last := len(directories) - 1
		directory := directories[last]
		directories = directories[:last]
		entries, children, err := countDenyDirectory(ctx, root, directory, limit-count)
		if err != nil {
			return count, err
		}
		count += entries
		if count > limit {
			return count, nil
		}
		directories = append(directories, children...)
	}
	return count, nil
}

func countDenyDirectory(ctx context.Context, root *os.Root, directory string, limit int) (int, []string, error) {
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}
	handle, err := root.Open(directory)
	if errors.Is(err, fs.ErrPermission) {
		return 0, nil, nil
	}
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = handle.Close() }()

	count := 0
	var children []string
	for {
		if err := ctx.Err(); err != nil {
			return count, nil, err
		}
		entries, readErr := handle.ReadDir(denyDirectoryBatchSize)
		for _, entry := range entries {
			count++
			if count > limit {
				return count, nil, nil
			}
			if entry.IsDir() {
				children = append(children, filepath.Join(directory, entry.Name()))
			}
		}
		if errors.Is(readErr, io.EOF) || errors.Is(readErr, fs.ErrPermission) {
			return count, children, nil
		}
		if readErr != nil {
			return count, nil, readErr
		}
	}
}
