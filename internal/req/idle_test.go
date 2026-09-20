package req_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crdx.org/io/internal/req"
	"crdx.org/io/pkg/agent"
)

func tricklingServer(t *testing.T, gap time.Duration, count int) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			flusher, canFlush := writer.(http.Flusher)
			if !canFlush {
				t.Error("the test server cannot flush")

				return
			}

			writer.WriteHeader(http.StatusOK)
			flusher.Flush()

			for range count {
				select {
				case <-request.Context().Done():
					return
				case <-time.After(gap):
				}

				_, _ = writer.Write([]byte("tick\n"))
				flusher.Flush()
			}
		},
	))

	t.Cleanup(server.Close)

	return server.URL
}

func TestAStreamThatGoesQuietIsStopped(t *testing.T) {
	url := tricklingServer(t, time.Hour, 1)

	body, _, err := req.NewStreaming(time.Second, 50*time.Millisecond).
		Stream(t.Context(), url, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("expected the stream to open: %v", err)
	}
	defer func() { _ = body.Close() }()

	_, err = io.ReadAll(body)

	var idle *req.IdleError
	if !errors.As(err, &idle) {
		t.Fatalf("expected the idleness to be reported, got %v", err)
	}

	if idle.After != 50*time.Millisecond {
		t.Errorf("expected the idle bound to be quantified, got %s", idle.After)
	}

	if !strings.Contains(idle.Error(), "the stream sent nothing") {
		t.Errorf("expected the silence to be named, got %q", idle)
	}
}

func TestIdlenessIsWorthAnotherAttempt(t *testing.T) {
	var retriable agent.Retriable

	if !errors.As(error(&req.IdleError{After: time.Minute}), &retriable) || !retriable.Retriable() {
		t.Error("expected an idle stream to be retriable")
	}
}

func TestAStreamStillArrivingIsLeftAlone(t *testing.T) {
	url := tricklingServer(t, 20*time.Millisecond, 10)

	body, _, err := req.NewStreaming(time.Second, 100*time.Millisecond).
		Stream(t.Context(), url, map[string]string{}, nil)
	if err != nil {
		t.Fatalf("expected the stream to open: %v", err)
	}
	defer func() { _ = body.Close() }()

	read, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("expected the whole stream, got %v", err)
	}

	if len(read) != len("tick\n")*10 {
		t.Errorf("expected every tick, got %q", read)
	}
}

func TestHeadersThatNeverArriveAreGivenUpOn(t *testing.T) {
	release := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(
		func(_ http.ResponseWriter, request *http.Request) {
			select {
			case <-request.Context().Done():
			case <-release:
			}
		},
	))

	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })

	_, _, err := req.NewStreaming(50*time.Millisecond, time.Hour).
		Stream(t.Context(), server.URL, map[string]string{}, nil)
	if err == nil {
		t.Fatal("expected the wait for the headers to end")
	}
}
