package req_test

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"crdx.org/oh/internal/req"
)

func TestLiveResponseTimeoutOnlyChangesSubsequentRequests(t *testing.T) {
	var calls atomic.Int64
	arrived := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		if calls.Add(1) == 1 {
			close(arrived)
			select {
			case <-release:
				_, _ = io.WriteString(writer, "finished")
			case <-request.Context().Done():
			}
		} else {
			<-request.Context().Done()
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	var timeouts atomic.Pointer[req.StreamTimeouts]
	timeouts.Store(&req.StreamTimeouts{Response: time.Second, Idle: time.Second})
	client := req.NewStreaming(time.Minute, time.Minute)
	client.TakeStreamTimeoutsFrom(func() req.StreamTimeouts { return *timeouts.Load() })
	first := make(chan error, 1)
	go func() {
		body, _, err := client.Stream(t.Context(), server.URL, nil, nil)
		if err == nil {
			defer func() { _ = body.Close() }()
			_, err = io.ReadAll(body)
		}
		first <- err
	}()
	<-arrived
	timeouts.Store(&req.StreamTimeouts{Response: 30 * time.Millisecond, Idle: time.Second})
	time.Sleep(80 * time.Millisecond)
	releaseOnce.Do(func() { close(release) })
	if err := <-first; err != nil {
		t.Fatalf("the open request lost its original timeout: %v", err)
	}
	started := time.Now()
	body, _, err := client.Stream(t.Context(), server.URL, nil, nil)
	if body != nil {
		_ = body.Close()
	}
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("the next request did not time out: %v", err)
	}
	if took := time.Since(started); took >= time.Second {
		t.Errorf("the next request kept its old timeout: %s", took)
	}
}

func TestLiveStreamIdleTimeoutOnlyChangesSubsequentStreams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		writer.WriteHeader(http.StatusOK)
		if err := http.NewResponseController(writer).Flush(); err != nil {
			t.Error(err)
			return
		}
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)
	var timeouts atomic.Pointer[req.StreamTimeouts]
	timeouts.Store(&req.StreamTimeouts{Response: time.Second, Idle: 200 * time.Millisecond})
	client := req.NewStreaming(time.Minute, time.Minute)
	client.CheckIdleEvery(5 * time.Millisecond)
	client.TakeStreamTimeoutsFrom(func() req.StreamTimeouts { return *timeouts.Load() })
	body, _, err := client.Stream(t.Context(), server.URL, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	timeouts.Store(&req.StreamTimeouts{Response: time.Second, Idle: 30 * time.Millisecond})
	_, err = io.ReadAll(body)
	_ = body.Close()
	var idle *req.IdleError
	if !errors.As(err, &idle) || idle.After != 200*time.Millisecond {
		t.Fatalf("the open stream lost its original timeout: %v", err)
	}
	body, _, err = client.Stream(t.Context(), server.URL, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(body)
	_ = body.Close()
	if !errors.As(err, &idle) || idle.After != 30*time.Millisecond {
		t.Errorf("the next stream kept its old timeout: %v", err)
	}
}
