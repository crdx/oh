package req_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/req"
	"crdx.org/oh/pkg/agent"
)

func refusingServer(t *testing.T, status int, body string) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(status)
			_, _ = writer.Write([]byte(body))
		},
	))

	t.Cleanup(server.Close)

	return server.URL
}

func TestFailureCarriesTheEndpointsOwnMessage(t *testing.T) {
	url := refusingServer(t, http.StatusTooManyRequests, `{"error":{"message":"slow down"}}`)

	_, _, err := req.New(time.Second).Stream(t.Context(), url, map[string]string{}, nil)
	if err == nil {
		t.Fatal("expected the refusal to be reported")
	}

	if err.Error() != "slow down" {
		t.Errorf("expected the endpoint's own message, got %q", err)
	}
}

func TestANumericCodeStillYieldsTheEndpointsOwnMessage(t *testing.T) {
	url := refusingServer(t, http.StatusBadRequest, `{"error":{"code":400,`+
		`"message":"request (264481 tokens) exceeds the available context size (262144 tokens)",`+
		`"type":"exceed_context_size_error","n_prompt_tokens":264481,"n_ctx":262144}}`)

	_, _, err := req.New(time.Second).Stream(t.Context(), url, map[string]string{}, nil)
	if err == nil {
		t.Fatal("expected the refusal to be reported")
	}

	failure := agent.FailureFrom(err)
	if failure == nil ||
		failure.Message != "request (264481 tokens) exceeds the available context size (262144 tokens)" ||
		failure.Code != "exceed_context_size_error" || failure.Body != "" {
		t.Errorf("expected the numeric code to be read past, got %+v", failure)
	}
}

func TestAQuotedCodeIsPreferredOverTheType(t *testing.T) {
	url := refusingServer(t, http.StatusTooManyRequests,
		`{"error":{"code":"rate_limit_exceeded","message":"slow down","type":"requests"}}`)

	_, _, err := req.New(time.Second).Stream(t.Context(), url, map[string]string{}, nil)
	if err == nil {
		t.Fatal("expected the refusal to be reported")
	}

	failure := agent.FailureFrom(err)
	if failure == nil || failure.Code != "rate_limit_exceeded" {
		t.Errorf("expected the quoted code, got %+v", failure)
	}
}

func TestFailureFallsBackToTheStatus(t *testing.T) {
	url := refusingServer(t, http.StatusBadGateway, "the gateway is unwell")

	_, _, err := req.New(time.Second).Stream(t.Context(), url, map[string]string{}, nil)
	if err == nil {
		t.Fatal("expected the refusal to be reported")
	}

	if !strings.Contains(err.Error(), "502") ||
		!strings.Contains(err.Error(), "the gateway is unwell") {
		t.Errorf("expected the status and the body, got %q", err)
	}

	failure := agent.FailureFrom(err)
	if failure == nil || failure.HTTPStatus != 502 || failure.Body != "the gateway is unwell" {
		t.Errorf("expected semantic details for the journal, got %+v", failure)
	}
}

func TestAnHTMLFailureDoesNotDumpItsPage(t *testing.T) {
	body := "<html>private diagnostics</html>"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/html; charset=UTF-8")
		writer.WriteHeader(520)
		_, _ = writer.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	_, _, err := req.New(time.Second).Stream(t.Context(), server.URL, map[string]string{}, nil)
	if err == nil {
		t.Fatal("expected the refusal to be reported")
	}

	if err.Error() != "request failed with status 520" {
		t.Errorf("expected only the status, got %q", err)
	}

	var refused *req.StatusError
	if !errors.As(err, &refused) || refused.Body != body {
		t.Errorf("expected the HTTP client to retain the response for provider logic, got %+v", refused)
	}

	failure := agent.FailureFrom(err)
	if failure == nil || failure.HTTPStatus != 520 || failure.MediaType != "text/html" || failure.Body != "" {
		t.Errorf("expected safe semantic details for the journal, got %+v", failure)
	}
}

