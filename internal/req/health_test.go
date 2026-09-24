package req

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type severableProxy struct {
	listener    net.Listener
	target      string
	connections atomic.Int64

	mutex   sync.Mutex
	severed []*atomic.Bool
}

func newSeverableProxy(t *testing.T, target string) *severableProxy {
	t.Helper()

	var listening net.ListenConfig

	listener, err := listening.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("expected the proxy to listen: %v", err)
	}

	proxy := &severableProxy{listener: listener, target: target}
	t.Cleanup(func() { _ = listener.Close() })

	go proxy.accept(t.Context())

	return proxy
}

func (self *severableProxy) accept(ctx context.Context) {
	for {
		client, err := self.listener.Accept()
		if err != nil {
			return
		}

		var dialer net.Dialer

		server, err := dialer.DialContext(ctx, "tcp", self.target)
		if err != nil {
			_ = client.Close()

			continue
		}

		self.connections.Add(1)

		var isSevered atomic.Bool

		self.mutex.Lock()
		self.severed = append(self.severed, &isSevered)
		self.mutex.Unlock()

		go forwardUnlessSevered(client, server, &isSevered)
		go forwardUnlessSevered(server, client, &isSevered)
	}
}

func forwardUnlessSevered(source net.Conn, destination net.Conn, isSevered *atomic.Bool) {
	defer func() { _ = destination.Close() }()

	buffer := make([]byte, 32*1024)
	for {
		count, err := source.Read(buffer)
		if count > 0 && !isSevered.Load() {
			if _, err := destination.Write(buffer[:count]); err != nil {
				return
			}
		}

		if err != nil {
			return
		}
	}
}

func (self *severableProxy) severEveryConnection() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	for _, isSevered := range self.severed {
		isSevered.Store(true)
	}
}

func (self *severableProxy) address() string {
	return "https://" + self.listener.Addr().String()
}

func standardTransport(t *testing.T, client *http.Client) *http.Transport {
	t.Helper()

	transport, isStandard := client.Transport.(*http.Transport)
	if !isStandard {
		t.Fatalf("expected the standard transport, got %T", client.Transport)
	}

	return transport
}

func TestAStreamingConnectionIsCheckedForSignsOfLife(t *testing.T) {
	client := NewStreaming(time.Minute, time.Hour)

	settings := standardTransport(t, client.http).HTTP2
	if settings == nil {
		t.Fatal("expected the connection to be checked, got no HTTP/2 settings")
	}

	if settings.SendPingTimeout != healthCheckAfter {
		t.Errorf("expected a check after %s of silence, got %s", healthCheckAfter, settings.SendPingTimeout)
	}

	if settings.PingTimeout != healthCheckAnswerWithin {
		t.Errorf("expected the check to be answered within %s, got %s", healthCheckAnswerWithin, settings.PingTimeout)
	}
}

func TestAConnectionThatStoppedAnsweringIsAbandonedForAFreshOne(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			flusher, canFlush := writer.(http.Flusher)
			if !canFlush {
				t.Error("the test server cannot flush")

				return
			}

			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("tick\n"))
			flusher.Flush()

			if request.URL.Path == "/quick" {
				return
			}

			<-request.Context().Done()
		},
	))
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)

	proxy := newSeverableProxy(t, server.Listener.Addr().String())

	client := NewStreaming(time.Minute, time.Hour)
	transport := standardTransport(t, client.http)
	transport.TLSClientConfig = standardTransport(t, server.Client()).TLSClientConfig.Clone()
	transport.HTTP2.SendPingTimeout = 100 * time.Millisecond
	transport.HTTP2.PingTimeout = 100 * time.Millisecond

	body, _, err := client.Stream(t.Context(), proxy.address()+"/stream", map[string]string{}, nil)
	if err != nil {
		t.Fatalf("expected the stream to open: %v", err)
	}
	defer func() { _ = body.Close() }()

	if _, err := body.Read(make([]byte, 16)); err != nil {
		t.Fatalf("expected the stream to begin: %v", err)
	}

	proxy.severEveryConnection()

	failure := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(body)
		failure <- err
	}()

	select {
	case err := <-failure:
		if err == nil || errors.Is(err, io.EOF) {
			t.Fatalf("expected the lost connection to be reported, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("expected a connection that stopped answering to be given up on, but the stream still waits")
	}

	quick, _, err := client.Stream(t.Context(), proxy.address()+"/quick", map[string]string{}, nil)
	if err != nil {
		t.Fatalf("expected the next request to reach the endpoint: %v", err)
	}
	defer func() { _ = quick.Close() }()

	if _, err := io.ReadAll(quick); err != nil {
		t.Fatalf("expected the next response to arrive whole: %v", err)
	}

	if connections := proxy.connections.Load(); connections != 2 {
		t.Errorf("expected the next request to open a fresh connection, got %d connections", connections)
	}
}
