package wire_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"crdx.org/oh/internal/app/store/wire"
	"crdx.org/oh/internal/req"
)

func TestANewTranscriptOpensWithItsHeaderAndNumbersFromOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wire.http.zst")
	recorder := openRecorder(t, path)
	recordExchange(recorder, 1)
	closeRecorder(t, recorder)

	transcript := decompressed(t, path)
	if !strings.HasPrefix(transcript, "# HTTP transcript\n# session: tame-impala\n") {
		t.Errorf("expected the header first, got:\n%s", transcript)
	}
	if !strings.Contains(transcript, "# exchange 1 start") {
		t.Errorf("expected numbering to begin at one, got:\n%s", transcript)
	}
}

func TestReopeningATranscriptContinuesItsNumbering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wire.http.zst")
	for run := range 3 {
		recorder := openRecorder(t, path)
		recordExchange(recorder, 2)
		closeRecorder(t, recorder)

		if run == 0 {
			continue
		}
		transcript := decompressed(t, path)
		if strings.Count(transcript, "# HTTP transcript") != 1 {
			t.Errorf("expected one header, got:\n%s", transcript)
		}
	}

	transcript := decompressed(t, path)
	for sequence := 1; sequence <= 6; sequence++ {
		if strings.Count(transcript, fmt.Sprintf("# exchange %d start", sequence)) != 1 {
			t.Errorf("expected exchange %d once, got:\n%s", sequence, transcript)
		}
	}
}

func TestATrailerNamesTheNextExchangeWithoutTheFrameBeingRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wire.http.zst")
	stored := []byte{0x28, 0xB5, 0x2F, 0xFD, 0x00, 0x58, 0x25, 0x00, 0x00, 0xDE, 0xAD, 0xBE, 0xEF}
	stored = binary.LittleEndian.AppendUint32(stored, 0x184D2A50)
	stored = binary.LittleEndian.AppendUint32(stored, 8)
	stored = binary.LittleEndian.AppendUint64(stored, 41)
	if err := os.WriteFile(path, stored, 0o600); err != nil {
		t.Fatal(err)
	}

	recorder := openRecorder(t, path)
	recordExchange(recorder, 1)
	closeRecorder(t, recorder)

	written, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	transcript := decompressedBytes(t, written[len(stored):])
	if !strings.HasPrefix(transcript, "# exchange 41 start") {
		t.Errorf("expected the trailer to be believed, got:\n%s", transcript)
	}
}

func TestAnUnclosedFrameIsClosedAndReadForItsNumbering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wire.http.zst")
	crashed := openRecorder(t, path)
	recordExchange(crashed, 4)
	exchange := crashed.Start(req.Request{
		StartedAt: time.Unix(5, 0),
		Method:    http.MethodPost,
		Header:    http.Header{"Content-Type": {"text/plain"}},
		Body:      []byte(strings.Repeat("x", 3<<20)),
	})
	exchange.Response(req.Response{ReceivedAt: time.Unix(6, 0), Protocol: "HTTP/1.1", Status: "200 OK", Code: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}})
	exchange.Body(time.Unix(6, 0), []byte("data: one\n"))
	exchange.Finish(time.Unix(7, 0), nil, false)

	recorder := openRecorder(t, path)
	recordExchange(recorder, 1)
	closeRecorder(t, recorder)

	transcript := decompressed(t, path)
	if !strings.Contains(transcript, "# exchange 6 start") {
		t.Errorf("expected numbering to continue past the unclosed frame, got:\n%s", tail(transcript))
	}
	if strings.Count(transcript, "# exchange 5 end") != 1 {
		t.Errorf("expected everything flushed before the crash to survive, got:\n%s", tail(transcript))
	}
}

func TestATornBlockIsCutAwayAndWhatCameBeforeItSurvives(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wire.http.zst")
	crashed := openRecorder(t, path)
	recordExchange(crashed, 2)
	crashed.Start(req.Request{StartedAt: time.Unix(9, 0), Method: http.MethodPost, Body: []byte("torn away")})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, info.Size()-2); err != nil {
		t.Fatal(err)
	}

	recorder := openRecorder(t, path)
	recordExchange(recorder, 1)
	closeRecorder(t, recorder)

	transcript := decompressed(t, path)
	if strings.Contains(transcript, "torn away") {
		t.Errorf("expected the torn block to be cut away, got:\n%s", transcript)
	}
	if !strings.Contains(transcript, "# exchange 2 end") || !strings.Contains(transcript, "# exchange 3 start") {
		t.Errorf("expected the whole exchanges to survive and numbering to continue, got:\n%s", transcript)
	}
}

