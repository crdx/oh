package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"crdx.org/oh/internal/format"
)

func Update[State any](path string, supportedFormat int, update func(*State) error) error {
	lock, err := lock(path, syscall.LOCK_EX)
	if err != nil {
		return err
	}
	defer release(lock)

	return updateHeld(path, supportedFormat, update)
}

func TryUpdate[State any](path string, supportedFormat int, update func(*State) error) (bool, error) {
	lock, err := lock(path, syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer release(lock)

	return true, updateHeld(path, supportedFormat, update)
}

func Read[State any](path string, supportedFormat int, state *State) error {
	return read(path, supportedFormat, state)
}

func updateHeld[State any](path string, supportedFormat int, update func(*State) error) error {
	var state State
	if err := read(path, supportedFormat, &state); err != nil {
		return err
	}
	if err := update(&state); err != nil {
		return err
	}

	return write(path, state)
}

func release(lock *os.File) {
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	_ = lock.Close()
}

func lock(path string, how int) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}

	file, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // the path is ours
	if err != nil {
		return nil, fmt.Errorf("open state lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), how); err != nil {
		_ = file.Close()

		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, err
		}

		return nil, fmt.Errorf("lock state: %w", err)
	}

	return file, nil
}

func read(path string, supportedFormat int, state any) error {
	data, err := os.ReadFile(path) //nolint:gosec // the path is ours
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read state: %w", err)
	}

	storedFormat, err := format.ReadJSON(data)
	if err != nil {
		return fmt.Errorf("read state: %w", err)
	}
	if err := format.Check(storedFormat, supportedFormat); err != nil {
		return fmt.Errorf("read state %s: %w", path, err)
	}

	if err := json.Unmarshal(data, state); err != nil {
		return fmt.Errorf("read state: %w", err)
	}

	return nil
}

func write(path string, state any) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	pendingFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	defer func() { _ = os.Remove(pendingFile.Name()) }()

	if _, err := pendingFile.Write(data); err != nil {
		_ = pendingFile.Close()
		return fmt.Errorf("write state: %w", err)
	}
	if err := pendingFile.Close(); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	if err := os.Rename(pendingFile.Name(), path); err != nil {
		return fmt.Errorf("write state: %w", err)
	}

	return nil
}
