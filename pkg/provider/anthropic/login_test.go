package anthropic_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/auth"
	"crdx.org/oh/pkg/provider/anthropic"
)

type loginExchange struct {
	GrantType    string `json:"grant_type"`
	Code         string `json:"code"`
	State        string `json:"state"`
	RedirectURI  string `json:"redirect_uri"`
	CodeVerifier string `json:"code_verifier"`
}

func TestALoginExchangesTheRedirectedCodeAndKeepsTheStoredRefreshToken(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	storedPath := anthropic.CredentialsPath()
	if err := os.MkdirAll(filepath.Dir(storedPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storedPath, []byte(`{"version":1,"anthropic":{"access":"old","refresh":"kept","expires_at":1}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	exchanges := make(chan loginExchange, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body loginExchange
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		exchanges <- body
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"access_token":"fresh","expires_in":3600}`)
	}))
	t.Cleanup(server.Close)
	anthropic.TokenURL = server.URL
	t.Cleanup(func() { anthropic.TokenURL = "" })

	anthropic.ListenOnAnyPort(t)
	redirects := make(chan string, 1)
	var challenge string
	err := anthropic.LoginWithRedirect(t.Context(), func(address string) {
		parsed, err := url.Parse(address)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		challenge = query.Get("code_challenge")
		if query.Get("code_challenge_method") != "S256" {
			t.Errorf("got challenge method %q, want S256", query.Get("code_challenge_method"))
		}
		redirects <- "http://localhost:53692/callback?code=granted&state=" + query.Get("state")
	}, redirects)
	if err != nil {
		t.Fatal(err)
	}

	exchange := <-exchanges
	if exchange.GrantType != "authorization_code" || exchange.Code != "granted" {
		t.Errorf("exchanged %+v, want the granted code", exchange)
	}
	digest := sha256.Sum256([]byte(exchange.CodeVerifier))
	if base64.RawURLEncoding.EncodeToString(digest[:]) != challenge {
		t.Error("the verifier sent does not answer the challenge presented")
	}

	stored, err := auth.Load(storedPath)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Anthropic.Access != "fresh" || stored.Anthropic.Refresh != "kept" {
		t.Errorf("stored %+v, want the fresh access beside the refresh token already held", *stored.Anthropic)
	}
}

func TestALoginRefusesARedirectForAnotherState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	anthropic.ListenOnAnyPort(t)
	redirects := make(chan string, 1)
	err := anthropic.LoginWithRedirect(t.Context(), func(string) {
		redirects <- "http://localhost:53692/callback?code=granted&state=forged"
	}, redirects)
	if err == nil || !strings.Contains(err.Error(), "state did not match") {
		t.Fatalf("got %v, want a redirect carrying another state refused", err)
	}

	if _, err := os.Stat(anthropic.CredentialsPath()); !os.IsNotExist(err) {
		t.Errorf("credentials were stored after a refused login: %v", err)
	}
}

func TestALoginWithoutAnAccessTokenStoresNothing(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	anthropicTokenEndpoint(t, http.StatusOK, `{"refresh_token":"only"}`)

	anthropic.ListenOnAnyPort(t)
	redirects := make(chan string, 1)
	err := anthropic.LoginWithRedirect(t.Context(), func(address string) {
		parsed, _ := url.Parse(address)
		redirects <- "http://localhost:53692/callback?code=granted&state=" + parsed.Query().Get("state")
	}, redirects)
	if err == nil || !strings.Contains(err.Error(), "carried no access token") {
		t.Fatalf("got %v, want a token response without an access token refused", err)
	}

	if _, err := os.Stat(anthropic.CredentialsPath()); !os.IsNotExist(err) {
		t.Errorf("credentials were stored after a failed exchange: %v", err)
	}
}
