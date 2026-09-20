package opencodego

import (
	"errors"
	"os"

	"crdx.org/io/internal/auth"
)

func CredentialsPath() string {
	return auth.Path()
}

func SaveKey(key string) error {
	return SaveKeyAt(CredentialsPath(), key)
}

func SaveKeyAt(path string, key string) error {
	return auth.SaveOpenCodeGoKey(path, key)
}

func StoredKey() (string, error) {
	return StoredKeyAt(CredentialsPath())
}

func StoredKeyAt(path string) (string, error) {
	credentials, err := auth.Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", errors.New("not logged in to OpenCode Go: run the login command with opencode-go")
	}
	if err != nil {
		return "", err
	}
	if credentials.OpenCodeGo == nil || credentials.OpenCodeGo.APIKey == "" {
		return "", errors.New("not logged in to OpenCode Go: run the login command with opencode-go")
	}

	return credentials.OpenCodeGo.APIKey, nil
}