func TestATornTrailerIsCutAwayAndTheFrameReadInstead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wire.http.zst")
	recorder := openRecorder(t, path)
	recordExchange(recorder, 2)
	closeRecorder(t, recorder)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, info.Size()-3); err != nil {
		t.Fatal(err)
	}

	recorder = openRecorder(t, path)
	recordExchange(recorder, 1)
	closeRecorder(t, recorder)

	if !strings.Contains(decompressed(t, path), "# exchange 3 start") {
		t.Errorf("expected numbering to continue, got:\n%s", decompressed(t, path))
	}
}

func TestATranscriptThatIsNotZstdIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wire.http.zst")
	if err := os.WriteFile(path, []byte("# HTTP transcript\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := wire.Open(path, wire.Meta{}, nil); err == nil {
		t.Error("expected a plain transcript to be refused")
	}

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != "# HTTP transcript\n\n" {
		t.Errorf("expected the refused file untouched, got %q", stored)
	}
}

func openRecorder(t *testing.T, path string) *wire.Recorder {
	t.Helper()

	recorder, err := wire.Open(path, wire.Meta{Name: "tame-impala", StartedAt: time.Unix(1, 0)}, func(err error) {
		t.Errorf("unexpected recorder failure: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}
	return recorder
}

func closeRecorder(t *testing.T, recorder *wire.Recorder) {
	t.Helper()

	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}
}

func recordExchange(recorder *wire.Recorder, count int) {
	for range count {
		exchange := recorder.Start(req.Request{
			StartedAt: time.Unix(2, 0),
			Method:    http.MethodPost,
			URL:       "https://example.test/",
			Protocol:  "HTTP/1.1",
			Header:    http.Header{"Content-Type": {"application/json"}},
			Body:      []byte(`{"messages":[]}`),
		})
		exchange.Response(req.Response{
			ReceivedAt: time.Unix(3, 0),
			Protocol:   "HTTP/1.1",
			Status:     "200 OK",
			Code:       200,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
		})
		exchange.Body(time.Unix(3, 0), []byte("data: one\n"))
		exchange.Body(time.Unix(3, 0).Add(time.Second), []byte("data: two\n"))
		exchange.Finish(time.Unix(4, 0), nil, false)
	}
}

func decompressed(t *testing.T, path string) string {
	t.Helper()

	stored, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	return decompressedBytes(t, stored)
}

func decompressedBytes(t *testing.T, stored []byte) string {
	t.Helper()

	decoder, err := zstd.NewReader(bytes.NewReader(stored), zstd.WithDecoderMaxWindow(128<<20))
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()

	transcript, err := io.ReadAll(decoder)
	if err != nil {
		t.Fatalf("the transcript does not decompress: %v", err)
	}
	return string(transcript)
}

func tail(transcript string) string {
	return transcript[max(len(transcript)-2000, 0):]
}

func TestEachStreamingReadIsTimestampedSoBurstsAreVisible(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wire.http.zst")
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

	stored := decompressed(t, path)
	transcript := stored

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
	path := filepath.Join(t.TempDir(), "wire.http.zst")
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

	stored := decompressed(t, path)
	transcript := stored
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
	path := filepath.Join(t.TempDir(), "wire.http.zst")
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

	stored := decompressed(t, path)
	transcript := stored
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

	path := filepath.Join(t.TempDir(), "wire.http.zst")
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

	stored := decompressed(t, path)

	return stored
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
		path := filepath.Join(t.TempDir(), "wire.http.zst")
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

		stored := decompressed(t, path)
		note := "# exchange 1 gzip decompressed by the transport"
		if strings.Contains(stored, note) != isCompressed {
			t.Errorf("%s response recorded wrongly:\n%s", name, stored)
		}
	}
}

func TestRecorderCensorsOAuthCodesInFormsJSONAndURLs(t *testing.T) {
	form := `grant_type=authorization_code&client_id=public-client&redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback&code=single-use-code&code_verifier=single-use-verifier&state=single-use-state`
	transcript := recordBody(t, []byte(form), "application/x-www-form-urlencoded", "")
	for _, secret := range []string{"single-use-code", "single-use-verifier", "single-use-state"} {
		if strings.Contains(transcript, secret) {
			t.Errorf("form secret %q survived censorship:\n%s", secret, transcript)
		}
	}
	for _, kept := range []string{"authorization_code", "public-client"} {
		if !strings.Contains(transcript, kept) {
			t.Errorf("expected benign form value %q to survive:\n%s", kept, transcript)
		}
	}
	if strings.Count(transcript, "REDACTED") != 3 {
		t.Errorf("expected three form replacements, got:\n%s", transcript)
	}

	jsonBody := []byte(`{"grant_type":"authorization_code","client_id":"public-client","code":"json-code","code_verifier":"json-verifier","state":"json-state","redirect_uri":"http://localhost:53692/callback"}`)
	transcript = recordBody(t, jsonBody, "application/json", "")
	for _, secret := range []string{"json-code", "json-verifier", "json-state"} {
		if strings.Contains(transcript, secret) {
			t.Errorf("JSON secret %q survived censorship:\n%s", secret, transcript)
		}
	}
	for _, kept := range []string{"authorization_code", "public-client"} {
		if !strings.Contains(transcript, kept) {
			t.Errorf("expected benign JSON value %q to survive:\n%s", kept, transcript)
		}
	}
	if strings.Count(transcript, "[REDACTED]") != 3 {
		t.Errorf("expected three JSON replacements, got:\n%s", transcript)
	}
}

func TestRecorderCensorsSensitiveQueryParametersInRequestURLs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wire.http.zst")
	recorder, err := wire.Open(path, wire.Meta{}, func(err error) {
		t.Errorf("unexpected recorder failure: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}

	exchange := recorder.Start(req.Request{
		StartedAt: time.Unix(2, 0),
		Method:    http.MethodPost,
		URL:       "https://example.test/token?code=url-code&state=url-state&next=kept",
		Protocol:  "HTTP/1.1",
		Header:    http.Header{"Content-Type": {"application/json"}},
		Body:      []byte(`{}`),
	})
	exchange.Response(req.Response{
		ReceivedAt: time.Unix(3, 0),
		Protocol:   "HTTP/1.1",
		Status:     "200 OK",
		Code:       200,
		Header:     http.Header{"Content-Type": {"application/json"}},
	})
	exchange.Finish(time.Unix(4, 0), nil, false)
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	transcript := decompressed(t, path)
	for _, secret := range []string{"url-code", "url-state"} {
		if strings.Contains(transcript, secret) {
			t.Errorf("URL secret %q survived censorship:\n%s", secret, transcript)
		}
	}
	if !strings.Contains(transcript, "next=kept") {
		t.Errorf("expected a benign query value to survive:\n%s", transcript)
	}
	if !strings.Contains(transcript, "REDACTED") {
		t.Errorf("expected a redacted query value:\n%s", transcript)
	}
}

func TestARecordedURLWithoutSecretsIsLeftExactlyAsWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wire.http.zst")
	recorder, err := wire.Open(path, wire.Meta{}, func(err error) {
		t.Errorf("unexpected recorder failure: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}

	address := "https://example.test/token?grant_type=authorization_code&next=kept"
	exchange := recorder.Start(req.Request{
		StartedAt: time.Unix(2, 0),
		Method:    http.MethodPost,
		URL:       address,
		Protocol:  "HTTP/1.1",
		Header:    http.Header{"Content-Type": {"application/json"}},
		Body:      []byte(`{}`),
	})
	exchange.Response(req.Response{
		ReceivedAt: time.Unix(3, 0),
		Protocol:   "HTTP/1.1",
		Status:     "200 OK",
		Code:       200,
		Header:     http.Header{"Content-Type": {"application/json"}},
	})
	exchange.Finish(time.Unix(4, 0), nil, false)
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	transcript := decompressed(t, path)
	if !strings.Contains(transcript, "> POST "+address+" HTTP/1.1") {
		t.Errorf("expected the address unchanged, got:\n%s", transcript)
	}
}

func TestANestedErrorCodeIsKeptWhileATopLevelOAuthCodeIsCensored(t *testing.T) {
	body := []byte(`{"code":"top-level-secret","error":{"code":"rate_limit_exceeded","message":"slow down","type":"rate_limit_error"},"innocent":"kept"}`)

	transcript := recordBody(t, body, "application/json", "")
	if strings.Contains(transcript, "top-level-secret") {
		t.Errorf("the top-level code survived censorship:\n%s", transcript)
	}
	for _, kept := range []string{"rate_limit_exceeded", "slow down", "rate_limit_error", `"innocent":"kept"`} {
		if !strings.Contains(transcript, kept) {
			t.Errorf("expected protocol value %q to survive:\n%s", kept, transcript)
		}
	}
	if strings.Count(transcript, "[REDACTED]") != 1 {
		t.Errorf("expected one replacement, got:\n%s", transcript)
	}
}
