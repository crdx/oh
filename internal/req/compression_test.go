package req_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/req"
	"crdx.org/oh/internal/sim"
)

func compressionText() string {
	var text strings.Builder
	for i := range 300 {
		fmt.Fprintf(&text, "Request item %d carries its own conversation text.\n", i)
	}
	return text.String()
}

func TestRequestsUseTheirSelectedCompression(t *testing.T) {
	for _, compression := range []req.Compression{"", req.Gzip, req.Zstd} {
		t.Run(string(compression), func(t *testing.T) {
			payload := map[string]string{"message": compressionText()}
			plain, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			type capture struct {
				header http.Header
				wire   []byte
				body   []byte
				length int64
			}
			captures := make(chan capture, 1)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				wire, readError := io.ReadAll(request.Body)
				if readError != nil {
					t.Error(readError)
				}
				request.Body = io.NopCloser(bytes.NewReader(wire))
				body, readError := sim.ReadBody(request)
				if readError != nil {
					t.Error(readError)
				}
				captures <- capture{request.Header.Clone(), wire, body, request.ContentLength}
				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(writer, "data: streamed answer\n\n")
			}))
			t.Cleanup(server.Close)

			client := req.NewStreaming(time.Second, time.Second)
			client.CompressWith(compression)
			exchange := &observedExchange{}
			client.Observe(observer{exchange: exchange})
			header := http.Header{"X-Test": {"kept"}}
			body, _, err := client.Stream(t.Context(), server.URL, payload, header)
			if err != nil {
				t.Fatal(err)
			}
			answer, err := io.ReadAll(body)
			_ = body.Close()
			if err != nil || string(answer) != "data: streamed answer\n\n" {
				t.Fatalf("got stream %q: %v", answer, err)
			}
			sent := <-captures
			if got := sent.header.Get("Content-Encoding"); got != string(compression) {
				t.Errorf("got encoding %q, want %q", got, compression)
			}
			if !bytes.Equal(sent.body, plain) || !bytes.Equal(exchange.request.Body, plain) {
				t.Error("the decoded request or the observer body changed")
			}
			if sent.length != int64(len(sent.wire)) {
				t.Errorf("content length %d differs from %d wire bytes", sent.length, len(sent.wire))
			}
			if compression != "" && len(sent.wire) >= len(plain) {
				t.Error("compression did not reduce the upload")
			}
			if sent.header.Get("Content-Type") != "application/json" || sent.header.Get("X-Test") != "kept" {
				t.Errorf("request headers changed: %v", sent.header)
			}
			if exchange.request.Header.Get("Content-Encoding") != string(compression) {
				t.Errorf("observer did not record the actual encoding: %v", exchange.request.Header)
			}
			if header.Get("Content-Encoding") != "" || header.Get("Content-Type") != "" {
				t.Errorf("the caller's headers changed: %v", header)
			}
		})
	}
}

func TestSmallRequestsAndExcessiveZstdRatiosStayPlain(t *testing.T) {
	for _, test := range []struct {
		name        string
		compression req.Compression
		body        string
	}{
		{"small gzip", req.Gzip, "hello"},
		{"small zstd", req.Zstd, "hello"},
		{"zstd expansion limit", req.Zstd, strings.Repeat("a", 100_000)},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if got := request.Header.Get("Content-Encoding"); got != "" {
					t.Errorf("unexpected encoding %q", got)
				}
				if _, err := sim.ReadBody(request); err != nil {
					t.Error(err)
				}
			}))
			t.Cleanup(server.Close)
			client := req.New(time.Second)
			client.CompressWith(test.compression)
			body, _, err := client.Stream(t.Context(), server.URL, test.body, nil)
			if err != nil {
				t.Fatal(err)
			}
			_ = body.Close()
		})
	}
}

func TestAnEncodingRejectionRetriesPlainAndDisablesCompression(t *testing.T) {
	for _, compression := range []req.Compression{req.Gzip, req.Zstd} {
		t.Run(string(compression), func(t *testing.T) {
			encodings := make(chan string, 3)
			bodies := make(chan []byte, 3)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				encoding := request.Header.Get("Content-Encoding")
				encodings <- encoding
				body, err := sim.ReadBody(request)
				if err != nil {
					t.Error(err)
				}
				bodies <- body
				if encoding != "" {
					writer.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(writer, `{"error":{"message":"The request body is not valid JSON"}}`)
					return
				}
				_, _ = io.WriteString(writer, "streamed after fallback")
			}))
			t.Cleanup(server.Close)
			client := req.NewStreaming(time.Second, time.Second)
			client.CompressWith(compression)
			for range 2 {
				body, _, err := client.Stream(t.Context(), server.URL, compressionText(), nil)
				if err != nil {
					t.Fatal(err)
				}
				answer, err := io.ReadAll(body)
				_ = body.Close()
				if err != nil || string(answer) != "streamed after fallback" {
					t.Fatalf("got %q: %v", answer, err)
				}
			}
			got := []string{<-encodings, <-encodings, <-encodings}
			if !slices.Equal(got, []string{string(compression), "", ""}) {
				t.Errorf("got attempt encodings %q", got)
			}
			first := <-bodies
			fallback := <-bodies
			next := <-bodies
			if !bytes.Equal(first, fallback) || !bytes.Equal(first, next) {
				t.Error("fallback or the following request changed the JSON")
			}
		})
	}
}

func TestOrdinaryRefusalsDoNotRetryWithoutCompression(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{"invalid argument", 400, `{"error":{"message":"invalid tool arguments"}}`},
		{"authentication", 401, `{"error":{"message":"unsupported encoding"}}`},
		{"permission", 403, `{"error":{"message":"unsupported encoding"}}`},
		{"size limit", 413, `{"error":{"message":"too large"}}`},
		{"rate limit", 429, `{"error":{"message":"try later"}}`},
		{"server failure", 500, `{"error":{"message":"unsupported encoding"}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			attempts := make(chan struct{}, 2)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				attempts <- struct{}{}
				writer.WriteHeader(test.status)
				_, _ = io.WriteString(writer, test.body)
			}))
			t.Cleanup(server.Close)
			client := req.New(time.Second)
			client.CompressWith(req.Gzip)
			_, _, err := client.Stream(t.Context(), server.URL, compressionText(), nil)
			var refusal *req.StatusError
			if !errors.As(err, &refusal) || refusal.Status != test.status {
				t.Fatalf("got %v, want status %d", err, test.status)
			}
			if len(attempts) != 1 {
				t.Errorf("got %d attempts, want one", len(attempts))
			}
		})
	}
}
