package anthropic

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"crdx.org/io/internal/browser"
	"crdx.org/io/internal/oauth"
	"crdx.org/io/internal/req"
)

const (
	authoriseURL = "https://claude.ai/oauth/authorize"
	tokenURL     = "https://platform.claude.com/v1/oauth/token" //nolint:gosec // an address, not a credential
	clientID     = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	callbackHost = "127.0.0.1:53692"
	callbackPath = "/callback"
	redirectURL  = "http://localhost:53692/callback"

	scope = "org:create_api_key user:profile user:inference " +
		"user:sessions:claude_code user:mcp_servers user:file_upload"
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
	token := newToken()

	var config net.ListenConfig

	listener, err := config.Listen(ctx, "tcp", callbackHost)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", callbackHost, err)
	}

	address := authoriseAddress(token)
	presentAddress(address)

	code, err := waitForCallback(ctx, listener, token, redirects)
	if err != nil {
		return err
	}

	credentials, err := exchange(ctx, code, token)
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

func authoriseAddress(token string) string {
	digest := sha256.Sum256([]byte(token))

	query := url.Values{
		"code":                  {"true"},
		"client_id":             {clientID},
		"response_type":         {"code"},
		"redirect_uri":          {redirectURL},
		"scope":                 {scope},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(digest[:])},
		"code_challenge_method": {"S256"},
		"state":                 {token},
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
			if request.URL.Path != callbackPath {
				http.Error(writer, "not found", http.StatusNotFound)

				return
			}

			query := request.URL.Query()

			if query.Get("state") != state {
				http.Error(writer, "state did not match", http.StatusBadRequest)
				radio <- transmission{err: errors.New("the callback state did not match")}

				return
			}

			if failure := query.Get("error"); failure != "" {
				http.Error(writer, failure, http.StatusBadRequest)
				radio <- transmission{err: errors.New("authorisation failed: " + failure)}

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

type tokenRequest struct {
	GrantType    string `json:"grant_type"`
	ClientID     string `json:"client_id"`
	Code         string `json:"code,omitempty"`
	State        string `json:"state,omitempty"`
	RedirectURI  string `json:"redirect_uri,omitempty"`
	CodeVerifier string `json:"code_verifier,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

func exchange(ctx context.Context, code string, verifier string) (*Credentials, error) {
	credentials, err := postToken(ctx, loginRequests, tokenRequest{
		GrantType:    "authorization_code",
		ClientID:     clientID,
		Code:         code,
		State:        verifier,
		RedirectURI:  redirectURL,
		CodeVerifier: verifier,
	})
	if err != nil {
		return nil, fmt.Errorf("exchange the code: %w", err)
	}

	return credentials, nil
}

func refreshToken(requests *req.Client, refresh string) (*Credentials, error) {
	return postToken(context.Background(), requests, tokenRequest{
		GrantType:    "refresh_token",
		ClientID:     clientID,
		RefreshToken: refresh,
	})
}

const authTimeout = 30 * time.Second

var loginRequests = req.New(authTimeout)

func postToken(ctx context.Context, requests *req.Client, body tokenRequest) (*Credentials, error) {
	var payload struct {
		Access    string `json:"access_token"`
		Refresh   string `json:"refresh_token"`
		ExpiresIn int64  `json:"expires_in"`
	}

	if err := requests.JSON(ctx, tokenEndpoint(), body, &payload); err != nil {
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

func newToken() string {
	buffer := make([]byte, 32)
	_, _ = rand.Read(buffer)

	return hex.EncodeToString(buffer)
}
