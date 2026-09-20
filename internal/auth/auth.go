package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"crdx.org/oh/internal/format"
	"crdx.org/oh/internal/xdg"
)

const Version = 1

var ErrUnsupportedVersion = errors.New("credentials are in a format this build does not read: run the login command again")

func Unusable(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, ErrUnsupportedVersion)
}

type Credentials struct {
	Version    int                    `json:"version"`
	Codex      *CodexCredentials      `json:"codex,omitempty"`
	OpenCodeGo *OpenCodeGoCredentials `json:"opencode-go,omitempty"`
	Anthropic  *AnthropicCredentials  `json:"anthropic,omitempty"`
}

type CodexCredentials struct {
	Access    string `json:"access"`
	Refresh   string `json:"refresh"`
	ExpiresAt int64  `json:"expires_at"`
	AccountID string `json:"account_id"`
}

type AnthropicCredentials struct {
	Access    string `json:"access"`
	Refresh   string `json:"refresh"`
	ExpiresAt int64  `json:"expires_at"`
}

type OpenCodeGoCredentials struct {
	APIKey string `json:"api_key"`
}

func Path() string {
	return xdg.StatePath("org.crdx", "io", "auth.json")
}

func Load(path string) (*Credentials, error) {
	if path == "" {
		return nil, errors.New("could not determine where credentials live")
	}

	data, err := os.ReadFile(path) //nolint:gosec // the path is selected by the caller
	if err != nil {
		return nil, fmt.Errorf("read credentials: %w", err)
	}

	storedVersion, err := format.ReadJSON(data)
	if err != nil {
		return nil, fmt.Errorf("parse credentials %s: %w", path, err)
	}
	if storedVersion != Version {
		return nil, fmt.Errorf("%s: format %d: %w", path, storedVersion, ErrUnsupportedVersion)
	}

	var credentials Credentials
	if err := json.Unmarshal(data, &credentials); err != nil {
		return nil, fmt.Errorf("parse credentials %s: %w", path, err)
	}

	return &credentials, nil
}

func Save(path string, credentials *Credentials) error {
	if path == "" {
		return errors.New("could not determine where credentials live")
	}

	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	credentials.Version = Version
	data, err := json.Marshal(credentials)
	if err != nil {
		return err
	}

	pendingFile, err := os.CreateTemp(directory, filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}

	defer func() { _ = os.Remove(pendingFile.Name()) }()

	if _, err := pendingFile.Write(data); err != nil {
		_ = pendingFile.Close()

		return fmt.Errorf("write credentials: %w", err)
	}

	if err := pendingFile.Close(); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}

	if err := os.Rename(pendingFile.Name(), path); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}

	return nil
}

func Update(path string, update func(*Credentials) error) error {
	lock, err := lock(path)
	if err != nil {
		return err
	}
	defer unlock(lock)

	credentials, err := Load(path)
	if Unusable(err) {
		credentials = &Credentials{Version: Version}
	} else if err != nil {
		return err
	}

	if err := update(credentials); err != nil {
		return err
	}

	return Save(path, credentials)
}

func lock(path string) (*os.File, error) {
	if path == "" {
		return nil, errors.New("could not determine where credentials live")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}

	file, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // the path is selected by the caller
	if err != nil {
		return nil, fmt.Errorf("open credentials lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock credentials: %w", err)
	}

	return file, nil
}

func unlock(lock *os.File) {
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	_ = lock.Close()
}

func SaveOpenCodeGoKey(path string, key string) error {
	return Update(path, func(credentials *Credentials) error {
		credentials.OpenCodeGo = &OpenCodeGoCredentials{APIKey: key}
		return nil
	})
}
