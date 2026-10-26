package harness

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"crdx.org/oh/internal/req"
	"crdx.org/oh/internal/sim"
	"crdx.org/oh/pkg/provider/codex"
)

type compressionReply struct {
	status  int
	message string
	isFinal bool
}

type compressionPolicyCase struct {
	name           string
	compression    req.Compression
	body           json.RawMessage
	header         http.Header
	replies        []compressionReply
	calls          int
	wantEncodings  []string
	wantErrors     []int
	wantRejections int
}

type compressionCounter struct {
	rejections atomic.Int32
}

func (self *compressionCounter) Start(req.Request) req.ExchangeObserver { return nil }

func (self *compressionCounter) CompressionRejected() { self.rejections.Add(1) }

func compressionGoldenBody(length int) json.RawMessage {
	var text strings.Builder
	for i := 0; text.Len() < length; i++ {
		fmt.Fprintf(&text, "Conversation item %04d has unchanged content. ", i)
	}
	return json.RawMessage(`"` + text.String()[:length-2] + `"`)
}

func compressionPolicyCases() []compressionPolicyCase {
	body := compressionGoldenBody(8192)
	cases := []compressionPolicyCase{
		{name: "default client stays plain", body: body, calls: 1, wantEncodings: []string{""}, wantErrors: []int{0}},
		{name: "zstd expansion ratio limit", compression: req.Zstd, body: json.RawMessage(`"` + strings.Repeat("a", 100_000) + `"`), calls: 1, wantEncodings: []string{""}, wantErrors: []int{0}},
	}
	for _, compression := range []req.Compression{req.Gzip, req.Zstd} {
		name := string(compression) + " "
		cases = append(cases,
			compressionPolicyCase{name: name + "below threshold", compression: compression, body: compressionGoldenBody(4095), calls: 1, wantEncodings: []string{""}, wantErrors: []int{0}},
			compressionPolicyCase{name: name + "at threshold", compression: compression, body: compressionGoldenBody(4096), calls: 1, wantEncodings: []string{string(compression)}, wantErrors: []int{0}},
			compressionPolicyCase{name: name + "success", compression: compression, body: body, calls: 1, wantEncodings: []string{string(compression)}, wantErrors: []int{0}},
			compressionPolicyCase{name: name + "explicit identity", compression: compression, body: body, header: http.Header{"Content-Encoding": {"identity"}}, calls: 1, wantEncodings: []string{"identity"}, wantErrors: []int{0}},
			compressionPolicyCase{name: name + "fallback fails once", compression: compression, body: body, replies: []compressionReply{{status: 415}, {status: 415}, {status: 200}}, calls: 2, wantEncodings: []string{string(compression), "", ""}, wantErrors: []int{415, 0}, wantRejections: 1},
		)
		for _, rejection := range []compressionReply{
			{status: 415},
			{status: 400, message: "unsupported content-encoding: test"},
			{status: 400, message: "unsupported content encoding: test"},
			{status: 400, message: "unsupported encoding: test"},
			{status: 400, message: "invalid content-encoding: test"},
			{status: 400, message: "invalid content encoding: test"},
			{status: 400, message: "The request body is not valid JSON"},
			{status: 400, message: "Failed to parse request: could not parse request body as JSON"},
		} {
			cases = append(cases, compressionPolicyCase{
				name:           fmt.Sprintf("%sfallback %d %s", name, rejection.status, rejection.message),
				compression:    compression,
				body:           body,
				replies:        []compressionReply{rejection, {status: 200}, {status: 200}},
				calls:          2,
				wantEncodings:  []string{string(compression), "", ""},
				wantErrors:     []int{0, 0},
				wantRejections: 1,
			})
		}
		for _, rejection := range []compressionReply{
			{status: 400, message: "unsupported encoding", isFinal: true},
			{status: 415, isFinal: true},
			{status: 400, message: "invalid tool arguments"},
			{status: 401, message: "unsupported encoding"},
			{status: 403, message: "unsupported encoding"},
			{status: 413, message: "too large"},
			{status: 429, message: "try later"},
			{status: 500, message: "unsupported encoding"},
		} {
			nextEncoding := string(compression)
			wantRejections := 0
			if rejection.isFinal {
				nextEncoding = ""
				wantRejections = 1
			}
			cases = append(cases, compressionPolicyCase{
				name:           fmt.Sprintf("%sno fallback %d final %t", name, rejection.status, rejection.isFinal),
				compression:    compression,
				body:           body,
				replies:        []compressionReply{rejection, {status: 200}},
				calls:          2,
				wantEncodings:  []string{string(compression), nextEncoding},
				wantErrors:     []int{rejection.status, 0},
				wantRejections: wantRejections,
			})
		}
	}
	return cases
}

