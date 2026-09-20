package codex

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"crdx.org/io/internal/auth"
)

func getCredentialStore(t *testing.T, path string) *credentialStore {
	t.Helper()

	tokenSource := StoredCredentialsAt(path)
	source, isCredentialStore := tokenSource.(*credentialStore)
	if !isCredentialStore {
		t.Fatalf("stored credentials returned %T", tokenSource)
	}
	return source
}

func TestAStaleProcessAdoptsCredentialsAnotherProcessRefreshed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	storedCredentials := &Credentials{
		Access:    "new-access",
		Refresh:   "new-refresh",
		ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
		AccountID: "new-account",
	}
	if err := auth.Save(path, &auth.Credentials{Version: auth.Version, Codex: storedCredentials}); err != nil {
		t.Fatal(err)
	}

	var refreshRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		refreshRequests.Add(1)
		http.Error(writer, "the current credentials should not be refreshed", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	TokenURL = server.URL
	t.Cleanup(func() { TokenURL = "" })

	source := getCredentialStore(t, path)
	source.credentials = &Credentials{
		Access:    "old-access",
		Refresh:   "old-refresh",
		ExpiresAt: time.Now().Add(-time.Minute).UnixMilli(),
		AccountID: "old-account",
	}

	token, err := source.Token()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token.Access != "new-access" || token.AccountID != "new-account" {
		t.Errorf("got token %+v, want the token another process stored", token)
	}
	if refreshRequests.Load() != 0 {
		t.Errorf("got %d refresh requests, want none", refreshRequests.Load())
	}
}

func TestARejectedRefreshAdoptsCredentialsWrittenByAnOlderProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	oldCredentials := &Credentials{
		Access:    "old-access",
		Refresh:   "old-refresh",
		ExpiresAt: time.Now().Add(-time.Minute).UnixMilli(),
		AccountID: "old-account",
	}
	if err := auth.Save(path, &auth.Credentials{
		Version:    auth.Version,
		Codex:      oldCredentials,
		Anthropic:  &auth.AnthropicCredentials{Access: "old-anthropic-access"},
		OpenCodeGo: &auth.OpenCodeGoCredentials{APIKey: "old-key"},
	}); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		rotatedCredentials := &Credentials{
			Access:    "new-access",
			Refresh:   "new-refresh",
			ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
			AccountID: "new-account",
		}
		if err := auth.Save(path, &auth.Credentials{
			Version:    auth.Version,
			Codex:      rotatedCredentials,
			Anthropic:  &auth.AnthropicCredentials{Access: "new-anthropic-access"},
			OpenCodeGo: &auth.OpenCodeGoCredentials{APIKey: "new-key"},
		}); err != nil {
			t.Errorf("rotate credentials: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(writer, `{"error":"invalid_grant"}`)
	}))
	t.Cleanup(server.Close)
	TokenURL = server.URL
	t.Cleanup(func() { TokenURL = "" })

	source := getCredentialStore(t, path)
	source.credentials = oldCredentials

	token, err := source.Token()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token.Access != "new-access" || token.AccountID != "new-account" {
		t.Errorf("got token %+v, want the token written during the rejected refresh", token)
	}
	storedCredentials, err := auth.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if storedCredentials.Anthropic == nil || storedCredentials.Anthropic.Access != "new-anthropic-access" {
		t.Errorf("credential adoption lost the Anthropic credentials: %+v", storedCredentials.Anthropic)
	}
	if storedCredentials.OpenCodeGo == nil || storedCredentials.OpenCodeGo.APIKey != "new-key" {
		t.Errorf("credential adoption lost the OpenCode Go credentials: %+v", storedCredentials.OpenCodeGo)
	}
}
