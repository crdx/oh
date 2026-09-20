package anthropic

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
	}
	if err := auth.Save(path, &auth.Credentials{Version: auth.Version, Anthropic: storedCredentials}); err != nil {
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
	}

	access, err := source.Token()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if access != "new-access" {
		t.Errorf("got access token %q, want the token another process stored", access)
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
	}
	if err := auth.Save(path, &auth.Credentials{
		Version:    auth.Version,
		Anthropic:  oldCredentials,
		Codex:      &auth.CodexCredentials{Access: "old-codex-access"},
		OpenCodeGo: &auth.OpenCodeGoCredentials{APIKey: "old-key"},
	}); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		rotatedCredentials := &Credentials{
			Access:    "new-access",
			Refresh:   "new-refresh",
			ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
		}
		if err := auth.Save(path, &auth.Credentials{
			Version:    auth.Version,
			Anthropic:  rotatedCredentials,
			Codex:      &auth.CodexCredentials{Access: "new-codex-access"},
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

	access, err := source.Token()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if access != "new-access" {
		t.Errorf("got access token %q, want the token written during the rejected refresh", access)
	}
	storedCredentials, err := auth.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if storedCredentials.Codex == nil || storedCredentials.Codex.Access != "new-codex-access" {
		t.Errorf("credential adoption lost the Codex credentials: %+v", storedCredentials.Codex)
	}
	if storedCredentials.OpenCodeGo == nil || storedCredentials.OpenCodeGo.APIKey != "new-key" {
		t.Errorf("credential adoption lost the OpenCode Go credentials: %+v", storedCredentials.OpenCodeGo)
	}
}