func TestGoldenRequestCompressionPolicy(t *testing.T) {
	passes := map[string]func() string{}
	for _, test := range compressionPolicyCases() {
		t.Run(test.name, func(t *testing.T) {
			report := compressionPolicyReport(t, test)
			passes[test.name] = func() string { return report }
		})
	}
	for _, compression := range []req.Compression{req.Gzip, req.Zstd} {
		passes[string(compression)+" cancelled request"] = func() string {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				attempts.Add(1)
			}))
			defer server.Close()
			client := req.NewStreaming(time.Second, time.Second)
			client.CompressWith(compression)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			_, _, err := client.Stream(ctx, server.URL, compressionGoldenBody(8192), nil)
			if !errors.Is(err, context.Canceled) || attempts.Load() != 0 {
				t.Fatalf("got %d attempts and error %v", attempts.Load(), err)
			}
			return fmt.Sprintf("requests sent: %d\ncontext cancelled: %t\n", attempts.Load(), errors.Is(err, context.Canceled))
		}
	}
	compareWithGolden(t, "request-compression-policy", ".txt", passes)
}

func compressionPolicyReport(t *testing.T, test compressionPolicyCase) string {
	t.Helper()

	type capture struct {
		header http.Header
		wire   []byte
		body   []byte
		length int64
		status int
	}
	captures := make(chan capture, 8)
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		wire, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
		}
		request.Body = io.NopCloser(bytes.NewReader(wire))
		body, err := sim.ReadBody(request)
		if err != nil {
			t.Error(err)
		}
		response := compressionReply{status: 200}
		index := int(attempts.Add(1) - 1)
		if len(test.replies) > 0 {
			response = test.replies[min(index, len(test.replies)-1)]
		}
		captures <- capture{request.Header.Clone(), wire, body, request.ContentLength, response.status}
		if response.isFinal {
			writer.Header().Set("X-Should-Retry", "false")
		}
		if response.status != http.StatusOK {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(response.status)
			_ = json.NewEncoder(writer).Encode(map[string]any{"error": map[string]string{"message": response.message}})
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: accepted\n\n")
	}))
	defer server.Close()

	client := req.NewStreaming(time.Second, time.Second)
	client.CompressWith(test.compression)
	counter := &compressionCounter{}
	client.Observe(counter)
	header := test.header.Clone()
	if header == nil {
		header = http.Header{}
	}
	header.Set("X-Compression-Test", "kept")
	originalHeader := header.Clone()
	var report strings.Builder
	var failures []int
	for i := range test.calls {
		body, _, err := client.Stream(t.Context(), server.URL, test.body, header)
		status := 0
		if err != nil {
			var refusal *req.StatusError
			if !errors.As(err, &refusal) {
				t.Fatal(err)
			}
			status = refusal.Status
			fmt.Fprintf(&report, "call %d: HTTP %d\n", i+1, status)
		} else {
			answer, readError := io.ReadAll(body)
			_ = body.Close()
			if readError != nil || string(answer) != "data: accepted\n\n" {
				t.Fatalf("got stream %q: %v", answer, readError)
			}
			fmt.Fprintf(&report, "call %d: %q\n", i+1, answer)
		}
		failures = append(failures, status)
	}
	if !slices.Equal(failures, test.wantErrors) {
		t.Errorf("got call statuses %v, want %v", failures, test.wantErrors)
	}
	var encodings []string
	for len(captures) > 0 {
		sent := <-captures
		encodings = append(encodings, sent.header.Get("Content-Encoding"))
		isSameBody := bytes.Equal(sent.body, test.body)
		isLengthCorrect := sent.length == int64(len(sent.wire))
		if !isSameBody || !isLengthCorrect || sent.header.Get("X-Compression-Test") != "kept" {
			t.Error("the body, content length or caller header changed")
		}
		fmt.Fprintf(&report, "attempt %d: encoding %q, HTTP %d\n", len(encodings), sent.header.Get("Content-Encoding"), sent.status)
		fmt.Fprintf(&report, "  content-type: %s\n  caller header: %s\n", sent.header.Get("Content-Type"), sent.header.Get("X-Compression-Test"))
		fmt.Fprintf(&report, "  JSON bytes: %d; wire bytes: %d; content length correct: %t\n", len(sent.body), len(sent.wire), isLengthCorrect)
		fmt.Fprintf(&report, "  decoded SHA-256: %x\n  body unchanged: %t\n", sha256.Sum256(sent.body), isSameBody)
	}
	if !slices.Equal(encodings, test.wantEncodings) {
		t.Errorf("got encodings %q, want %q", encodings, test.wantEncodings)
	}
	isHeaderSame := reflect.DeepEqual(header, originalHeader)
	if !isHeaderSame {
		t.Error("the caller's header map changed")
	}
	fmt.Fprintf(&report, "caller header map unchanged: %t\n", isHeaderSame)
	if got := int(counter.rejections.Load()); got != test.wantRejections {
		t.Errorf("got %d compression rejections, want %d", got, test.wantRejections)
	}
	fmt.Fprintf(&report, "compression rejections: %d\n", counter.rejections.Load())
	return report.String()
}

