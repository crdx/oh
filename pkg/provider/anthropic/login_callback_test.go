package anthropic

import (
	"fmt"
	"net"
	"net/http"
	"testing"
)

func TestWaitForCallbackAcceptsTheLoopbackCallback(t *testing.T) {
	var listenConfig net.ListenConfig

	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	result := make(chan struct {
		code string
		err  error
	}, 1)
	go func() {
		code, err := waitForCallback(t.Context(), listener, "expected", nil)
		result <- struct {
			code string
			err  error
		}{code: code, err: err}
	}()

	address := fmt.Sprintf("http://%s/callback?code=accepted&state=expected", listener.Addr())

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, address, nil)
	if err != nil {
		t.Fatal(err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()

	got := <-result
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.code != "accepted" {
		t.Errorf("got code %q", got.code)
	}
}

func TestWaitForCallbackValidatesTheStateOfAnError(t *testing.T) {
	for _, test := range []struct {
		query string
		want  string
	}{
		{query: "error=denied", want: "the callback state did not match"},
		{query: "error=denied&state=wrong", want: "the callback state did not match"},
		{query: "error=denied&state=expected", want: "authorisation failed: denied"},
	} {
		var listenConfig net.ListenConfig

		listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}

		result := make(chan error, 1)
		go func() {
			_, err := waitForCallback(t.Context(), listener, "expected", nil)
			result <- err
		}()

		address := fmt.Sprintf("http://%s/callback?%s", listener.Addr(), test.query)
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, address, nil)
		if err != nil {
			t.Fatal(err)
		}

		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()

		if err := <-result; err == nil || err.Error() != test.want {
			t.Errorf("%s: got %v, want %q", test.query, err, test.want)
		}
	}
}

func TestWaitForCallbackAcceptsAPastedRedirect(t *testing.T) {
	var listenConfig net.ListenConfig

	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	redirects := make(chan string, 1)
	redirects <- "http://localhost:53692/callback?code=accepted&state=expected"

	code, err := waitForCallback(t.Context(), listener, "expected", redirects)
	if err != nil {
		t.Fatal(err)
	}
	if code != "accepted" {
		t.Errorf("got code %q", code)
	}
}
