package harness

import (
	"bytes"
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

func TestGoldenCompressionWireRecords(t *testing.T) {
	passes := map[string]func() string{}
	for _, contentEncoding := range []string{"", "identity", "IDENTITY", "gzip", "zstd"} {
		for _, shouldReject := range []bool{false, true} {
			if shouldReject && (contentEncoding == "" || strings.EqualFold(contentEncoding, "identity")) {
				continue
			}
			name := fmt.Sprintf("encoding %q fallback %t", contentEncoding, shouldReject)
			passes[name] = func() string { return compressionWireRecord(t, contentEncoding, shouldReject) }
		}
	}
	compareWithGolden(t, "compression-wire-records", ".txt", passes)
}

func compressionWireRecord(t *testing.T, contentEncoding string, shouldReject bool) string {
	t.Helper()

	at := time.Date(2026, time.March, 18, 12, 0, 0, 0, time.UTC)
	directory := t.TempDir()
	path := filepath.Join(directory, "wire.http.zst")
	recorder, err := wire.Open(path, wire.Meta{
		Name:      "tame-impala",
		Model:     "fake",
		Effort:    "high",
		Provider:  "fixture",
		Workspace: "/workspace",
		StartedAt: at,
	}, func(err error) { t.Error(err) })
	if err != nil {
		t.Fatal(err)
	}
	attempts := 1
	if shouldReject {
		attempts = 2
	}
	for i := range attempts {
		header := http.Header{
			"Cookie":       {"private-test-token"},
			"Content-Type": {"application/json"},
		}
		if contentEncoding != "" && i == 0 {
			header.Set("Content-Encoding", contentEncoding)
		}
		startedAt := at.Add(time.Duration(i*3) * time.Second)
		exchange := recorder.Start(req.Request{
			StartedAt: startedAt,
			Method:    http.MethodPost,
			URL:       "https://endpoint/v1/conversation",
			Protocol:  "HTTP/1.1",
			Header:    header,
			Body:      []byte(`{"messages":[{"role":"user","content":"unchanged input"}],"token":"private-test-key"}`),
		})
		status := http.StatusOK
		body := `{"answer":"done"}`
		if shouldReject && i == 0 {
			status = http.StatusUnsupportedMediaType
			body = `{"error":{"message":"unsupported content encoding"}}`
		}
		exchange.Response(req.Response{
			ReceivedAt: startedAt.Add(time.Second),
			Protocol:   "HTTP/1.1",
			Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
			Code:       status,
			Header:     http.Header{"Content-Type": {"application/json"}},
		})
		exchange.Body(startedAt.Add(time.Second), []byte(body))
		exchange.Finish(startedAt.Add(2*time.Second), nil, false)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	storedBytes, err := root.ReadFile("wire.http.zst")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zstd.NewReader(bytes.NewReader(storedBytes), zstd.WithDecoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	transcript := string(body)
	if strings.Contains(transcript, "private-test-token") || strings.Contains(transcript, "private-test-key") {
		t.Fatal("wire recording exposed a credential")
	}
	if strings.Count(transcript, `"content":"unchanged input"`) != attempts {
		t.Fatal("wire recording lost or rewrote a request body")
	}
	return transcript
}
