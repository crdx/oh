package codex_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/provider/codex"
)

var (
	_ agent.UsageReporter = (*codex.Client)(nil)
	_ agent.UsageProber   = (*codex.Client)(nil)
)

func newUsageClient(t *testing.T, url string) *codex.Client {
	t.Helper()

	client, err := codex.New(codex.Static("token", "account"), "gpt-5.6-sol", "high")
	if err != nil {
		t.Fatal(err)
	}
	client.URL = url

	return client
}

func rateLimitedTurn(t *testing.T, header map[string]string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) {
			for name, value := range header {
				writer.Header().Set(name, value)
			}

			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(writer, events(message("Hello."), completed))
		},
	))

	t.Cleanup(server.Close)

	return server
}

const (
	weekResets  = 1788277246
	sparkResets = 1787705193
)

func at(seconds int64) time.Time {
	return time.Unix(seconds, 0).UTC()
}

func theAccountsOwnHeaders() map[string]string {
	return map[string]string{
		"X-Codex-Plan-Type":                          "prolite",
		"X-Codex-Active-Limit":                       "premium",
		"X-Codex-Credits-Balance":                    "0",
		"X-Codex-Primary-Used-Percent":               "6",
		"X-Codex-Primary-Window-Minutes":             "10080",
		"X-Codex-Primary-Reset-At":                   "1788277246",
		"X-Codex-Primary-Reset-After-Seconds":        "590060",
		"X-Codex-Secondary-Used-Percent":             "0",
		"X-Codex-Secondary-Window-Minutes":           "0",
		"X-Codex-Secondary-Reset-At":                 "",
		"X-Codex-Secondary-Reset-After-Seconds":      "0",
		"X-Codex-Bengalfox-Limit-Name":               "GPT-5.3-Codex-Spark",
		"X-Codex-Bengalfox-Primary-Used-Percent":     "4",
		"X-Codex-Bengalfox-Primary-Window-Minutes":   "300",
		"X-Codex-Bengalfox-Primary-Reset-At":         "1787705193",
		"X-Codex-Bengalfox-Secondary-Used-Percent":   "2",
		"X-Codex-Bengalfox-Secondary-Window-Minutes": "10080",
		"X-Codex-Bengalfox-Secondary-Reset-At":       "1788291993",
	}
}

func reportedWindows(t *testing.T, header map[string]string) []agent.UsageWindow {
	t.Helper()

	server := rateLimitedTurn(t, header)
	client := newUsageClient(t, server.URL)
	client.AddUserMessage("hello")

	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatal(err)
	}

	windows, err := client.UsageWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	return windows
}

