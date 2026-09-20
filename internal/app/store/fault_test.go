package store

import (
	"errors"
	"testing"
)

type closeFailingCanonicalWriter struct {
	canonicalWriter

	failure error
}

func (self closeFailingCanonicalWriter) Close() error {
	return self.failure
}

func TestCanonicalCloseFailureIsReturned(t *testing.T) {
	log, err := Create(t.TempDir(), Meta{})
	if err != nil {
		t.Fatal(err)
	}

	failure := errors.New("canonical close failed")
	log.innerWriter = closeFailingCanonicalWriter{
		canonicalWriter: log.innerWriter,
		failure:         failure,
	}
	if err := log.Close(); !errors.Is(err, failure) {
		t.Fatalf("got %v, want %v", err, failure)
	}
}
