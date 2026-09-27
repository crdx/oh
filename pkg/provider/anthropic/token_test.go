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
	"crdx.org/oh/internal/req"
	"crdx.org/oh/pkg/provider/anthropic"
)

func writeExpiredAnthropicCredentials(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "auth.json")

	credentials := fmt.Sprintf(
		`{"version":1,"anthropic":{"access":"old","refresh":"refresh-me","expires_at":%d}}`,
		time.Now().Add(-time.Minute).UnixMilli(),
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

	path := writeExpiredAnthropicCredentials(t)

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

	path := writeExpiredAnthropicCredentials(t)

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

	path := writeExpiredAnthropicCredentials(t)

	_, err := anthropic.StoredCredentialsAt(path).Token()
	if err == nil {
		t.Fatal("expected the failure to be reported")
	}

	if strings.Contains(err.Error(), "run the login command again") {
		t.Errorf("expected a passing failure not to send the user to the login command, got %v", err)
	}
}

type exchangeRecorder struct {
	addresses *[]string
}

func (self exchangeRecorder) Start(request req.Request) req.ExchangeObserver {
	*self.addresses = append(*self.addresses, request.Method+" "+request.URL)
	return self
}

func (exchangeRecorder) Response(req.Response)         {}
func (exchangeRecorder) Body(time.Time, []byte)        {}
func (exchangeRecorder) Finish(time.Time, error, bool) {}

func TestARefreshIsSeenByWhoeverObservesTheTraffic(t *testing.T) {
	anthropicTokenEndpoint(t, http.StatusOK, `{"access_token":"new","refresh_token":"next","expires_in":3600}`)

	source := anthropic.StoredCredentialsAt(writeExpiredAnthropicCredentials(t))
	observable, isObservable := source.(interface{ ObserveHTTP(observer req.Observer) })
	if !isObservable {
		t.Fatal("stored credentials cannot be observed")
	}

	var addresses []string
	observable.ObserveHTTP(exchangeRecorder{addresses: &addresses})

	if _, err := source.Token(); err != nil {
		t.Fatal(err)
	}

	if len(addresses) != 1 || addresses[0] != "POST "+anthropic.TokenURL {
		t.Errorf("observed %q, want the one refresh", addresses)
	}
}
