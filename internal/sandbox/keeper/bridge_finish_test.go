package keeper

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func joinTo(t *testing.T, far net.Listener) (*bridge, string) {
	t.Helper()

	var listenConfig net.ListenConfig
	near, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback sockets are unavailable: %v", err)
	}

	joined := newBridge(t.Context(), near, func(ctx context.Context) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "tcp", far.Addr().String())
	})
	t.Cleanup(func() { _ = joined.Close() })

	return joined, near.Addr().String()
}

func shortenFinishedIdleTimeout(t *testing.T, timeout time.Duration) {
	t.Helper()
	previous := finishedIdleTimeout
	finishedIdleTimeout = timeout
	t.Cleanup(func() { finishedIdleTimeout = previous })
}

func TestAConnectionThatTheFarSideNeverAnswersFreesItsSlotOnceTheNearSideCloses(t *testing.T) {
	shortenFinishedIdleTimeout(t, 100*time.Millisecond)

	var listenConfig net.ListenConfig
	far, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback sockets are unavailable: %v", err)
	}
	defer func() { _ = far.Close() }()

	held := make(chan net.Conn, 1)
	go func() {
		connection, err := far.Accept()
		if err != nil {
			return
		}
		var buffer [64]byte
		_, _ = connection.Read(buffer[:])
		held <- connection
	}()
	defer func() {
		select {
		case connection := <-held:
			_ = connection.Close()
		default:
		}
	}()

	joined, address := joinTo(t, far)

	var dialer net.Dialer
	connection, err := dialer.DialContext(t.Context(), "tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write([]byte("GET /hang")); err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()

	deadline := time.Now().Add(2 * time.Second)
	for len(joined.slots) > 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the slot of a closed connection was still held after %s", 2*time.Second)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestASlowAnswerToAHalfClosedRequestStillArrives(t *testing.T) {
	shortenFinishedIdleTimeout(t, 200*time.Millisecond)

	var listenConfig net.ListenConfig
	far, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback sockets are unavailable: %v", err)
	}
	defer func() { _ = far.Close() }()

	go func() {
		connection, err := far.Accept()
		if err != nil {
			return
		}
		defer func() { _ = connection.Close() }()
		_, _ = io.ReadAll(connection)
		for _, part := range []string{"slow ", "but ", "steady"} {
			time.Sleep(100 * time.Millisecond)
			_, _ = connection.Write([]byte(part))
		}
	}()

	_, address := joinTo(t, far)

	var dialer net.Dialer
	connection, err := dialer.DialContext(t.Context(), "tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	if _, err := connection.Write([]byte("ask")); err != nil {
		t.Fatal(err)
	}
	if tcp, isTCP := connection.(*net.TCPConn); isTCP {
		_ = tcp.CloseWrite()
	}

	answer, err := io.ReadAll(connection)
	if err != nil {
		t.Fatal(err)
	}
	if string(answer) != "slow but steady" {
		t.Errorf("got %q, want the whole slow answer", answer)
	}
}
