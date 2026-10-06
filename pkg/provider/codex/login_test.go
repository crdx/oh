package codex_test

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"crdx.org/oh/internal/auth"
	"crdx.org/oh/pkg/provider/codex"
)

func TestALoginExchangesTheRedirectedCodeAndNamesTheAccount(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	storedPath := codex.CredentialsPath()
	if err := os.MkdirAll(filepath.Dir(storedPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storedPath, []byte(`{"version":1,"codex":{"access":"old","refresh":"kept","account_id":"account","expires_at":1}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	exchanges := make(chan url.Values, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		exchanges <- request.PostForm
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer, `{"access_token":%q,"expires_in":3600}`, accessToken())
	}))
	t.Cleanup(server.Close)
	codex.TokenURL = server.URL
	t.Cleanup(func() { codex.TokenURL = "" })

	redirects := make(chan string, 1)
	var challenge string
	err := codex.LoginWithRedirect(t.Context(), func(address string) {
		parsed, err := url.Parse(address)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		challenge = query.Get("code_challenge")
		redirects <- "http://localhost:1455/auth/callback?code=granted&state=" + query.Get("state")
	}, redirects)
	if err != nil {
		t.Fatal(err)
	}

	exchange := <-exchanges
	if exchange.Get("grant_type") != "authorization_code" || exchange.Get("code") != "granted" {
		t.Errorf("exchanged %v, want the granted code", exchange)
	}
	digest := sha256.Sum256([]byte(exchange.Get("code_verifier")))
	if base64.RawURLEncoding.EncodeToString(digest[:]) != challenge {
		t.Error("the verifier sent does not answer the challenge presented")
	}

	stored, err := auth.Load(storedPath)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Codex.Access != accessToken() || stored.Codex.AccountID != "account" || stored.Codex.Refresh != "kept" {
		t.Errorf("stored %+v, want the fresh access naming its account beside the refresh token already held", *stored.Codex)
	}
	if stored.Codex.Scope != codex.Scope {
		t.Errorf("stored the scope %q, want the login to record %q", stored.Codex.Scope, codex.Scope)
	}
}

func TestALoginRefusesATokenNamingNoAccount(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"access_token":"not-a-jwt","expires_in":3600}`)
	}))
	t.Cleanup(server.Close)
	codex.TokenURL = server.URL
	t.Cleanup(func() { codex.TokenURL = "" })

	redirects := make(chan string, 1)
	err := codex.LoginWithRedirect(t.Context(), func(address string) {
		parsed, _ := url.Parse(address)
		redirects <- "http://localhost:1455/auth/callback?code=granted&state=" + parsed.Query().Get("state")
	}, redirects)
	if err == nil {
		t.Fatal("a token naming no account was accepted")
	}

	if _, err := os.Stat(codex.CredentialsPath()); !os.IsNotExist(err) {
		t.Errorf("credentials were stored after a refused login: %v", err)
	}
}

func TestALoginRefusesARedirectForAnotherState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	redirects := make(chan string, 1)
	err := codex.LoginWithRedirect(t.Context(), func(string) {
		redirects <- "http://localhost:1455/auth/callback?code=granted&state=forged"
	}, redirects)
	if err == nil {
		t.Fatal("a redirect carrying another state was accepted")
	}
}