func TestTheAccountUsageProbeReadsPermissionAndEveryWindow(t *testing.T) {
	var askedPath string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		askedPath = request.URL.Path
		if got := request.Header.Get("Authorization"); got != "Bearer token" {
			t.Errorf("got authorisation %q", got)
		}
		if got := request.Header.Get("Chatgpt-Account-Id"); got != "account" {
			t.Errorf("got account %q", got)
		}
		writer.Header().Set("Cache-Control", "private, max-age=120")
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{
			"rate_limit": {
				"allowed": true,
				"primary_window": {"used_percent": 42, "limit_window_seconds": 18000, "reset_at": 1788277246}
			},
			"additional_rate_limits": [{
				"limit_name": "codex_other",
				"normal_model_slug": "gpt-5.3-codex-spark",
				"rate_limit": {
					"allowed": false,
					"primary_window": {"used_percent": 100, "limit_window_seconds": 604800, "reset_at": 1788291993}
				}
			}]
		}`)
	}))
	t.Cleanup(server.Close)

	client := newUsageClient(t, server.URL+"/backend-api/codex/responses")
	probe, err := client.ProbeUsage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if askedPath != "/backend-api/wham/usage" {
		t.Errorf("got path %q", askedPath)
	}
	if probe.Availability != agent.UsageAvailabilityAllowed || probe.RefreshAfter != 2*time.Minute {
		t.Errorf("got probe %+v", probe)
	}
	if len(probe.Windows) != 2 || probe.Windows[0].IsLimited || !probe.Windows[1].IsLimited || probe.Windows[1].Scope != "gpt-5.3-codex-spark" {
		t.Errorf("got windows %+v", probe.Windows)
	}
}

func TestEveryShapeOfRateLimitHeaderIsReadTheSameWay(t *testing.T) {
	for _, test := range []struct {
		name   string
		header map[string]string
		want   []agent.UsageWindow
	}{
		{
			name:   "the account's own response",
			header: theAccountsOwnHeaders(),
			want: []agent.UsageWindow{
				{Duration: 7 * 24 * time.Hour, Percent: 6, ResetsAt: at(weekResets)},
				{
					Duration: 5 * time.Hour,
					Percent:  4,
					ResetsAt: at(sparkResets),
					Scope:    "gpt-5.3-codex-spark",
				},
				{
					Duration: 7 * 24 * time.Hour,
					Percent:  2,
					ResetsAt: at(1788291993),
					Scope:    "gpt-5.3-codex-spark",
				},
			},
		},
		{
			name: "the active model repeated in the root headers",
			header: map[string]string{
				"X-Codex-Active-Limit":                       "codex_bengalfox",
				"X-Codex-Primary-Used-Percent":               "4",
				"X-Codex-Primary-Window-Minutes":             "300",
				"X-Codex-Primary-Reset-At":                   "1787705193",
				"X-Codex-Secondary-Used-Percent":             "2",
				"X-Codex-Secondary-Window-Minutes":           "10080",
				"X-Codex-Secondary-Reset-At":                 "1788291993",
				"X-Codex-Bengalfox-Limit-Name":               "GPT-5.3-Codex-Spark",
				"X-Codex-Bengalfox-Primary-Used-Percent":     "4",
				"X-Codex-Bengalfox-Primary-Window-Minutes":   "300",
				"X-Codex-Bengalfox-Primary-Reset-At":         "1787705193",
				"X-Codex-Bengalfox-Secondary-Used-Percent":   "2",
				"X-Codex-Bengalfox-Secondary-Window-Minutes": "10080",
				"X-Codex-Bengalfox-Secondary-Reset-At":       "1788291993",
			},
			want: []agent.UsageWindow{
				{
					Duration: 5 * time.Hour,
					Percent:  4,
					ResetsAt: at(sparkResets),
					Scope:    "gpt-5.3-codex-spark",
				},
				{
					Duration: 7 * 24 * time.Hour,
					Percent:  2,
					ResetsAt: at(1788291993),
					Scope:    "gpt-5.3-codex-spark",
				},
			},
		},
		{
			name: "a plan whose primary window is the short one",
			header: map[string]string{
				"X-Codex-Primary-Used-Percent":     "37.5",
				"X-Codex-Primary-Window-Minutes":   "300",
				"X-Codex-Primary-Reset-At":         "1788277246",
				"X-Codex-Secondary-Used-Percent":   "12",
				"X-Codex-Secondary-Window-Minutes": "10080",
				"X-Codex-Secondary-Reset-At":       "1788291993",
			},
			want: []agent.UsageWindow{
				{Duration: 5 * time.Hour, Percent: 37.5, ResetsAt: at(weekResets)},
				{Duration: 7 * 24 * time.Hour, Percent: 12, ResetsAt: at(1788291993)},
			},
		},
		{
			name: "a bucket that names no model",
			header: map[string]string{
				"X-Codex-Primary-Used-Percent":           "6",
				"X-Codex-Primary-Window-Minutes":         "10080",
				"X-Codex-Someday-Primary-Used-Percent":   "8",
				"X-Codex-Someday-Primary-Window-Minutes": "300",
			},
			want: []agent.UsageWindow{
				{Duration: 7 * 24 * time.Hour, Percent: 6},
				{Duration: 5 * time.Hour, Percent: 8, Scope: "someday"},
			},
		},
		{
			name: "buckets are read in a settled order whatever their name",
			header: map[string]string{
				"X-Codex-Primary-Used-Percent":            "6",
				"X-Codex-Primary-Window-Minutes":          "10080",
				"X-Codex-Zebra-Primary-Used-Percent":      "1",
				"X-Codex-Zebra-Primary-Window-Minutes":    "300",
				"X-Codex-Aardvark-Primary-Used-Percent":   "2",
				"X-Codex-Aardvark-Primary-Window-Minutes": "300",
			},
			want: []agent.UsageWindow{
				{Duration: 7 * 24 * time.Hour, Percent: 6},
				{Duration: 5 * time.Hour, Percent: 2, Scope: "aardvark"},
				{Duration: 5 * time.Hour, Percent: 1, Scope: "zebra"},
			},
		},
		{
			name: "a window the server zeroed",
			header: map[string]string{
				"X-Codex-Primary-Used-Percent":     "6",
				"X-Codex-Primary-Window-Minutes":   "300",
				"X-Codex-Primary-Reset-At":         "1788277246",
				"X-Codex-Secondary-Used-Percent":   "0",
				"X-Codex-Secondary-Window-Minutes": "0",
			},
			want: []agent.UsageWindow{
				{Duration: 5 * time.Hour, Percent: 6, ResetsAt: at(weekResets)},
			},
		},
		{
			name: "a window carrying no duration",
			header: map[string]string{
				"X-Codex-Primary-Used-Percent": "6",
				"X-Codex-Primary-Reset-At":     "1788277246",
			},
			want: nil,
		},
		{
			name: "a bucket carrying no percentage",
			header: map[string]string{
				"X-Codex-Primary-Used-Percent":           "6",
				"X-Codex-Primary-Window-Minutes":         "300",
				"X-Codex-Someday-Primary-Window-Minutes": "300",
				"X-Codex-Someday-Limit-Name":             "Some Model",
			},
			want: []agent.UsageWindow{
				{Duration: 5 * time.Hour, Percent: 6},
			},
		},
		{
			name: "figures the server garbled",
			header: map[string]string{
				"X-Codex-Primary-Used-Percent":   "six",
				"X-Codex-Primary-Window-Minutes": "later",
				"X-Codex-Primary-Reset-At":       "soon",
			},
			want: nil,
		},
		{
			name: "a reset the server garbled",
			header: map[string]string{
				"X-Codex-Primary-Used-Percent":        "6",
				"X-Codex-Primary-Window-Minutes":      "300",
				"X-Codex-Primary-Reset-At":            "soon",
				"X-Codex-Primary-Reset-After-Seconds": "later",
			},
			want: []agent.UsageWindow{
				{Duration: 5 * time.Hour, Percent: 6},
			},
		},
		{
			name: "a negative window",
			header: map[string]string{
				"X-Codex-Primary-Used-Percent":   "6",
				"X-Codex-Primary-Window-Minutes": "-300",
			},
			want: nil,
		},
		{
			name: "a bucket seen only in its secondary window",
			header: map[string]string{
				"X-Codex-Primary-Used-Percent":             "6",
				"X-Codex-Primary-Window-Minutes":           "300",
				"X-Codex-Someday-Secondary-Used-Percent":   "8",
				"X-Codex-Someday-Secondary-Window-Minutes": "300",
			},
			want: []agent.UsageWindow{
				{Duration: 5 * time.Hour, Percent: 6},
			},
		},
		{
			name: "a limit name the server padded",
			header: map[string]string{
				"X-Codex-Someday-Primary-Used-Percent":   "8",
				"X-Codex-Someday-Primary-Window-Minutes": "300",
				"X-Codex-Someday-Limit-Name":             "  GPT-5.3-Codex-Spark  ",
			},
			want: []agent.UsageWindow{
				{Duration: 5 * time.Hour, Percent: 8, Scope: "gpt-5.3-codex-spark"},
			},
		},
		{
			name: "a window in minutes of its own",
			header: map[string]string{
				"X-Codex-Primary-Used-Percent":   "6",
				"X-Codex-Primary-Window-Minutes": "90",
			},
			want: []agent.UsageWindow{
				{Duration: 90 * time.Minute, Percent: 6},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := reportedWindows(t, test.header)

			if !slices.Equal(got, test.want) {
				t.Errorf("got %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestARelativeResetIsReadWhereNoAbsoluteOneArrived(t *testing.T) {
	before := time.Now()
	windows := reportedWindows(t, map[string]string{
		"X-Codex-Primary-Used-Percent":        "6",
		"X-Codex-Primary-Window-Minutes":      "300",
		"X-Codex-Primary-Reset-After-Seconds": "3600",
	})

	if len(windows) != 1 {
		t.Fatalf("expected the one window, got %+v", windows)
	}

	if windows[0].ResetsAt.Before(before.Add(time.Hour)) ||
		windows[0].ResetsAt.After(time.Now().Add(time.Hour)) {
		t.Errorf("expected a reset about an hour out, got %s", windows[0].ResetsAt)
	}
}

func TestAnExplicitUsageLimitIsReportedWithTheActiveWindow(t *testing.T) {
	header := theAccountsOwnHeaders()
	header["X-Codex-Primary-Used-Percent"] = "100"

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		for name, value := range header {
			writer.Header().Set(name, value)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(writer, `{"error":{"type":"usage_limit_reached","message":"usage is gone"}}`)
	}))
	t.Cleanup(server.Close)

	client := newUsageClient(t, server.URL)
	client.AddUserMessage("hello")
	_, err := client.Send(t.Context(), func(agent.Output) bool { return true })

	var limit *agent.UsageLimitError
	if !errors.As(err, &limit) {
		t.Fatalf("expected a usage limit, got %v", err)
	}
	if limit.Retriable() {
		t.Error("expected an exhausted allowance not to be retried")
	}
	if len(limit.Windows) != 3 || !limit.Windows[0].IsLimited {
		t.Fatalf("got windows %+v", limit.Windows)
	}
	for _, window := range limit.Windows[1:] {
		if window.IsLimited {
			t.Errorf("unrelated window was marked limited: %+v", window)
		}
	}
}

func TestAFullWindowOnASuccessfulResponseRemainsAvailable(t *testing.T) {
	header := theAccountsOwnHeaders()
	header["X-Codex-Primary-Used-Percent"] = "100"

	windows := reportedWindows(t, header)
	if len(windows) == 0 || windows[0].Percent != 100 || windows[0].IsLimited {
		t.Errorf("got windows %+v", windows)
	}
}

func TestAResponseWithoutUsageHeadersKeepsReportingUnavailable(t *testing.T) {
	server := rateLimitedTurn(t, nil)
	client := newUsageClient(t, server.URL)
	client.AddUserMessage("hello")

	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatal(err)
	}

	if client.IsAvailable() {
		t.Fatal("expected usage reporting to remain unavailable without rate-limit headers")
	}
}

func TestAResponseWithoutUsageHeadersLeavesTheLastSnapshotStanding(t *testing.T) {
	first := rateLimitedTurn(t, map[string]string{
		"X-Codex-Primary-Used-Percent":   "50",
		"X-Codex-Primary-Window-Minutes": "300",
	})

	client := newUsageClient(t, first.URL)
	client.AddUserMessage("hello")

	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatal(err)
	}

	bare := rateLimitedTurn(t, nil)
	client.URL = bare.URL
	client.AddUserMessage("again")

	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatal(err)
	}

	windows, err := client.UsageWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(windows) != 1 || windows[0].Percent != 50 {
		t.Errorf("expected the first snapshot kept, got %v", windows)
	}
}

func TestThereAreNoUsageWindowsWhereNoAccountUsageAddressExists(t *testing.T) {
	client := newUsageClient(t, "http://127.0.0.1:1/nowhere")

	if client.IsAvailable() {
		t.Fatal("expected usage reporting to be unavailable without an account usage address")
	}

	windows, err := client.UsageWindows(t.Context())
	if err != nil || windows != nil {
		t.Errorf("expected no windows and no error, got %v and %v", windows, err)
	}
}

func TestTheAccountUsageIsReadBeforeTheFirstTurn(t *testing.T) {
	var askedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		askedPaths = append(askedPaths, request.URL.Path)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{
			"rate_limit": {
				"allowed": true,
				"primary_window": {"used_percent": 42, "limit_window_seconds": 18000, "reset_at": 1788277246}
			}
		}`)
	}))
	t.Cleanup(server.Close)

	client := newUsageClient(t, server.URL+"/backend-api/codex/responses")

	if !client.IsAvailable() {
		t.Fatal("expected usage reporting to be available before any turn")
	}

	windows, err := client.UsageWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(windows) != 1 || windows[0].Percent != 42 || windows[0].Duration != 5*time.Hour {
		t.Fatalf("got windows %+v", windows)
	}

	if !slices.Equal(askedPaths, []string{"/backend-api/wham/usage"}) {
		t.Errorf("got paths %v", askedPaths)
	}
}

func TestWindowsReportedByATurnAreKeptOverAFreshProbe(t *testing.T) {
	usageServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		t.Error("expected no probe when a turn already reported its windows")
		writer.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(usageServer.Close)

	turnServer := rateLimitedTurn(t, map[string]string{
		"X-Codex-Primary-Used-Percent":   "50",
		"X-Codex-Primary-Window-Minutes": "300",
	})

	client := newUsageClient(t, turnServer.URL+"/backend-api/codex/responses")
	client.URL = turnServer.URL
	client.AddUserMessage("hello")

	if _, err := client.Send(t.Context(), func(agent.Output) bool { return true }); err != nil {
		t.Fatal(err)
	}

	client.URL = usageServer.URL + "/backend-api/codex/responses"

	windows, err := client.UsageWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(windows) != 1 || windows[0].Percent != 50 {
		t.Errorf("expected the turn's own snapshot, got %v", windows)
	}
}
