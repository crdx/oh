package sim

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/klauspost/compress/zstd"
)

const maximumRequestBytes = 128 << 20

func ReadBody(request *http.Request) ([]byte, error) {
	var body io.Reader = request.Body
	switch contentEncoding := strings.ToLower(strings.TrimSpace(request.Header.Get("Content-Encoding"))); contentEncoding {
	case "", "identity":
	case "gzip":
		reader, err := gzip.NewReader(request.Body)
		if err != nil {
			return nil, err
		}
		defer func() { _ = reader.Close() }()
		body = reader
	case "zstd":
		reader, err := zstd.NewReader(request.Body,
			zstd.WithDecoderConcurrency(1),
			zstd.WithDecoderMaxMemory(maximumRequestBytes),
		)
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		body = reader
	default:
		return nil, fmt.Errorf("unsupported request encoding %q", contentEncoding)
	}

	raw, err := io.ReadAll(io.LimitReader(body, maximumRequestBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maximumRequestBytes {
		return nil, fmt.Errorf("request exceeds %d bytes", maximumRequestBytes)
	}
	return raw, nil
}
