package agent

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

type FailureKind string

const (
	GenericFailure    FailureKind = "generic_error"
	HTTPStatusFailure FailureKind = "http_status"
)

type Failure struct {
	Kind       FailureKind `json:"kind"`
	Context    string      `json:"context,omitempty"`
	Message    string      `json:"message,omitempty"`
	HTTPStatus int         `json:"http_status,omitempty"`
	Code       string      `json:"code,omitempty"`
	Body       string      `json:"body,omitempty"`
	MediaType  string      `json:"media_type,omitempty"`
}

type failureDescriber interface {
	error

	DescribeFailure() Failure
}

func FailureFrom(err error) *Failure {
	if err == nil {
		return nil
	}

	if describer, isDescribed := errors.AsType[failureDescriber](err); isDescribed {
		failure := describer.DescribeFailure()
		failure.Context = failureContext(err, describer)
		return &failure
	}

	return &Failure{Kind: GenericFailure, Message: err.Error()}
}

func FailureText(event Event) string {
	if event.Failure != nil {
		return event.Failure.Text()
	}

	return event.Text
}

var contextExceededCodes = map[string]bool{
	"context_length_exceeded":   true,
	"context_window_exceeded":   true,
	"exceed_context_size_error": true,
}

var contextExceededPhrases = []string{
	"context length",
	"context size",
	"context window",
	"input token count",
	"prompt is too long",
	"prompt too long",
}

func (self Failure) IsContextExceeded() bool {
	if self.Kind != HTTPStatusFailure || self.HTTPStatus < 400 || self.HTTPStatus >= 500 {
		return false
	}

	if contextExceededCodes[strings.ToLower(self.Code)] {
		return true
	}

	wording := strings.ToLower(self.Message + " " + self.Body)

	return slices.ContainsFunc(contextExceededPhrases, func(phrase string) bool {
		return strings.Contains(wording, phrase)
	})
}

func (self Failure) Text() string {
	message := self.Message
	if self.Kind == HTTPStatusFailure && message == "" {
		message = fmt.Sprintf("request failed with status %d", self.HTTPStatus)
		if body := strings.TrimSpace(self.Body); body != "" {
			message += ": " + body
		}
	}

	if self.Context != "" && message != "" {
		return self.Context + ": " + message
	}

	return message
}

func failureContext(outer error, inner error) string {
	context, found := strings.CutSuffix(outer.Error(), inner.Error())
	if !found {
		return ""
	}

	return strings.TrimSuffix(context, ": ")
}
