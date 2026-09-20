package anthropic

import (
	"errors"
	"os"
	"time"

	"crdx.org/io/internal/auth"
)

type Credentials = auth.AnthropicCredentials

func stale(credentials *Credentials) bool {
	return time.Now().Add(refreshWindow).UnixMilli() >= credentials.ExpiresAt
}

func inherit(childCredentials *Credentials, parentCredentials *Credentials) {
	if childCredentials.Refresh == "" {
		childCredentials.Refresh = parentCredentials.Refresh
	}
}

func CredentialsPath() string {
	return auth.Path()
}

func LoadStoredCredentials() (*Credentials, error) {
	return loadCredentials(CredentialsPath())
}

func loadCredentials(path string) (*Credentials, error) {
	storedCredentials, err := auth.Load(path)
	if errors.Is(err, os.ErrNotExist) || err == nil && storedCredentials.Anthropic == nil {
		return nil, errors.New("not logged in to Anthropic: run the login command with anthropic")
	}
	if err != nil {
		return nil, err
	}

	return storedCredentials.Anthropic, nil
}

func saveCredentials(path string, credentials *Credentials) error {
	return auth.Update(path, func(storedCredentials *auth.Credentials) error {
		if storedCredentials.Anthropic != nil {
			inherit(credentials, storedCredentials.Anthropic)
		}
		storedCredentials.Anthropic = credentials
		return nil
	})
}
