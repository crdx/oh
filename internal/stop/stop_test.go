package stop_test

import (
	"context"
	"errors"
	"testing"

	"crdx.org/oh/internal/stop"
)

func TestAReasonIsReadBackFromTheContext(t *testing.T) {
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(stop.Because("the user pressed escape"))

	if got := stop.Sentence(ctx); got != "the user pressed escape" {
		t.Errorf("got %q, want the reason it was cancelled with", got)
	}
	if got := stop.Phrase(ctx); got != " because the user pressed escape" {
		t.Errorf("got %q, want a clause to append", got)
	}
}

func TestAReasonStillLooksLikeACancellation(t *testing.T) {
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(stop.Because("the session is being replaced"))

	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Error("expected the context to report an ordinary cancellation")
	}
	if !errors.Is(context.Cause(ctx), context.Canceled) {
		t.Error("expected the cause to unwrap to a cancellation")
	}
}

func TestNothingIsSaidWithoutAReason(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if got := stop.Sentence(ctx); got != "" {
		t.Errorf("got %q, want nothing said", got)
	}
	if got := stop.Phrase(ctx); got != "" {
		t.Errorf("got %q, want no clause", got)
	}
}

func TestALiveContextSaysNothing(t *testing.T) {
	if got := stop.Sentence(t.Context()); got != "" {
		t.Errorf("got %q from a live context", got)
	}
}

func TestStoppedWorkIsExplainedInProse(t *testing.T) {
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(stop.Because("the user sent another message"))

	err := stop.Error(ctx, "the search")
	want := "the search stopped because the user sent another message"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, context.Canceled) {
		t.Error("expected the prose to answer as a cancellation")
	}
}

func TestStoppedWorkNeedsNoSubjectWhenTheCallNamesIt(t *testing.T) {
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(stop.Because("access changed"))

	err := stop.Error(ctx, "")
	if got, want := err.Error(), "stopped because access changed"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if !errors.Is(err, context.Canceled) {
		t.Error("expected the prose to answer as a cancellation")
	}
}

func TestUnexplainedStoppedWorkSaysOnlyThat(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if got := stop.Error(ctx, "the notification").Error(); got != "the notification stopped" {
		t.Errorf("got %q", got)
	}
}

func TestWorkThatRanOutOfTimeSaysSo(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 0)
	defer cancel()
	<-ctx.Done()

	err := stop.Error(ctx, "the search")
	if err.Error() != "the search ran out of time" {
		t.Errorf("got %q", err.Error())
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Error("expected the prose to answer as a deadline")
	}
}
