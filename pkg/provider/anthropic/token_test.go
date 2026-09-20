package anthropic_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/auth"
	"crdx.org/oh/pkg/provider/anthropic"
)

func writeAnthropicCredentials(t *testing.T, expiresIn time.Duration) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "auth.json")

	credentials := fmt.Sprintf(
		`{"version":1,"anthropic":{"access":"old","refresh":"refresh-me","expires_at":%d}}`,
		time.Now().Add(expiresIn).UnixMilli(),
	)

	if err := os.WriteFile(path, []byte(credentials), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return path
}

func anthropicTokenEndpoint(t *testing.T, status int, body string) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(status)
			_, _ = fmt.Fprint(writer, body)
		},
	))

	t.Cleanup(server.Close)

	anthropic.TokenURL = server.URL
	t.Cleanup(func() { anthropic.TokenURL = "" })
}

func TestStoredRefreshesATokenNearExpiry(t *testing.T) {
	anthropicTokenEndpoint(t, http.StatusOK, `{"access_token":"new","refresh_token":"next","expires_in":3600}`)

	path := writeAnthropicCredentials(t, -time.Minute)

	token, err := anthropic.StoredCredentialsAt(path).Token()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "new" {
		t.Errorf("expected the refreshed token, got %q", token)
	}

	stored, err := auth.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stored.Anthropic == nil || stored.Anthropic.Access != "new" || stored.Anthropic.Refresh != "next" {
		t.Errorf("expected the refreshed pair to be written back, got %+v", stored.Anthropic)
	}
}

func TestARefusedRefreshSaysToLogInAgain(t *testing.T) {
	anthropicTokenEndpoint(
		t,
		http.StatusBadRequest,
		`{"error":"invalid_grant","error_description":"Refresh token not found"}`,
	)

	path := writeAnthropicCredentials(t, -time.Minute)

	_, err := anthropic.StoredCredentialsAt(path).Token()
	if err == nil {
		t.Fatal("expected the refusal to be reported")
	}

	if !strings.Contains(err.Error(), "run the login command again") {
		t.Errorf("expected the way out to be named, got %v", err)
	}
}

func TestAFailedRefreshIsNotMistakenForARefusal(t *testing.T) {
	anthropicTokenEndpoint(t, http.StatusInternalServerError, `{"error":"server_error"}`)

	path := writeAnthropicCredentials(t, -time.Minute)

	_, err := anthropic.StoredCredentialsAt(path).Token()
	if err == nil {
		t.Fatal("expected the failure to be reported")
	}

	if strings.Contains(err.Error(), "run the login command again") {
		t.Errorf("expected a passing failure not to send the user to the login command, got %v", err)
	}
}
