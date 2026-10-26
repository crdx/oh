package req

import (
	"bytes"
	"math/rand/v2"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCompressionSkipsBodiesThatDoNotShrink(t *testing.T) {
	body := make([]byte, 8192)
	if _, err := rand.NewChaCha8([32]byte{}).Read(body); err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(body)
	for _, compression := range []Compression{Gzip, Zstd} {
		t.Run(string(compression), func(t *testing.T) {
			client := New(time.Second)
			client.CompressWith(compression)
			header := http.Header{}
			wire, err := client.compress(body, header)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(wire, original) || !bytes.Equal(body, original) || header.Get("Content-Encoding") != "" {
				t.Error("an incompressible body or its encoding changed")
			}
		})
	}
}

func TestCompressionRejectsUnknownAlgorithms(t *testing.T) {
	client := New(time.Second)
	client.CompressWith("unknown")
	_, err := client.compress([]byte(strings.Repeat("body", 2048)), http.Header{})
	if err == nil || !strings.Contains(err.Error(), "unknown request compression") {
		t.Fatalf("got %v, want an unsupported algorithm error", err)
	}
}
