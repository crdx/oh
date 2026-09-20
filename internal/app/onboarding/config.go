package onboarding

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/BurntSushi/toml"

	"crdx.org/oh/internal/app/config"
)

func setInitialModel(path string, selection string) (bool, error) {
	lock, err := lockConfig(path)
	if err != nil {
		return false, err
	}
	defer unlockConfig(lock)

	settings, err := config.Load(path)
	if err != nil {
		return false, err
	}
	if len(settings.Model.RoundRobin) > 0 {
		return false, nil
	}

	contents, err := os.ReadFile(path) //nolint:gosec // the configured path is ours
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}

	updatedContents := addInitialModel(contents, selection)
	if err := writeConfig(path, updatedContents); err != nil {
		return false, err
	}

	return true, nil
}

func addInitialModel(contents []byte, selection string) []byte {
	setting := "round_robin = [" + strconv.Quote(selection) + "]\n"
	if len(contents) == 0 {
		return fmt.Appendf(nil, "version = %d\n\n[model]\n%s", config.Format, setting)
	}

	text := string(contents)

	separator := "\n"
	if strings.HasSuffix(text, "\n") {
		separator = ""
	}
	if candidate := text + separator + "\n[model]\n" + setting; isSelectionRead(candidate, selection) {
		return []byte(candidate)
	}

	lines := strings.SplitAfter(text, "\n")
	for i := range lines {
		above := strings.Join(lines[:i+1], "")
		if !strings.HasSuffix(above, "\n") {
			above += "\n"
		}
		if candidate := above + setting + strings.Join(lines[i+1:], ""); isSelectionRead(candidate, selection) {
			return []byte(candidate)
		}
	}

	return contents
}

func isSelectionRead(candidate string, selection string) bool {
	var document struct {
		Model struct {
			RoundRobin []string `toml:"round_robin"`
		} `toml:"model"`
	}

	if _, err := toml.Decode(candidate, &document); err != nil {
		return false
	}

	return slices.Contains(document.Model.RoundRobin, selection)
}

func lockConfig(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create config directory: %w", err)
	}

	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // the path is ours
	if err != nil {
		return nil, fmt.Errorf("open config lock: %w", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("lock config: %w", err)
	}

	return lock, nil
}

func unlockConfig(lock *os.File) {
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	_ = lock.Close()
}

func writeConfig(path string, contents []byte) error {
	pendingFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	defer func() { _ = os.Remove(pendingFile.Name()) }()

	if _, err := pendingFile.Write(contents); err != nil {
		_ = pendingFile.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := pendingFile.Close(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(pendingFile.Name(), path); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}