func TestGoldenCodexSearchCompression(t *testing.T) {
	passes := map[string]func() string{"large search": func() string {
		query := string(compressionGoldenBody(8192))
		endpoint := sim.New(&sim.Scenario{Model: "fake", Turns: []sim.Turn{{Say: "Search works."}}})
		requests := make(chan *http.Request, 1)
		bodies := make(chan []byte, 1)
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			body, err := sim.ReadBody(request)
			if err != nil {
				t.Error(err)
			}
			requests <- request.Clone(request.Context())
			bodies <- body
			request.Header.Del("Content-Encoding")
			request.Body = io.NopCloser(bytes.NewReader(body))
			endpoint.ServeHTTP(writer, request)
		}))
		defer server.Close()
		client, err := codex.NewSearch(codex.Static("test-token", "test-account"), "fake")
		if err != nil {
			t.Fatal(err)
		}
		client.URL = endpoint.Addresses(server.URL)[sim.Responses]
		answer, err := client.Search(t.Context(), query)
		if err != nil {
			t.Fatal(err)
		}
		request := <-requests
		body := <-bodies
		var payload struct {
			Input []struct {
				Content string `json:"content"`
			} `json:"input"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		isQuerySame := len(payload.Input) == 1 && payload.Input[0].Content == query
		if request.Header.Get("Content-Encoding") != "zstd" || !isQuerySame || answer != "Search works." {
			t.Fatalf("unexpected search: encoding %q, query unchanged %t, answer %q", request.Header.Get("Content-Encoding"), isQuerySame, answer)
		}
		return fmt.Sprintf("POST %s\ncontent-encoding: %s\nquery unchanged: %t\nanswer: %s\n", request.URL.Path, request.Header.Get("Content-Encoding"), isQuerySame, answer)
	}}
	compareWithGolden(t, "codex-search-compression", ".txt", passes)
}
