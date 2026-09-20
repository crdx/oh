package sse_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/sse"
)

func collect(stream string, last string) ([]string, error) {
	var payloads []string

	err := sse.Read(strings.NewReader(stream), func(payload string) (bool, error) {
		payloads = append(payloads, payload)
		return payload == last, nil
	})

	return payloads, err
}

func TestFramesArriveOneAtATime(t *testing.T) {
	payloads, err := collect("data: one\n\ndata: two\ndata: three\n\ndata: end\n\n", "end")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedPayloads := []string{"one", "twothree", "end"}
	if !slices.Equal(payloads, expectedPayloads) {
		t.Errorf("expected %v, got %v", expectedPayloads, payloads)
	}
}

func TestOtherFieldsAreIgnored(t *testing.T) {
	stream := ": keeping the line warm\nevent: message\nid: 1\ndata: one\nretry: 500\n\n"

	payloads, err := collect(stream, "one")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Equal(payloads, []string{"one"}) {
		t.Errorf("expected the data alone, got %v", payloads)
	}
}

func TestEmptyFramesAreNotDispatched(t *testing.T) {
	payloads, err := collect("\n\ndata: one\n\n\n\ndata: end\n\n", "end")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedPayloads := []string{"one", "end"}
	if !slices.Equal(payloads, expectedPayloads) {
		t.Errorf("expected %v, got %v", expectedPayloads, payloads)
	}
}

func TestCarriageReturnsAreNotPartOfThePayload(t *testing.T) {
	payloads, err := collect("data: one\r\n\r\n", "one")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Equal(payloads, []string{"one"}) {
		t.Errorf("expected the payload without the carriage return, got %q", payloads)
	}
}

func TestALoneCarriageReturnStaysInThePayload(t *testing.T) {
	payloads, err := collect("data: one\rtwo\n\n", "one\rtwo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Equal(payloads, []string{"one\rtwo"}) {
		t.Errorf("expected the payload whole, got %q", payloads)
	}
}

func TestTheLastFrameCountsWithoutItsBlankLine(t *testing.T) {
	payloads, err := collect("data: one\n\ndata: end", "end")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedPayloads := []string{"one", "end"}
	if !slices.Equal(payloads, expectedPayloads) {
		t.Errorf("expected %v, got %v", expectedPayloads, payloads)
	}
}

func TestAStreamThatEndsEarlyIsTruncated(t *testing.T) {
	payloads, err := collect("data: one\n\n", "end")
	if !errors.Is(err, sse.ErrTruncated) {
		t.Fatalf("expected a truncated stream to be reported, got %v", err)
	}

	if !slices.Equal(payloads, []string{"one"}) {
		t.Errorf("expected what did arrive, got %v", payloads)
	}
}

type failingReader struct {
	failure error
}

func (self failingReader) Read([]byte) (int, error) {
	return 0, self.failure
}

func TestAStreamThatBrokeIsWorthAskingAgainAfter(t *testing.T) {
	tests := map[string]struct {
		failure         error
		wantIsRetriable bool
	}{
		"a connection that went away": {failure: io.ErrUnexpectedEOF, wantIsRetriable: true},
		"a turn the user cancelled":   {failure: context.Canceled, wantIsRetriable: false},
		"a turn that ran out of time": {failure: context.DeadlineExceeded, wantIsRetriable: false},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := sse.Read(failingReader{failure: test.failure}, func(string) (bool, error) {
				return false, nil
			})

			if !errors.Is(err, test.failure) {
				t.Fatalf("expected what went wrong to be kept, got %v", err)
			}

			var retriable interface{ Retriable() bool }
			isRetriable := errors.As(err, &retriable) && retriable.Retriable()

			if isRetriable != test.wantIsRetriable {
				t.Errorf("expected retriable %t, got %t", test.wantIsRetriable, isRetriable)
			}
		})
	}
}

func TestReadingStopsWhenTheReaderIsDone(t *testing.T) {
	payloads, err := collect("data: end\n\ndata: two\n\n", "end")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Equal(payloads, []string{"end"}) {
		t.Errorf("expected nothing after the end, got %v", payloads)
	}
}

func fields(stream string) string {
	var out strings.Builder

	for line := range strings.SplitSeq(stream, "\n") {
		field, hasData := strings.CutPrefix(strings.TrimRight(line, "\r"), "data:")
		if hasData {
			out.WriteString(strings.TrimSpace(field))
		}
	}

	return out.String()
}

func FuzzRead(f *testing.F) {
	for _, seed := range []string{
		"data: one\n\n",
		"data: one\ndata: two\n\ndata: end",
		"\n\n\n",
		"data:\n\n",
		"data:   spaced   \r\n\r\n",
		": comment\nevent: message\nid: 1\nretry: 500\n\n",
		"data",
		"datadata: one\n\n",
		"\r\n",
		"data: \xff\x00\n\n",
		"data:0\r0",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, stream string) {
		var payloads []string

		err := sse.Read(strings.NewReader(stream), func(payload string) (bool, error) {
			payloads = append(payloads, payload)
			return false, nil
		})

		if !errors.Is(err, sse.ErrTruncated) {
			t.Fatalf("expected the stream to run out, got %v", err)
		}

		for _, payload := range payloads {
			if payload == "" {
				t.Error("expected no empty payload")
			}

			if strings.Contains(payload, "\n") {
				t.Errorf("expected no newline in a payload, got %q", payload)
			}
		}

		if joinedFields := strings.Join(payloads, ""); joinedFields != fields(stream) {
			t.Errorf("expected the data fields, got %q", joinedFields)
		}
	})
}

func TestTheReadersOwnFailureIsHandedBack(t *testing.T) {
	refusal := errors.New("that made no sense")

	err := sse.Read(strings.NewReader("data: one\n\ndata: two\n\n"),
		func(string) (bool, error) { return false, refusal })

	if !errors.Is(err, refusal) {
		t.Errorf("expected the reader's own failure, got %v", err)
	}
}
