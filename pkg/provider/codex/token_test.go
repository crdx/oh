package codex_test

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/auth"
	"crdx.org/oh/pkg/provider/codex"
)

func accessToken() string {
	const claims = `{"https://api.openai.com/auth":{"chatgpt_account_id":"account"}}`

	return "header." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".signature"
}

func writeCredentials(t *testing.T, expiresIn time.Duration) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "auth.json")

	credentials := fmt.Sprintf(
		`{"version":1,"codex":{"access":"old","refresh":"refresh-me","account_id":"account","expires_at":%d},"opencode-go":{"api_key":"open-code-key"}}`,
		time.Now().Add(expiresIn).UnixMilli(),
	)

	if err := os.WriteFile(path, []byte(credentials), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return path
}

func tokenEndpoint(t *testing.T, grants *[]string) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			if err := request.ParseForm(); err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			*grants = append(*grants, request.PostForm.Get("grant_type"))

			writer.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(writer, `{"access_token":%q,"refresh_token":"next","expires_in":3600}`, accessToken())
		},
	))

	t.Cleanup(server.Close)

	codex.TokenURL = server.URL
	t.Cleanup(func() { codex.TokenURL = "" })
}

func TestStoredRefreshesATokenNearExpiry(t *testing.T) {
	var grants []string

	tokenEndpoint(t, &grants)
	path := writeCredentials(t, time.Minute)

	token, err := codex.StoredCredentialsAt(path).Token()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if token.Access != accessToken() {
		t.Errorf("expected the refreshed token, got %q", token.Access)
	}

	if token.AccountID != "account" {
		t.Errorf("expected the account to survive the refresh, got %q", token.AccountID)
	}

	if len(grants) != 1 || grants[0] != "refresh_token" {
		t.Errorf("expected one refresh_token grant, got %v", grants)
	}

	stored, err := auth.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stored.Codex == nil || stored.Codex.Access != accessToken() || stored.Codex.Refresh != "next" {
		t.Errorf("expected the refreshed pair to be written back, got %+v", stored.Codex)
	}
	if stored.OpenCodeGo == nil || stored.OpenCodeGo.APIKey != "open-code-key" {
		t.Errorf("expected the OpenCode key to survive, got %+v", stored.OpenCodeGo)
	}
}

func TestARefusedRefreshSaysToLogInAgain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(writer, `{"error":"invalid_grant","error_description":"Refresh token not found"}`)
		},
	))

	t.Cleanup(server.Close)

	codex.TokenURL = server.URL
	t.Cleanup(func() { codex.TokenURL = "" })

	path := writeCredentials(t, -time.Minute)

	_, err := codex.StoredCredentialsAt(path).Token()
	if err == nil {
		t.Fatal("expected the refusal to be reported")
	}

	if !strings.Contains(err.Error(), "run the login command again") {
		t.Errorf("expected the way out to be named, got %v", err)
	}
}

func TestAFailedRefreshIsNotMistakenForARefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(writer, `{"error":"server_error"}`)
		},
	))

	t.Cleanup(server.Close)

	codex.TokenURL = server.URL
	t.Cleanup(func() { codex.TokenURL = "" })

	path := writeCredentials(t, -time.Minute)

	_, err := codex.StoredCredentialsAt(path).Token()
	if err == nil {
		t.Fatal("expected the failure to be reported")
	}

	if strings.Contains(err.Error(), "run the login command again") {
		t.Errorf("expected a passing failure not to send the user to the login command, got %v", err)
	}
}

func TestStoredKeepsTheRefreshTokenTheResponseOmits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(writer, `{"access_token":%q,"expires_in":3600}`, accessToken())
		},
	))

	t.Cleanup(server.Close)

	codex.TokenURL = server.URL
	t.Cleanup(func() { codex.TokenURL = "" })

	path := writeCredentials(t, time.Minute)

	if _, err := codex.StoredCredentialsAt(path).Token(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, err := auth.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stored.Codex == nil || stored.Codex.Refresh != "refresh-me" {
		t.Errorf("expected the held refresh token to survive, got %+v", stored.Codex)
	}
}

func TestStoredKeepsTheAccountARefreshedTokenNoLongerNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(writer,
				`{"access_token":"not.a.jwt","refresh_token":"next","expires_in":3600}`)
		},
	))

	t.Cleanup(server.Close)

	codex.TokenURL = server.URL
	t.Cleanup(func() { codex.TokenURL = "" })

	path := writeCredentials(t, time.Minute)

	token, err := codex.StoredCredentialsAt(path).Token()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if token.AccountID != "account" {
		t.Errorf("expected the account the login stored to stand, got %q", token.AccountID)
	}

	stored, err := auth.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stored.Codex == nil || stored.Codex.AccountID != "account" {
		t.Errorf("expected the account to be written back too, got %+v", stored.Codex)
	}
}

func TestStoredLeavesAGoodTokenAlone(t *testing.T) {
	var grants []string

	tokenEndpoint(t, &grants)
	path := writeCredentials(t, time.Hour)

	token, err := codex.StoredCredentialsAt(path).Token()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if token.Access != "old" {
		t.Errorf("expected the stored token, got %q", token.Access)
	}

	if len(grants) != 0 {
		t.Errorf("expected no refresh, got %v", grants)
	}
}

func TestStoredReportsMissingCredentials(t *testing.T) {
	source := codex.StoredCredentialsAt(filepath.Join(t.TempDir(), "absent.json"))

	if _, err := source.Token(); err == nil {
		t.Error("expected an error when there are no credentials")
	}
}
