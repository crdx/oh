package wire_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/store/wire"
	"crdx.org/oh/internal/req"
)

func TestRecorderContinuesSequenceNumbersAfterLongLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wire.http")
	stored := strings.Join([]string{
		"# HTTP transcript\n",
		strings.Repeat("x", 128*1024),
		"\n# exchange 7 start\n",
	}, "")
	if err := os.WriteFile(path, []byte(stored), 0o600); err != nil {
		t.Fatal(err)
	}

	recorder, err := wire.Open(path, wire.Meta{}, func(err error) {
		t.Errorf("unexpected recorder failure: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder.Start(req.Request{StartedAt: time.Unix(2, 0), Method: http.MethodPost})
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	transcript, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(transcript), "# exchange 8 start") {
		t.Errorf("expected sequence numbering to continue after the long line")
	}
}

func TestRecorderFindsTheLastSequenceNumberFarFromTheEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wire.http")
	stored := strings.Join([]string{
		"# HTTP transcript\n",
		"# exchange 7 start\n",
		strings.Repeat("body line\n", 200*1024),
	}, "")
	if err := os.WriteFile(path, []byte(stored), 0o600); err != nil {
		t.Fatal(err)
	}

	recorder, err := wire.Open(path, wire.Meta{}, func(err error) {
		t.Errorf("unexpected recorder failure: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder.Start(req.Request{StartedAt: time.Unix(2, 0), Method: http.MethodPost})
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	transcript, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(transcript), "# exchange 8 start") {
		t.Errorf("expected the marker to be found beyond the first window read back")
	}
}

func TestRecorderNumbersTheFirstExchangeOfATranscriptWithoutOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wire.http")
	if err := os.WriteFile(path, []byte("# HTTP transcript\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	recorder, err := wire.Open(path, wire.Meta{}, func(err error) {
		t.Errorf("unexpected recorder failure: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder.Start(req.Request{StartedAt: time.Unix(2, 0), Method: http.MethodPost})
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	transcript, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(transcript), "# exchange 1 start") {
		t.Errorf("expected numbering to begin at one, got %q", string(transcript))
	}
}

func TestRecorderIgnoresAnEndMarkerWhenNumberingTheNextExchange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wire.http")
	stored := strings.Join([]string{
		"# HTTP transcript\n",
		"# exchange 4 start\n",
		"# exchange 4 end 1970-01-01T00:00:00Z elapsed=1s completed\n\n",
	}, "")
	if err := os.WriteFile(path, []byte(stored), 0o600); err != nil {
		t.Fatal(err)
	}

	recorder, err := wire.Open(path, wire.Meta{}, func(err error) {
		t.Errorf("unexpected recorder failure: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder.Start(req.Request{StartedAt: time.Unix(2, 0), Method: http.MethodPost})
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	transcript, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(transcript), "# exchange 5 start") {
		t.Errorf("expected the end marker to be passed over, got %q", string(transcript))
	}
}

func TestRecorderIgnoresAReadMarkerWhenNumberingTheNextExchange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wire.http")
	stored := strings.Join([]string{
		"# HTTP transcript\n",
		"# exchange 4 start 1970-01-01T00:00:02Z\n",
		"# exchange 4 read 1970-01-01T00:00:03Z elapsed=1s\n",
		"data: one\n",
		"# exchange 4 read 1970-01-01T00:00:03.25Z elapsed=1.25s gap=250ms\n",
		"data: two\n",
	}, "")
	if err := os.WriteFile(path, []byte(stored), 0o600); err != nil {
		t.Fatal(err)
	}

	recorder, err := wire.Open(path, wire.Meta{}, func(err error) {
		t.Errorf("unexpected recorder failure: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder.Start(req.Request{StartedAt: time.Unix(2, 0), Method: http.MethodPost})
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	transcript, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(transcript), "# exchange 5 start") {
		t.Errorf("expected the read markers to be passed over, got %q", string(transcript))
	}
}

func TestEachStreamingReadIsTimestampedSoBurstsAreVisible(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wire.http")
	recorder, err := wire.Open(path, wire.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 0)}, func(err error) {
		t.Errorf("unexpected recorder failure: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}

	exchange := recorder.Start(req.Request{
		StartedAt: time.Unix(2, 0),
		Method:    http.MethodPost,
		URL:       "http://example.test/",
		Protocol:  "HTTP/1.1",
		Header:    http.Header{"Content-Type": {"application/json"}},
		Body:      []byte(`{}`),
	})
	exchange.Response(req.Response{
		ReceivedAt: time.Unix(3, 0),
		Protocol:   "HTTP/1.1",
		Status:     "200 OK",
		Code:       200,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
	})
	exchange.Body(time.Unix(3, 0), []byte("data: one\n"))
	exchange.Body(time.Unix(3, 0).Add(250*time.Millisecond), []byte("data: two\ndata: three\n"))
	exchange.Body(time.Unix(3, 0).Add(400*time.Millisecond), []byte("data: partial"))
	exchange.Finish(time.Unix(4, 0), nil, false)

	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	transcript := string(stored)

	for _, marker := range []string{
		"# exchange 1 read 1970-01-01T00:00:03Z elapsed=1s bytes=10\ndata: one\n",
		"# exchange 1 read 1970-01-01T00:00:03.25Z elapsed=1.25s gap=250ms bytes=22\ndata: two\ndata: three\n",
		"# exchange 1 read 1970-01-01T00:00:03.4Z elapsed=1.4s gap=150ms bytes=13\n",
	} {
		if !strings.Contains(transcript, marker) {
			t.Errorf("expected %q in:\n%s", marker, transcript)
		}
	}

	if strings.Count(transcript, "# exchange 1 read ") != 3 {
		t.Errorf("expected a marker for every read, got:\n%s", transcript)
	}
}

func TestRecorderCensorsHeadersJSONFormsSSEAndBearerText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wire.http")
	recorder, err := wire.Open(path, wire.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 0)}, func(err error) {
		t.Errorf("unexpected recorder failure: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}

	exchange := recorder.Start(req.Request{
		StartedAt: time.Unix(2, 0),
		Method:    http.MethodPost,
		URL:       "http://example.test/",
		Protocol:  "HTTP/1.1",
		Header: http.Header{
			"Authorization": {"Bearer request-secret"},
			"Content-Type":  {"application/json"},
		},
		Body: []byte(`{"nested":{"access_token":"json-secret","innocent":"kept"}}`),
	})
	exchange.Response(req.Response{
		ReceivedAt: time.Unix(3, 0),
		Protocol:   "HTTP/1.1",
		Status:     "200 OK",
		Code:       200,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
	})
	exchange.Body(time.Unix(3, 0), []byte("data: {\"refresh_token\":\"sse-secret\",\"ok\":true}\n\ndata: Bearer response-secret\n"))
	exchange.Finish(time.Unix(4, 0), nil, false)
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	transcript := string(stored)
	for _, secret := range []string{"request-secret", "json-secret", "sse-secret", "response-secret"} {
		if strings.Contains(transcript, secret) {
			t.Errorf("secret %q survived censorship:\n%s", secret, transcript)
		}
	}
	if strings.Count(transcript, "[REDACTED]") != 4 {
		t.Errorf("expected four replacements, got:\n%s", transcript)
	}
	if !strings.Contains(transcript, `"innocent":"kept"`) || !strings.Contains(transcript, `"ok":true`) {
		t.Errorf("expected benign values to survive, got:\n%s", transcript)
	}
	if !strings.Contains(transcript, "# exchange 1 end") || !strings.Contains(transcript, "completed") {
		t.Errorf("expected a completed exchange marker, got:\n%s", transcript)
	}
}

func TestRecorderCensorsIdentityMetadataWithoutCensoringProtocolIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wire.http")
	recorder, err := wire.Open(path, wire.Meta{}, func(err error) {
		t.Errorf("unexpected recorder failure: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}

	exchange := recorder.Start(req.Request{
		StartedAt: time.Unix(2, 0),
		Method:    http.MethodPost,
		URL:       "https://example.test/",
		Protocol:  "HTTP/1.1",
		Header: http.Header{
			"Anthropic-Organization-Id": {"organisation-secret"},
			"Anthropic-Workspace-Id":    {"workspace-secret"},
			"Request-Id":                {"request-secret"},
			"Traceresponse":             {"trace-secret"},
			"Cf-Ray":                    {"ray-secret"},
			"Content-Type":              {"application/json"},
		},
		Body: []byte(`{"account":{"email_address":"person@example.test","uuid":"account-secret"},"organization":{"name":"Private Organisation","uuid":"organisation-body-secret"},"token_uuid":"token-secret","request_id":"request-body-secret","safety_identifier":"safety-secret","id":"message-id","call_id":"call-id","innocent":"kept"}`),
	})
	exchange.Response(req.Response{
		ReceivedAt: time.Unix(3, 0),
		Protocol:   "HTTP/1.1",
		Status:     "200 OK",
		Code:       200,
		Header: http.Header{
			"Content-Type":              {"text/event-stream"},
			"Anthropic-Organization-Id": {"response-organisation-secret"},
			"X-Request-Id":              {"response-request-secret"},
		},
	})
	exchange.Body(time.Unix(3, 0), []byte("data: {\"workspace_id\":\"response-workspace-secret\",\"uuid\":\"response-uuid-secret\",\"id\":\"response-id\",\"call_id\":\"response-call-id\",\"ok\":true}\n\n"))
	exchange.Finish(time.Unix(4, 0), nil, false)
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	transcript := string(stored)
	for _, secret := range []string{
		"organisation-secret",
		"workspace-secret",
		"request-secret",
		"trace-secret",
		"ray-secret",
		"person@example.test",
		"account-secret",
		"Private Organisation",
		"organisation-body-secret",
		"token-secret",
		"request-body-secret",
		"safety-secret",
		"response-organisation-secret",
		"response-request-secret",
		"response-workspace-secret",
		"response-uuid-secret",
	} {
		if strings.Contains(transcript, secret) {
			t.Errorf("identity value %q survived censorship:\n%s", secret, transcript)
		}
	}
	for _, kept := range []string{"message-id", "call-id", "response-id", "response-call-id", `"innocent":"kept"`, `"ok":true`} {
		if !strings.Contains(transcript, kept) {
			t.Errorf("expected protocol value %q to survive:\n%s", kept, transcript)
		}
	}
}

func recordBody(t *testing.T, body []byte, contentType string, events string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "wire.http")
	recorder, err := wire.Open(path, wire.Meta{}, func(err error) {
		t.Errorf("unexpected recorder failure: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}

	exchange := recorder.Start(req.Request{
		StartedAt: time.Unix(2, 0),
		Method:    http.MethodPost,
		URL:       "https://example.test/",
		Protocol:  "HTTP/1.1",
		Header:    http.Header{"Content-Type": {contentType}},
		Body:      body,
	})
	exchange.Response(req.Response{
		ReceivedAt: time.Unix(3, 0),
		Protocol:   "HTTP/1.1",
		Status:     "200 OK",
		Code:       200,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
	})
	if events != "" {
		exchange.Body(time.Unix(3, 0), []byte(events))
	}
	exchange.Finish(time.Unix(4, 0), nil, false)

	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	return string(stored)
}

func recordedLine(t *testing.T, transcript string, prefix string) string {
	t.Helper()

	for line := range strings.SplitSeq(transcript, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}

	t.Fatalf("no line starting %q in:\n%s", prefix, transcript)
	return ""
}

func TestACensoredBodyIsStillTheJSONItWas(t *testing.T) {
	body := []byte(`{"messages":[{"text":"got != \"Bearer accepted-key\" {"}],"token":"secret"}`)

	transcript := recordBody(t, body, "application/json", "")
	recorded := recordedLine(t, transcript, `{"messages"`)

	if strings.Contains(recorded, "accepted-key") {
		t.Errorf("the token survived censorship: %s", recorded)
	}

	var value any
	if err := json.Unmarshal([]byte(recorded), &value); err != nil {
		t.Errorf("the censored body no longer decodes: %v\n%s", err, recorded)
	}
}

func TestACensoredEventIsStillTheJSONItWas(t *testing.T) {
	events := "data: {\"text\":\"got != \\\"Bearer accepted-key\\\" {\"}\n\n"

	transcript := recordBody(t, []byte(`{}`), "application/json", events)
	recorded := recordedLine(t, transcript, "data: ")

	if strings.Contains(recorded, "accepted-key") {
		t.Errorf("the token survived censorship: %s", recorded)
	}

	var value any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(recorded, "data: ")), &value); err != nil {
		t.Errorf("the censored event no longer decodes: %v\n%s", err, recorded)
	}
}

func TestABodyWithNothingToHideIsRecordedAsItWasSent(t *testing.T) {
	body := []byte(`{"zebra":1,"apple":2,"nested":{"kept":"as it is"}}`)

	transcript := recordBody(t, body, "application/json", "")

	if !strings.Contains(transcript, string(body)) {
		t.Errorf("expected the body unchanged, got:\n%s", transcript)
	}
}

func TestATransparentlyDecompressedResponseSaysSo(t *testing.T) {
	for name, isCompressed := range map[string]bool{"compressed": true, "plain": false} {
		path := filepath.Join(t.TempDir(), "wire.http")
		recorder, err := wire.Open(path, wire.Meta{}, func(err error) {
			t.Errorf("unexpected recorder failure: %v", err)
		})
		if err != nil {
			t.Fatal(err)
		}

		exchange := recorder.Start(req.Request{StartedAt: time.Unix(2, 0), Method: http.MethodPost})
		exchange.Response(req.Response{
			ReceivedAt:   time.Unix(3, 0),
			Protocol:     "HTTP/2.0",
			Status:       "200 OK",
			Code:         200,
			Header:       http.Header{"Content-Type": {"text/event-stream"}},
			IsCompressed: isCompressed,
		})
		exchange.Finish(time.Unix(4, 0), nil, false)
		if err := recorder.Close(); err != nil {
			t.Fatal(err)
		}

		stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
		if err != nil {
			t.Fatal(err)
		}
		note := "# exchange 1 gzip decompressed by the transport"
		if strings.Contains(string(stored), note) != isCompressed {
			t.Errorf("%s response recorded wrongly:\n%s", name, string(stored))
		}
	}
}
