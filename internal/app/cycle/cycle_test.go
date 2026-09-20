package cycle_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"crdx.org/oh/internal/app/cycle"
)

func TestHooksDispatchInRegistrationOrderAndIgnoreReportedErrors(t *testing.T) {
	var calls []string
	var reported []error
	hooks := cycle.NewHooks(func(err error) { reported = append(reported, err) })
	fixtureError := errors.New("fixture")

	hooks.OnSessionStarted(func(context.Context, cycle.SessionStarted) error {
		calls = append(calls, "first")
		return fixtureError
	})
	hooks.OnSessionStarted(func(context.Context, cycle.SessionStarted) error {
		calls = append(calls, "second")
		return nil
	})

	hooks.EmitSessionStarted(t.Context(), cycle.SessionStarted{})
	if !slices.Equal(calls, []string{"first", "second"}) {
		t.Errorf("got calls %v", calls)
	}
	if len(reported) != 1 || !errors.Is(reported[0], fixtureError) {
		t.Errorf("got reported errors %v", reported)
	}
}

func TestEverySessionHookReceivesItsTypedEvent(t *testing.T) {
	hooks := cycle.NewHooks(func(err error) { t.Error(err) })
	var calls []string
	hooks.OnSessionStarting(func(context.Context, cycle.SessionStarting) error {
		calls = append(calls, "starting")
		return nil
	})
	hooks.OnSessionStopping(func(context.Context, cycle.SessionStopping) error {
		calls = append(calls, "stopping")
		return nil
	})
	hooks.OnSessionStopped(func(context.Context, cycle.SessionStopped) error {
		calls = append(calls, "stopped")
		return nil
	})

	hooks.EmitSessionStarting(t.Context(), cycle.SessionStarting{})
	hooks.EmitSessionStopping(t.Context(), cycle.SessionStopping{})
	hooks.EmitSessionStopped(t.Context(), cycle.SessionStopped{})
	if !slices.Equal(calls, []string{"starting", "stopping", "stopped"}) {
		t.Errorf("got calls %v", calls)
	}
}
