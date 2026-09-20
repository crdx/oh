package anthropic

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"crdx.org/io/internal/auth"
	"crdx.org/io/internal/req"
	"crdx.org/io/pkg/wire/anthropic/messages"
)

const refreshWindow = 5 * time.Minute

type TokenSource = messages.TokenSource

func Static(access string) TokenSource {
	return static{token: access}
}

type static struct {
	token string
}

func (self static) Token() (string, error) {
	return self.token, nil
}

func StoredCredentials() TokenSource {
	return &credentialStore{path: CredentialsPath(), requests: req.New(authTimeout)}
}

func StoredCredentialsAt(path string) TokenSource {
	return &credentialStore{path: path, requests: req.New(authTimeout)}
}

type credentialStore struct {
	path        string
	requests    *req.Client
	mutex       sync.Mutex
	credentials *Credentials
}

func (self *credentialStore) Token() (string, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.credentials == nil {
		credentials, err := loadCredentials(self.path)
		if err != nil {
			return "", err
		}

		self.credentials = credentials
	}

	if stale(self.credentials) {
		if err := self.refresh(); err != nil {
			return "", err
		}
	}

	return self.credentials.Access, nil
}

func (self *credentialStore) ObserveHTTP(observer req.Observer) {
	self.requests.Observe(observer)
}

func (self *credentialStore) refresh() error {
	var currentCredentials *Credentials
	wasRefreshed := false

	err := auth.Update(self.path, func(storedCredentials *auth.Credentials) error {
		currentCredentials = storedCredentials.Anthropic
		if currentCredentials == nil {
			return errors.New("not logged in to Anthropic: run the login command with anthropic")
		}
		if !stale(currentCredentials) {
			return nil
		}
		if currentCredentials.Refresh == "" {
			return errors.New("credentials have expired: run the login command again")
		}

		attemptedCredentials := *currentCredentials
		refreshedCredentials, err := refreshToken(self.requests, currentCredentials.Refresh)
		if err != nil {
			if req.IsRejected(err) {
				latestStoredCredentials, loadErr := auth.Load(self.path)
				if loadErr == nil && latestStoredCredentials.Anthropic != nil && *latestStoredCredentials.Anthropic != attemptedCredentials && !stale(latestStoredCredentials.Anthropic) {
					*storedCredentials = *latestStoredCredentials
					currentCredentials = latestStoredCredentials.Anthropic
					return nil
				}

				return fmt.Errorf("credentials were refused: run the login command again: %w", err)
			}

			return fmt.Errorf("refresh credentials: %w", err)
		}

		inherit(refreshedCredentials, currentCredentials)
		storedCredentials.Anthropic = refreshedCredentials
		currentCredentials = refreshedCredentials
		wasRefreshed = true
		return nil
	})
	if err != nil {
		if wasRefreshed {
			return fmt.Errorf("store the refreshed credentials: %w", err)
		}
		return err
	}

	self.credentials = currentCredentials
	return nil
}