func TestAnUnknownHTMLStatusIsStillDescribedSafely(t *testing.T) {
	url := refusingServer(t, 598, "<!doctype html><html>future diagnostics</html>")

	_, _, err := req.New(time.Second).Stream(t.Context(), url, map[string]string{}, nil)
	if err == nil {
		t.Fatal("expected the refusal to be reported")
	}

	failure := agent.FailureFrom(err)
	if failure == nil || failure.Kind != agent.HTTPStatusFailure ||
		failure.HTTPStatus != 598 || failure.MediaType != "text/html" || failure.Body != "" {
		t.Errorf("expected safe semantic details for the unknown status, got %+v", failure)
	}
}

func TestFormPostsAndDecodes(t *testing.T) {
	var sent string

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			if err := request.ParseForm(); err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			sent = request.PostForm.Get("grant_type")

			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"access_token":"new"}`))
		},
	))

	t.Cleanup(server.Close)

	var response struct {
		Access string `json:"access_token"`
	}

	form := map[string][]string{"grant_type": {"refresh_token"}}

	if err := req.New(time.Second).Form(t.Context(), server.URL, form, &response); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sent != "refresh_token" {
		t.Errorf("expected the form to arrive, got %q", sent)
	}

	if response.Access != "new" {
		t.Errorf("expected the answer to be read, got %q", response.Access)
	}
}

func TestARefusalKeepsWhatItWasAsWellAsWhatItSaid(t *testing.T) {
	body := `{"error":{"message":"slow down","code":"rate_limit_exceeded"}}`
	url := refusingServer(t, http.StatusTooManyRequests, body)

	_, _, err := req.New(time.Second).Stream(t.Context(), url, map[string]string{}, nil)

	var refused *req.StatusError
	if !errors.As(err, &refused) {
		t.Fatalf("expected a refusal the caller can read, got %v", err)
	}

	switch {
	case refused.Status != http.StatusTooManyRequests:
		t.Errorf("expected the status to be kept, got %d", refused.Status)
	case refused.Code != "rate_limit_exceeded":
		t.Errorf("expected the endpoint's own code, got %q", refused.Code)
	case refused.Message != "slow down":
		t.Errorf("expected what it said, got %q", refused.Message)
	case refused.Body != body:
		t.Errorf("expected the complete refusal body to be kept, got %q", refused.Body)
	case !refused.Retriable():
		t.Error("expected a rate limit to be worth asking again after")
	}
}

func TestARefusalKeepsTheEndpointsErrorTypeWhenItHasNoCode(t *testing.T) {
	url := refusingServer(
		t,
		http.StatusTooManyRequests,
		`{"error":{"message":"usage is gone","type":"usage_limit_reached"}}`,
	)

	_, _, err := req.New(time.Second).Stream(t.Context(), url, map[string]string{}, nil)

	var refused *req.StatusError
	if !errors.As(err, &refused) {
		t.Fatalf("expected a refusal the caller can read, got %v", err)
	}
	if refused.Code != "usage_limit_reached" {
		t.Errorf("expected the endpoint's own type, got %q", refused.Code)
	}
}

func TestARefusalReturnsItsResponseHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("X-Usage-Reset", "later")
		writer.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	_, header, err := req.New(time.Second).Stream(t.Context(), server.URL, map[string]string{}, nil)
	if err == nil {
		t.Fatal("expected the refusal to be reported")
	}
	if got := header.Get("X-Usage-Reset"); got != "later" {
		t.Errorf("got response header %q", got)
	}
}

func TestCacheLifetimeReadsTheServersMaximumAge(t *testing.T) {
	for _, test := range []struct {
		header string
		want   time.Duration
	}{
		{header: "private, max-age=120", want: 2 * time.Minute},
		{header: `max-age="30"`, want: 30 * time.Second},
		{header: "no-cache"},
		{header: "max-age=not-a-number"},
	} {
		header := http.Header{"Cache-Control": {test.header}}
		if got := req.CacheLifetime(header); got != test.want {
			t.Errorf("header %q gave %s, want %s", test.header, got, test.want)
		}
	}
}

func TestAConnectionThatWasNeverMadeIsWorthAskingAgainAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := server.URL
	server.Close()

	_, _, err := req.New(time.Second).Stream(t.Context(), address, map[string]string{}, nil)
	if err == nil {
		t.Fatal("expected the dial to fail")
	}

	var retriable interface{ Retriable() bool }
	if !errors.As(err, &retriable) || !retriable.Retriable() {
		t.Errorf("expected a connection that was never made to be worth another attempt, got %v", err)
	}
}

func TestACancelledRequestIsNotWorthAskingAgainAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := req.New(time.Second).Stream(ctx, server.URL, map[string]string{}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected the cancellation to be kept, got %v", err)
	}

	var retriable interface{ Retriable() bool }
	if errors.As(err, &retriable) && retriable.Retriable() {
		t.Error("expected a cancelled request to be left alone")
	}
}

func TestARefusalSaysWhetherAskingAgainIsWorthIt(t *testing.T) {
	tests := map[int]bool{
		http.StatusBadRequest:                    false,
		http.StatusUnauthorized:                  false,
		http.StatusNotFound:                      false,
		http.StatusTooManyRequests:               true,
		http.StatusInternalServerError:           true,
		http.StatusBadGateway:                    true,
		http.StatusServiceUnavailable:            true,
		http.StatusGatewayTimeout:                true,
		http.StatusInsufficientStorage:           true,
		http.StatusNotImplemented:                false,
		http.StatusHTTPVersionNotSupported:       false,
		http.StatusVariantAlsoNegotiates:         false,
		http.StatusLoopDetected:                  false,
		http.StatusNotExtended:                   false,
		http.StatusNetworkAuthenticationRequired: false,
		520:                                      true,
		529:                                      true,
		598:                                      true,
	}

	for status, worthIt := range tests {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			url := refusingServer(t, status, `{"error":{"message":"no"}}`)

			_, _, err := req.New(time.Second).Stream(t.Context(), url, map[string]string{}, nil)

			var refused *req.StatusError
			if !errors.As(err, &refused) {
				t.Fatalf("expected a refusal the caller can read, got %v", err)
			}

			if refused.Retriable() != worthIt {
				t.Errorf("expected retriable %t for %d", worthIt, status)
			}
		})
	}
}

func TestARefusalCarriesHowLongItAskedToBeLeftAloneFor(t *testing.T) {
	tests := map[string]time.Duration{
		"7":                             7 * time.Second,
		" 7 ":                           7 * time.Second,
		"":                              0,
		"whenever":                      0,
		"Mon, 02 Jan 2006 15:04:05 GMT": 0,
	}

	for header, want := range tests {
		t.Run(header, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(
				func(writer http.ResponseWriter, _ *http.Request) {
					if header != "" {
						writer.Header().Set("Retry-After", header)
					}
					writer.WriteHeader(http.StatusTooManyRequests)
					_, _ = writer.Write([]byte(`{"error":{"message":"slow down"}}`))
				}))

			t.Cleanup(server.Close)

			_, _, err := req.New(time.Second).Stream(t.Context(), server.URL, map[string]string{}, nil)

			var refused *req.StatusError
			if !errors.As(err, &refused) {
				t.Fatalf("expected a refusal the caller can read, got %v", err)
			}

			if refused.RetryAfter() != want {
				t.Errorf("expected a wait of %s, got %s", want, refused.RetryAfter())
			}
		})
	}
}

func TestARefusalReadsAWaitGivenAsADate(t *testing.T) {
	asked := time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Retry-After", asked)
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = writer.Write([]byte(`{"error":{"message":"back shortly"}}`))
		}))

	t.Cleanup(server.Close)

	_, _, err := req.New(time.Second).Stream(t.Context(), server.URL, map[string]string{}, nil)

	var refused *req.StatusError
	if !errors.As(err, &refused) {
		t.Fatalf("expected a refusal the caller can read, got %v", err)
	}

	if wait := refused.RetryAfter(); wait <= 0 || wait > 30*time.Second {
		t.Errorf("expected a wait of about thirty seconds, got %s", wait)
	}
}
