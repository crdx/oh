package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/metrics"
	"crdx.org/oh/internal/req"
	"crdx.org/oh/pkg/agent"
)

type compressionFailingCanonicalWriter struct {
	canonicalWriter

	failure error
}

func (self compressionFailingCanonicalWriter) Event(event agent.Event) (time.Time, error) {
	if event.Kind == metrics.RequestCompressionRejected {
		return time.Time{}, self.failure
	}
	return self.canonicalWriter.Event(event)
}

func TestACompressionDiagnosticWriteFailureIsReportedWithoutInventingAStoredRejection(t *testing.T) {
	directory := t.TempDir()
	log, err := Create(directory, Meta{Provider: "codex", Model: "fake", ShouldSkipWire: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	if err := log.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("diagnostic write failed")
	log.innerWriter = compressionFailingCanonicalWriter{canonicalWriter: log.innerWriter, failure: failure}
	observer, isCounting := log.Observer().(req.CompressionObserver)
	if !isCounting {
		t.Fatal("the session observer does not record compression rejections")
	}
	observer.CompressionRejected()
	warnings := log.TakeWarnings()
	if len(warnings) != 1 || !errors.Is(warnings[0], failure) || !strings.Contains(warnings[0].Error(), "request statistics could not be stored") {
		t.Fatalf("the diagnostic write failure was not reported: %v", warnings)
	}
	if warnings := log.TakeWarnings(); len(warnings) != 0 {
		t.Errorf("the same storage warning was returned again: %v", warnings)
	}
	stored, err := Read(directory, log.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Events) != 1 || stored.Events[0].Kind != agent.UserMessageEvent {
		t.Errorf("a failed diagnostic write changed the journal: %+v", stored.Events)
	}
}
