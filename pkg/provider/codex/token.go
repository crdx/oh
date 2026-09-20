package codex

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"crdx.org/oh/internal/auth"
	"crdx.org/oh/internal/req"
	"crdx.org/oh/pkg/wire/openai/responses"
)

const refreshWindow = 5 * time.Minute

type Token = responses.Token

type TokenSource = responses.TokenSource

func Static(access string, accountID string) TokenSource {
	return static{token: Token{Access: access, AccountID: accountID}}
}

type static struct {
	token Token
}

func (self static) Token() (Token, error) {
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

func (self *credentialStore) Token() (Token, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.credentials == nil {
		credentials, err := loadCredentials(self.path)
		if err != nil {
			return Token{}, err
		}

		self.credentials = credentials
	}

	if stale(self.credentials) {
		if err := self.refresh(); err != nil {
			return Token{}, err
		}
	}

	return Token{
		Access:    self.credentials.Access,
		AccountID: self.credentials.AccountID,
	}, nil
}

func (self *credentialStore) ObserveHTTP(observer req.Observer) {
	self.requests.Observe(observer)
}

func (self *credentialStore) refresh() error {
	var currentCredentials *Credentials
	wasRefreshed := false

	err := auth.Update(self.path, func(storedCredentials *auth.Credentials) error {
		currentCredentials = storedCredentials.Codex
		if currentCredentials == nil {
			return errors.New("not logged in to ChatGPT: run the login command with codex")
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
				if loadErr == nil && latestStoredCredentials.Codex != nil && *latestStoredCredentials.Codex != attemptedCredentials && !stale(latestStoredCredentials.Codex) {
					*storedCredentials = *latestStoredCredentials
					currentCredentials = latestStoredCredentials.Codex
					return nil
				}

				return fmt.Errorf("credentials were refused: run the login command again: %w", err)
			}

			return fmt.Errorf("refresh credentials: %w", err)
		}

		inherit(refreshedCredentials, currentCredentials)
		storedCredentials.Codex = refreshedCredentials
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
