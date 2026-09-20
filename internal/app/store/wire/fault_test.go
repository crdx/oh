package wire

import (
	"strings"
	"testing"
	"time"

	"crdx.org/io/internal/req"
)

func TestAppendFailureDisablesRecordingAndWarnsOnce(t *testing.T) {
	var warnings []error
	recorder, err := Open(t.TempDir()+"/wire.http", Meta{}, func(err error) {
		warnings = append(warnings, err)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.file.Close(); err != nil {
		t.Fatal(err)
	}

	request := req.Request{StartedAt: time.Now(), Method: "POST", URL: "https://example.com"}
	recorder.Start(request)
	recorder.Start(request)

	if len(warnings) != 1 {
		t.Fatalf("got %d warnings, want 1", len(warnings))
	}
	if !strings.Contains(warnings[0].Error(), "wire.http recording disabled") {
		t.Errorf("unexpected warning: %v", warnings[0])
	}
	if !recorder.hasFailed || recorder.file != nil {
		t.Errorf("recorder remained active after failure: failed=%t file=%v", recorder.hasFailed, recorder.file)
	}
}
