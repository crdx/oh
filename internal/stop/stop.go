package stop

import (
	"context"
	"errors"
	"strings"
)

type stopError struct {
	sentence string
	cause    error
}

func (self stopError) Error() string { return self.sentence }
func (self stopError) Unwrap() error { return self.cause }

func Because(sentence string) error {
	return stopError{sentence: sentence, cause: context.Canceled}
}

func BecauseOf(sentence string, cause error) error {
	return stopError{sentence: sentence, cause: cause}
}

func Sentence(ctx context.Context) string {
	if why, ok := errors.AsType[stopError](context.Cause(ctx)); ok {
		return why.sentence
	}

	return ""
}

func Phrase(ctx context.Context) string {
	if sentence := Sentence(ctx); sentence != "" {
		return " because " + sentence
	}

	return ""
}

func Error(ctx context.Context, subject string) error {
	prefix := ""
	if subject != "" {
		prefix = subject + " "
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return stopError{sentence: prefix + "reached its time limit", cause: context.DeadlineExceeded}
	}

	return stopError{sentence: prefix + "stopped" + Phrase(ctx), cause: context.Canceled}
}

func ErrorFrom(ctx context.Context, subject string, err error) error {
	if err != nil && ctx.Err() != nil {
		return Error(ctx, subject)
	}

	return err
}

func Report(output string, err error) string {
	if strings.TrimSpace(output) == "" {
		return err.Error()
	}

	return strings.TrimRight(output, "\n") + "\nnote: " + strings.TrimSuffix(err.Error(), ".") + "."
}
