package agent_test

import (
	"errors"
	"fmt"
	"testing"

	"crdx.org/io/pkg/agent"
)

type describedFailureError struct{}

func (describedFailureError) Error() string { return "rendered prose that should not be stored" }

func (describedFailureError) DescribeFailure() agent.Failure {
	return agent.Failure{
		Kind:       agent.HTTPStatusFailure,
		HTTPStatus: 598,
		Code:       "future_edge_error",
		MediaType:  "text/html",
	}
}

func TestFailureFromKeepsStructuredDetailsThroughAWrappedError(t *testing.T) {
	failure := agent.FailureFrom(fmt.Errorf("provider failed: %w", describedFailureError{}))
	if failure == nil {
		t.Fatal("expected a failure")
	}

	want := agent.Failure{
		Kind:       agent.HTTPStatusFailure,
		Context:    "provider failed",
		HTTPStatus: 598,
		Code:       "future_edge_error",
		MediaType:  "text/html",
	}
	if *failure != want {
		t.Errorf("got %+v, want %+v", *failure, want)
	}
}

func TestFailureFromFallsBackToAnUnhandledErrorsMessage(t *testing.T) {
	failure := agent.FailureFrom(errors.New("something entirely new happened"))
	if failure == nil {
		t.Fatal("expected a failure")
	}
	if failure.Kind != agent.GenericFailure || failure.Message != "something entirely new happened" {
		t.Errorf("got %+v", *failure)
	}
}

func TestFailureTextRendersSemanticFieldsAndOldText(t *testing.T) {
	tests := map[string]struct {
		event agent.Event
		want  string
	}{
		"endpoint message": {
			event: agent.Event{Failure: &agent.Failure{
				Kind:       agent.HTTPStatusFailure,
				Message:    "slow down",
				HTTPStatus: 429,
			}},
			want: "slow down",
		},
		"http body": {
			event: agent.Event{Failure: &agent.Failure{
				Kind:       agent.HTTPStatusFailure,
				HTTPStatus: 598,
				Body:       "future details",
			}},
			want: "request failed with status 598: future details",
		},
		"http status": {
			event: agent.Event{Failure: &agent.Failure{
				Kind:       agent.HTTPStatusFailure,
				HTTPStatus: 520,
				MediaType:  "text/html",
			}},
			want: "request failed with status 520",
		},
		"http context": {
			event: agent.Event{Failure: &agent.Failure{
				Kind:       agent.HTTPStatusFailure,
				Context:    "refresh credentials",
				HTTPStatus: 500,
				Body:       `{"error":"server_error"}`,
			}},
			want: `refresh credentials: request failed with status 500: {"error":"server_error"}`,
		},
		"generic error": {
			event: agent.Event{Failure: &agent.Failure{
				Kind:    agent.GenericFailure,
				Message: "the stream ended",
			}},
			want: "the stream ended",
		},
		"old text": {
			event: agent.Event{Text: "an error recorded before structured failures"},
			want:  "an error recorded before structured failures",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := agent.FailureText(test.event); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestIsContextExceededRecognisesEveryEndpointsWording(t *testing.T) {
	for _, test := range []struct {
		name    string
		failure agent.Failure
		want    bool
	}{
		{
			name: "llama.cpp names its own type",
			failure: agent.Failure{
				Kind:       agent.HTTPStatusFailure,
				HTTPStatus: 400,
				Code:       "exceed_context_size_error",
				Message:    "request (264481 tokens) exceeds the available context size (262144 tokens)",
			},
			want: true,
		},
		{
			name: "OpenAI names the code",
			failure: agent.Failure{
				Kind:       agent.HTTPStatusFailure,
				HTTPStatus: 400,
				Code:       "context_length_exceeded",
				Message:    "This model's maximum context length is 128000 tokens.",
			},
			want: true,
		},
		{
			name: "Anthropic says only that the prompt is too long",
			failure: agent.Failure{
				Kind:       agent.HTTPStatusFailure,
				HTTPStatus: 400,
				Code:       "invalid_request_error",
				Message:    "prompt is too long: 250000 tokens > 200000 maximum",
			},
			want: true,
		},
		{
			name: "Gemini counts the input tokens",
			failure: agent.Failure{
				Kind:       agent.HTTPStatusFailure,
				HTTPStatus: 400,
				Code:       "INVALID_ARGUMENT",
				Message:    "The input token count (1100000) exceeds the maximum allowed",
			},
			want: true,
		},
		{
			name: "an unparsed body still carries the wording",
			failure: agent.Failure{
				Kind:       agent.HTTPStatusFailure,
				HTTPStatus: 400,
				Body:       "the context window is full",
			},
			want: true,
		},
		{
			name: "an ordinary refusal is not one",
			failure: agent.Failure{
				Kind:       agent.HTTPStatusFailure,
				HTTPStatus: 400,
				Code:       "invalid_request_error",
				Message:    "messages: at least one message is required",
			},
			want: false,
		},
		{
			name: "a rate limit is not one",
			failure: agent.Failure{
				Kind:       agent.HTTPStatusFailure,
				HTTPStatus: 429,
				Code:       "rate_limit_exceeded",
				Message:    "slow down",
			},
			want: false,
		},
		{
			name: "a server fault is not one",
			failure: agent.Failure{
				Kind:       agent.HTTPStatusFailure,
				HTTPStatus: 500,
				Message:    "the context length service fell over",
			},
			want: false,
		},
		{
			name:    "a generic failure is not one",
			failure: agent.Failure{Kind: agent.GenericFailure, Message: "the context length is wrong"},
			want:    false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.failure.IsContextExceeded(); got != test.want {
				t.Errorf("got %v, want %v", got, test.want)
			}
		})
	}
}
