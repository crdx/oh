package codex

import (
	"errors"
	"os"
	"time"

	"crdx.org/oh/internal/auth"
)

type Credentials = auth.CodexCredentials

var ErrOutdatedLogin = errors.New("the Codex login is invalid for this build: run oh --login codex")

func stale(credentials *Credentials) bool {
	return time.Now().Add(refreshWindow).UnixMilli() >= credentials.ExpiresAt
}

func inherit(childCredentials *Credentials, parentCredentials *Credentials) {
	if childCredentials.Refresh == "" {
		childCredentials.Refresh = parentCredentials.Refresh
	}

	if childCredentials.AccountID == "" {
		childCredentials.AccountID = parentCredentials.AccountID
	}

	if childCredentials.Scope == "" {
		childCredentials.Scope = parentCredentials.Scope
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
	if errors.Is(err, os.ErrNotExist) || err == nil && storedCredentials.Codex == nil {
		return nil, errors.New("not logged in to ChatGPT: run the login command with codex")
	}
	if err != nil {
		return nil, err
	}
	if storedCredentials.Codex.Scope != Scope {
		return nil, ErrOutdatedLogin
	}

	return storedCredentials.Codex, nil
}

func saveCredentials(path string, credentials *Credentials) error {
	return auth.Update(path, func(storedCredentials *auth.Credentials) error {
		if storedCredentials.Codex != nil {
			inherit(credentials, storedCredentials.Codex)
		}
		storedCredentials.Codex = credentials
		return nil
	})
}
