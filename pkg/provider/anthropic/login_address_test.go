package anthropic

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
		_, _ = fmt.Fprint(writer, `{"access_token":"presented","expires_in":3600}`)
	}))
	t.Cleanup(server.Close)
	TokenURL = server.URL
	t.Cleanup(func() { TokenURL = "" })

	callbackAddress := ListenOnAnyPort(t)
	callbacks := make(chan error, 1)
	err := LoginWithAddress(t.Context(), func(address string) {
		parsed, err := url.Parse(address)
		if err != nil {
			t.Error(err)
			return
		}

		callback := "http://" + callbackAddress() + callbackPath + "?code=granted&state=" + url.QueryEscape(parsed.Query().Get("state"))
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
	if stored.Anthropic == nil || stored.Anthropic.Access != "presented" {
		t.Errorf("stored %+v, want the access the callback earned", stored)
	}
}
