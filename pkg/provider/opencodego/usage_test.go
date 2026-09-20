package opencodego_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/req"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/provider/opencodego"
)

var (
	_ agent.UsageReporter = (*opencodego.Client)(nil)
	_ agent.UsageProber   = (*opencodego.Client)(nil)
)

func usageServer(t *testing.T, code int, payload string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(code)
			_, _ = fmt.Fprint(writer, payload)
		},
	))

	t.Cleanup(server.Close)

	return server
}

func TestAClientWithNoUsageAddressReportsNothing(t *testing.T) {
	client := newClient(t, "http://127.0.0.1:1/v1/chat/completions")

	if client.IsAvailable() {
		t.Fatal("expected usage reporting to be unavailable without a usage address")
	}

	windows, err := client.UsageWindows(t.Context())
	if err != nil || windows != nil {
		t.Errorf("expected no windows and no error, got %v and %v", windows, err)
	}
}

func TestARealUsageLimitRefusalNamesItsWindowAndReset(t *testing.T) {
	const wait = 165899 * time.Second

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain;charset=UTF-8")
		writer.Header().Set("Retry-After", "165899")
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(writer, `{"type":"error","error":{"type":"GoUsageLimitError","message":"Monthly usage limit reached. Resets in 1 day."},"metadata":{"workspace":"workspace-1","limitName":"monthly"}}`)
	}))
	t.Cleanup(server.Close)

	client := newClient(t, server.URL)
	client.AddUserMessage("hello")
	before := time.Now()
	_, err := client.Send(t.Context(), func(agent.Output) bool { return true })

	var limit *agent.UsageLimitError
	if !errors.As(err, &limit) {
		t.Fatalf("expected a usage limit, got %v", err)
	}
	if len(limit.Windows) != 1 {
		t.Fatalf("got windows %+v", limit.Windows)
	}
	window := limit.Windows[0]
	if window.Duration != 30*24*time.Hour || window.Percent != 100 || !window.IsLimited {
		t.Errorf("got window %+v", window)
	}
	if window.ResetsAt.Before(before.Add(wait)) || window.ResetsAt.After(time.Now().Add(wait)) {
		t.Errorf("got reset %s", window.ResetsAt)
	}
	var refusal *req.StatusError
	if !errors.As(err, &refusal) || refusal.Code != "GoUsageLimitError" {
		t.Errorf("the provider refusal was not preserved: %v", err)
	}
}

func TestTheThreeGoWindowsAreReportedInOrder(t *testing.T) {
	server := usageServer(t, http.StatusOK, `{
		"usage": {
			"rolling": {"status": "ok", "percent": 39, "resetsAt": "2026-08-17T12:30:33.430Z"},
			"weekly": {"status": "ok", "percent": 15, "resetsAt": "2026-08-24T00:00:00.430Z"},
			"monthly": {"status": "rate-limited", "percent": 100, "resetsAt": "2026-09-01T04:14:25.430Z"}
		}
	}`)

	client := newClient(t, "http://127.0.0.1:1/v1/chat/completions")
	client.UsageURL = server.URL

	if !client.IsAvailable() {
		t.Fatal("expected usage reporting once a usage address is held")
	}

	windows, err := client.UsageWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	want := []agent.UsageWindow{
		{Duration: 5 * time.Hour, Percent: 39},
		{Duration: 7 * 24 * time.Hour, Percent: 15},
		{Duration: 30 * 24 * time.Hour, Percent: 100, IsLimited: true},
	}

	if len(windows) != len(want) {
		t.Fatalf("expected %d windows, got %v", len(want), windows)
	}

	for at, window := range want {
		got := windows[at]

		if got.Duration != window.Duration || got.Percent != window.Percent {
			t.Errorf("window %d = %+v, want %+v", at, got, window)
		}

		if got.IsLimited != window.IsLimited {
			t.Errorf("window %d limited = %t, want %t", at, got.IsLimited, window.IsLimited)
		}

		if got.ResetsAt.IsZero() {
			t.Errorf("window %d carries no reset", at)
		}
	}
}

func TestAWindowTheEndpointLeftOutIsNotReported(t *testing.T) {
	server := usageServer(t, http.StatusOK, `{
		"usage": {
			"rolling": {"status": "ok", "percent": 4, "resetsAt": "2026-08-17T12:30:33.430Z"}
		}
	}`)

	client := newClient(t, "http://127.0.0.1:1/v1/chat/completions")
	client.UsageURL = server.URL

	windows, err := client.UsageWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(windows) != 1 || windows[0].Duration != 5*time.Hour {
		t.Errorf("expected the rolling window alone, got %v", windows)
	}
}

