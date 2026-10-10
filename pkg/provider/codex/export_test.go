package codex

import (
	"context"
	"net"
	"testing"
)

func ListenOnAnyPort(t *testing.T) func() string {
	t.Helper()

	var address string
	callbackListener = func(ctx context.Context) (net.Listener, error) {
		var config net.ListenConfig
		listener, err := config.Listen(ctx, "tcp", "127.0.0.1:0")
		if err == nil {
			address = listener.Addr().String()
		}
		return listener, err
	}
	t.Cleanup(func() { callbackListener = listenOnCallbackHost })

	return func() string { return address }
}
