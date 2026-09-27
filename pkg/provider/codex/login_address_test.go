package codex

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"crdx.org/oh/internal/auth"
)

func TestALoginPresentsWhereToAuthoriseAndTakesTheCallback(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer, `{"access_token":%q,"expires_in":3600}`, presentedAccess)
	}))
	t.Cleanup(server.Close)
	TokenURL = server.URL
	t.Cleanup(func() { TokenURL = "" })

	callbacks := make(chan error, 1)
	err := LoginWithAddress(t.Context(), func(address string) {
		parsed, err := url.Parse(address)
		if err != nil {
			t.Error(err)
			return
		}

		callback := "http://" + callbackHost + "/auth/callback" + "?code=granted&state=" + url.QueryEscape(parsed.Query().Get("state"))
		go func() {
			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, callback, nil)
			if err != nil {
				callbacks <- err
				return
			}
			response, err := http.DefaultClient.Do(request)
			if err == nil {
				_ = response.Body.Close()
			}
			callbacks <- err
		}()
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-callbacks; err != nil {
		t.Fatal(err)
	}

	stored, err := auth.Load(CredentialsPath())
	if err != nil {
		t.Fatal(err)
	}
	if stored.Codex == nil || stored.Codex.Access != presentedAccess {
		t.Errorf("stored %+v, want the access the callback earned", stored)
	}
}

const presentedAccess = "header.eyJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9hY2NvdW50X2lkIjoiYWNjb3VudCJ9fQ.signature"
