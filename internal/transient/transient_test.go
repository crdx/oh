package transient_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"crdx.org/oh/internal/transient"
)

func TestOnlyWhatTheCallerDidNotDoIsWorthAnotherAttempt(t *testing.T) {
	tests := map[string]struct {
		failure         error
		wantIsRetriable bool
	}{
		"a connection that went away": {failure: io.ErrUnexpectedEOF, wantIsRetriable: true},
		"a name that would not dial":  {failure: errors.New("dial tcp: connection refused"), wantIsRetriable: true},
		"a turn the user cancelled":   {failure: context.Canceled, wantIsRetriable: false},
		"a turn that ran out of time": {failure: context.DeadlineExceeded, wantIsRetriable: false},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			wrapped := transient.Wrap(test.failure)

			if !errors.Is(wrapped, test.failure) {
				t.Fatalf("expected what went wrong to be kept, got %v", wrapped)
			}

			var retriable interface{ Retriable() bool }
			isRetriable := errors.As(wrapped, &retriable) && retriable.Retriable()

			if isRetriable != test.wantIsRetriable {
				t.Errorf("expected retriable %t, got %t", test.wantIsRetriable, isRetriable)
			}
		})
	}
}

func TestNothingWrongIsNothingToWrap(t *testing.T) {
	if err := transient.Wrap(nil); err != nil {
		t.Errorf("expected nothing, got %v", err)
	}
}
