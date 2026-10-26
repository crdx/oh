package sim_test

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klauspost/compress/zstd"

	"crdx.org/oh/internal/sim"
)

func TestSimulationReadsEachRequestEncoding(t *testing.T) {
	plain := []byte(`{"messages":[{"role":"user","content":"a conversation"}]}`)
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = encoder.Close() }()
	packed := encoder.EncodeAll(plain, nil)

	for _, test := range []struct {
		encoding string
		body     []byte
	}{
		{"", plain},
		{"identity", plain},
		{"gzip", buffer.Bytes()},
		{"zstd", packed},
	} {
		t.Run(test.encoding, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/messages", bytes.NewReader(test.body))
			request.Header.Set("Content-Encoding", test.encoding)
			body, err := sim.ReadBody(request)
			if err != nil || !bytes.Equal(body, plain) {
				t.Errorf("got %q: %v", body, err)
			}
		})
	}
}

func TestSimulationRefusesUnknownAndBrokenEncodings(t *testing.T) {
	for _, encoding := range []string{"br", "gzip", "zstd"} {
		t.Run(encoding, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/messages", bytes.NewReader([]byte("not compressed")))
			request.Header.Set("Content-Encoding", encoding)
			if _, err := sim.ReadBody(request); err == nil {
				t.Error("expected the unsupported or malformed encoding to fail")
			}
		})
	}
}
