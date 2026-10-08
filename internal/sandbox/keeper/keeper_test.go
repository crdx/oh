package keeper

import (
	"context"
	"net"
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"
)

const realKeeperWait = 5 * time.Second

func dialForwarded(t *testing.T, host string, port uint16) (net.Conn, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), realKeeperWait)
	defer cancel()

	var dialer net.Dialer
	return dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(int(port))))
}

func TestMain(m *testing.M) {
	Init()
	os.Exit(m.Run())
}

func TestTheKeeperOwnsThePrivateNamespaces(t *testing.T) {
	for name, flag := range map[string]uintptr{
		"user":    syscall.CLONE_NEWUSER,
		"network": syscall.CLONE_NEWNET,
		"ipc":     syscall.CLONE_NEWIPC,
		"uts":     syscall.CLONE_NEWUTS,
	} {
		if Attributes().Cloneflags&flag == 0 {
			t.Errorf("expected a private %s namespace", name)
		}
	}
}
