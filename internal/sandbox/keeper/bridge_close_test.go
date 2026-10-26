package keeper

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestClosingABridgeClosesEveryActiveConnection(t *testing.T) {
	listenerConfig := net.ListenConfig{}
	listener, err := listenerConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	connection, peer := net.Pipe()
	t.Cleanup(func() { _ = connection.Close() })
	t.Cleanup(func() { _ = peer.Close() })
	_, cancel := context.WithCancel(t.Context())
	crossing := &bridge{
		cancel:      cancel,
		listener:    listener,
		connections: map[net.Conn]struct{}{connection: {}},
	}
	if err := peer.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := crossing.Close(); err != nil {
		t.Fatal(err)
	}
	var buffer [1]byte
	if count, err := peer.Read(buffer[:]); count != 0 || !errors.Is(err, io.EOF) {
		t.Errorf("active connection remained readable after bridge close: %d bytes, %v", count, err)
	}
	if err := crossing.Close(); err != nil {
		t.Errorf("closing the bridge twice returned %v", err)
	}
}
