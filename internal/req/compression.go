package req

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
)

type Compression string

const (
	Gzip Compression = "gzip"
	Zstd Compression = "zstd"
)

const (
	minimumCompressionBytes = 4096
	maximumCompressionRatio = 100
)

var requestEncoder = sync.OnceValues(func() (*zstd.Encoder, error) {
	return zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(3)),
		zstd.WithEncoderConcurrency(1),
	)
})

func (self *Client) CompressWith(compression Compression) {
	self.compression = compression
}

func (self *Client) compress(body []byte, header http.Header) ([]byte, error) {
	if self.compression == "" || self.isCompressionRejected.Load() ||
		len(body) < minimumCompressionBytes || header.Get("Content-Encoding") != "" {
		return body, nil
	}

	var compressedBody []byte
	switch self.compression {
	case Gzip:
		var buffer bytes.Buffer
		writer := gzip.NewWriter(&buffer)
		if _, err := writer.Write(body); err != nil {
			return nil, fmt.Errorf("compress request: %w", err)
		}
		if err := writer.Close(); err != nil {
			return nil, fmt.Errorf("compress request: %w", err)
		}
		compressedBody = buffer.Bytes()
	case Zstd:
		encoder, err := requestEncoder()
		if err != nil {
			return nil, fmt.Errorf("create request compressor: %w", err)
		}
		compressedBody = encoder.EncodeAll(body, nil)
		if len(body) > len(compressedBody)*maximumCompressionRatio {
			return body, nil
		}
	default:
		return nil, fmt.Errorf("unknown request compression %q", self.compression)
	}

	if len(compressedBody) >= len(body) {
		return body, nil
	}

	header.Set("Content-Encoding", string(self.compression))
	return compressedBody, nil
}

func compressionWasRejected(err error) bool {
	var refusal *StatusError
	if !errors.As(err, &refusal) {
		return false
	}
	if refusal.Status == http.StatusUnsupportedMediaType {
		return true
	}
	if refusal.Status != http.StatusBadRequest {
		return false
	}

	message := strings.ToLower(refusal.Message)
	for _, reason := range []string{
		"unsupported content-encoding",
		"unsupported content encoding",
		"unsupported encoding",
		"invalid content-encoding",
		"invalid content encoding",
		"the request body is not valid json",
		"failed to parse request: could not parse request body as json",
	} {
		if strings.HasPrefix(message, reason) {
			return true
		}
	}
	return false
}