func TestARefusedUsageRequestCarriesItsStatusCode(t *testing.T) {
	server := usageServer(t, http.StatusUnauthorized, `{"error": {"message": "invalid key"}}`)

	client := newClient(t, "http://127.0.0.1:1/v1/chat/completions")
	client.UsageURL = server.URL

	_, err := client.UsageWindows(t.Context())

	var refused *req.StatusError
	if !errors.As(err, &refused) {
		t.Fatalf("expected a status error, got %v", err)
	}

	if refused.Status != http.StatusUnauthorized || refused.Message != "invalid key" {
		t.Errorf("got %+v", refused)
	}
}

func TestEveryShapeOfUsagePayloadIsReadTheSameWay(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload string
		want    []agent.UsageWindow
	}{
		{
			name:    "nothing at all",
			payload: `{}`,
			want:    nil,
		},
		{
			name:    "an envelope holding no windows",
			payload: `{"usage": {}}`,
			want:    nil,
		},
		{
			name:    "a window the endpoint is refusing work against",
			payload: `{"usage": {"rolling": {"status": "rate-limited", "percent": 100}}}`,
			want: []agent.UsageWindow{
				{Duration: 5 * time.Hour, Percent: 100, IsLimited: true},
			},
		},
		{
			name:    "a reset the endpoint garbled",
			payload: `{"usage": {"weekly": {"status": "ok", "percent": 15, "resetsAt": "soon"}}}`,
			want: []agent.UsageWindow{
				{Duration: 7 * 24 * time.Hour, Percent: 15},
			},
		},
		{
			name:    "a fractional percentage",
			payload: `{"usage": {"monthly": {"status": "ok", "percent": 13.5}}}`,
			want: []agent.UsageWindow{
				{Duration: 30 * 24 * time.Hour, Percent: 13.5},
			},
		},
		{
			name:    "a window carrying no status",
			payload: `{"usage": {"rolling": {"percent": 4}}}`,
			want:    nil,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := usageServer(t, http.StatusOK, test.payload)

			client := newClient(t, "http://127.0.0.1:1/v1/chat/completions")
			client.UsageURL = server.URL

			got, err := client.UsageWindows(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			if !slices.Equal(got, test.want) {
				t.Errorf("got %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestAnUnreadableUsagePayloadIsReportedRatherThanGuessedAt(t *testing.T) {
	server := usageServer(t, http.StatusOK, `{"usage": `)

	client := newClient(t, "http://127.0.0.1:1/v1/chat/completions")
	client.UsageURL = server.URL

	if _, err := client.UsageWindows(t.Context()); err == nil {
		t.Error("expected the truncated payload reported")
	}
}

func TestASubscriptionTheKeyDoesNotHoldIsReported(t *testing.T) {
	server := usageServer(t, http.StatusForbidden, `{"error": {"message": "no go subscription"}}`)

	client := newClient(t, "http://127.0.0.1:1/v1/chat/completions")
	client.UsageURL = server.URL

	_, err := client.UsageWindows(t.Context())

	refused, ok := errors.AsType[*req.StatusError](err)
	if !ok {
		t.Fatalf("expected a status error, got %v", err)
	}

	if refused.Status != http.StatusForbidden {
		t.Errorf("got %+v", refused)
	}
}

func TestTheUsageRequestIsAuthorisedAndRecorded(t *testing.T) {
	var authorisation string
	var accepted string

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			authorisation = request.Header.Get("Authorization")
			accepted = request.Header.Get("Accept")

			writer.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(writer, `{"usage":{"rolling":{"status":"ok","percent":4}}}`)
		},
	))

	t.Cleanup(server.Close)

	client := newClient(t, "http://127.0.0.1:1/v1/chat/completions")
	client.UsageURL = server.URL

	observer := &countingObserver{}
	client.ObserveHTTP(observer)

	if _, err := client.UsageWindows(t.Context()); err != nil {
		t.Fatal(err)
	}

	if authorisation == "" || !strings.HasPrefix(authorisation, "Bearer ") {
		t.Errorf("expected the request authorised, got %q", authorisation)
	}

	if accepted != "application/json" {
		t.Errorf("expected a JSON answer asked for, got %q", accepted)
	}

	if observer.requests != 1 {
		t.Errorf("expected the usage poll recorded on the wire, observed %d", observer.requests)
	}
}

type countingObserver struct {
	requests int
}

func (self *countingObserver) Start(req.Request) req.ExchangeObserver {
	self.requests++

	return nil
}
