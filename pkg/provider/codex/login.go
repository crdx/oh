package codex

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"crdx.org/io/internal/browser"
	"crdx.org/io/internal/oauth"
	"crdx.org/io/internal/req"
)

const (
	authoriseURL = "https://auth.openai.com/oauth/authorize"
	tokenURL     = "https://auth.openai.com/oauth/token" //nolint:gosec // an address, not a credential
	clientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	callbackHost = "127.0.0.1:1455"
	redirectURL  = "http://localhost:1455/auth/callback"
	scope        = "openid profile email offline_access"
)

const chill = 5 * time.Minute

func Login(ctx context.Context) error {
	return LoginWithAddress(ctx, printAuthorisationAddress)
}

func LoginWithAddress(ctx context.Context, presentAddress func(string)) error {
	return LoginWithRedirect(ctx, presentAddress, nil)
}

func LoginWithRedirect(
	ctx context.Context,
	presentAddress func(string),
	redirects <-chan string,
) error {
	verifier := newToken()
	state := newToken()

	var config net.ListenConfig

	listener, err := config.Listen(ctx, "tcp", callbackHost)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", callbackHost, err)
	}

	address := authoriseAddress(verifier, state)
	presentAddress(address)

	code, err := waitForCallback(ctx, listener, state, redirects)
	if err != nil {
		return err
	}

	credentials, err := exchange(ctx, code, verifier)
	if err != nil {
		return err
	}

	return saveCredentials(CredentialsPath(), credentials)
}

func printAuthorisationAddress(address string) {
	fmt.Println("Visit this address to authorise:")
	fmt.Println()
	fmt.Println("  " + address)
	fmt.Println()
	if err := browser.Open(address); err != nil {
		fmt.Println("Could not open a browser:", err)
		fmt.Println("Visit the address above to continue.")
		fmt.Println()
	}
}

func authoriseAddress(verifier string, state string) string {
	digest := sha256.Sum256([]byte(verifier))

	query := url.Values{
		"response_type":              {"code"},
		"client_id":                  {clientID},
		"redirect_uri":               {redirectURL},
		"scope":                      {scope},
		"state":                      {state},
		"code_challenge":             {base64.RawURLEncoding.EncodeToString(digest[:])},
		"code_challenge_method":      {"S256"},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
		"originator":                 {Originator},
	}

	return authoriseURL + "?" + query.Encode()
}

func waitForCallback(
	ctx context.Context,
	listener net.Listener,
	state string,
	redirects <-chan string,
) (string, error) {
	type transmission struct {
		code string
		err  error
	}

	radio := make(chan transmission, 1)

	server := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			query := request.URL.Query()

			if query.Get("state") != state {
				http.Error(writer, "state did not match", http.StatusBadRequest)
				radio <- transmission{err: errors.New("the callback state did not match")}

				return
			}

			if code := query.Get("code"); code != "" {
				_, _ = fmt.Fprintln(writer, "Authorised. You can close this tab.")
				radio <- transmission{code: code}

				return
			}

			http.Error(writer, "no code", http.StatusBadRequest)
			radio <- transmission{err: errors.New("the callback carried no code")}
		}),
	}

	go func() { _ = server.Serve(listener) }()

	defer func() {
		shutdownContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()

		_ = server.Shutdown(shutdownContext)
	}()

	select {
	case transmission := <-radio:
		return transmission.code, transmission.err
	case redirect := <-redirects:
		return oauth.CodeFromRedirect(redirect, state)
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(chill):
		return "", errors.New("gave up waiting")
	}
}

func exchange(ctx context.Context, code string, verifier string) (*Credentials, error) {
	credentials, err := postForm(ctx, loginRequests, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {redirectURL},
		"code_verifier": {verifier},
	})
	if err != nil {
		return nil, fmt.Errorf("exchange the code: %w", err)
	}

	credentials.AccountID, err = accountID(credentials.Access)
	if err != nil {
		return nil, fmt.Errorf("exchange the code: %w", err)
	}

	return credentials, nil
}

func refreshToken(requests *req.Client, refresh string) (*Credentials, error) {
	credentials, err := postForm(context.Background(), requests, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {refresh},
		"scope":         {scope},
	})
	if err != nil {
		return nil, err
	}

	credentials.AccountID, _ = accountID(credentials.Access)

	return credentials, nil
}

const authTimeout = 30 * time.Second

var loginRequests = req.New(authTimeout)

func postForm(ctx context.Context, requests *req.Client, form url.Values) (*Credentials, error) {
	var payload struct {
		Access    string `json:"access_token"`
		Refresh   string `json:"refresh_token"`
		ExpiresIn int64  `json:"expires_in"`
	}

	if err := requests.Form(ctx, tokenEndpoint(), form, &payload); err != nil {
		return nil, err
	}

	if payload.Access == "" {
		return nil, errors.New("the token response carried no access token")
	}

	return &Credentials{
		Access:    payload.Access,
		Refresh:   payload.Refresh,
		ExpiresAt: time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second).UnixMilli(),
	}, nil
}

var TokenURL string

func tokenEndpoint() string {
	if TokenURL != "" {
		return TokenURL
	}

	return tokenURL
}

func accountID(access string) (string, error) {
	parts := strings.Split(access, ".")
	if len(parts) < 2 {
		return "", errors.New("the access token is not a JWT")
	}

	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode the access token: %w", err)
	}

	var claims struct {
		Auth struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}

	if err := json.Unmarshal(body, &claims); err != nil {
		return "", fmt.Errorf("read the access token claims: %w", err)
	}

	if claims.Auth.AccountID == "" {
		return "", errors.New("the access token names no ChatGPT account")
	}

	return claims.Auth.AccountID, nil
}

func newToken() string {
	buffer := make([]byte, 32)
	_, _ = rand.Read(buffer)

	return hex.EncodeToString(buffer)
}
