package wire_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/req"
)

func TestCompressedRequestsKeepReadableCensoredTranscripts(t *testing.T) {
	for _, compression := range []req.Compression{req.Gzip, req.Zstd} {
		t.Run(string(compression), func(t *testing.T) {
			var text strings.Builder
			for i := range 300 {
				fmt.Fprintf(&text, "Conversation item %d stays readable.\n", i)
			}
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				_, _ = io.WriteString(writer, "answer")
			}))
			t.Cleanup(server.Close)
			path := filepath.Join(t.TempDir(), "wire.http.zst")
			recorder := openRecorder(t, path)
			client := req.NewStreaming(time.Second, time.Second)
			client.CompressWith(compression)
			client.Observe(recorder)
			body, _, err := client.Stream(t.Context(), server.URL, map[string]string{
				"text":  text.String(),
				"token": "private-test-token",
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, body)
			_ = body.Close()
			closeRecorder(t, recorder)

			transcript := decompressed(t, path)
			for _, want := range []string{
				"> Content-Encoding: " + string(compression),
				"# exchange 1 request body recorded before " + string(compression) + " compression",
				"Conversation item 299 stays readable.",
				`"token":"[REDACTED]"`,
			} {
				if !strings.Contains(transcript, want) {
					t.Errorf("transcript omitted %q", want)
				}
			}
			if strings.Contains(transcript, "private-test-token") {
				t.Error("the compressed request bypassed redaction")
			}
		})
	}
}

func TestCompressionFallbackRecordsEveryAttemptAndTheDisabledState(t *testing.T) {
	for _, compression := range []req.Compression{req.Gzip, req.Zstd} {
		t.Run(string(compression), func(t *testing.T) {
			var text strings.Builder
			for i := range 300 {
				fmt.Fprintf(&text, "Conversation item %d stays unchanged.\n", i)
			}
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Header.Get("Content-Encoding") != "" {
					writer.WriteHeader(http.StatusUnsupportedMediaType)
					_, _ = io.WriteString(writer, `{"error":{"message":"unsupported content encoding"}}`)
					return
				}
				_, _ = io.WriteString(writer, "answer")
			}))
			t.Cleanup(server.Close)
			path := filepath.Join(t.TempDir(), "wire.http.zst")
			recorder := openRecorder(t, path)
			client := req.NewStreaming(time.Second, time.Second)
			client.CompressWith(compression)
			client.Observe(recorder)
			for range 2 {
				body, _, err := client.Stream(t.Context(), server.URL, map[string]string{
					"text":  text.String(),
					"token": "private-fallback-token",
				}, nil)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = io.Copy(io.Discard, body)
				_ = body.Close()
			}
			closeRecorder(t, recorder)
			transcript := decompressed(t, path)
			for _, want := range []string{
				"# exchange 1 start",
				"# exchange 1 end",
				"# exchange 2 start",
				"# exchange 2 end",
				"# exchange 3 start",
				"# exchange 3 end",
			} {
				if strings.Count(transcript, want) != 1 {
					t.Errorf("expected one %q", want)
				}
			}
			if strings.Count(transcript, "> Content-Encoding:") != 1 ||
				strings.Count(transcript, "request body recorded before "+string(compression)+" compression") != 1 ||
				strings.Count(transcript, "< HTTP/1.1 415 Unsupported Media Type") != 1 ||
				strings.Count(transcript, "< HTTP/1.1 200 OK") != 2 {
				t.Error("wire recording lost the rejection, fallback or disabled compression state")
			}
			if strings.Count(transcript, `"token":"[REDACTED]"`) != 3 ||
				strings.Count(transcript, "Conversation item 299 stays unchanged.") != 3 ||
				strings.Contains(transcript, "private-fallback-token") {
				t.Error("fallback changed the request body or bypassed redaction")
			}
		})
	}
}
