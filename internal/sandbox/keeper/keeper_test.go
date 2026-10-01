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

func openRealKeeper(t *testing.T) *Keeper {
	t.Helper()

	keeperProcess, err := Open(t.Context())
	if err != nil {
		t.Skipf("a real keeper could not start here: %v", err)
	}
	t.Cleanup(func() { _ = keeperProcess.Close() })

	return keeperProcess
}

func TestAForwardedPortNothingListensBehindIsClosedByTheKeepersRefusal(t *testing.T) {
	keeperProcess := openRealKeeper(t)
	port := freeLoopbackPort(t)

	if err := keeperProcess.OpenForward(t.Context(), testForwardHost, port); err != nil {
		t.Fatal(err)
	}

	connection, err := dialForwarded(t, testForwardHost, port)
	if err != nil {
		t.Fatalf("the forwarded port did not accept a connection: %v", err)
	}
	defer func() { _ = connection.Close() }()

	if err := connection.SetReadDeadline(time.Now().Add(realKeeperWait)); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Read(make([]byte, 1)); err == nil || os.IsTimeout(err) {
		t.Errorf("got %v, want the connection closed once the keeper found nothing listening", err)
	}
}

func TestClosingTheKeeperClosesEveryForwardedPort(t *testing.T) {
	keeperProcess := openRealKeeper(t)
	first := freeLoopbackPort(t)
	second := freeLoopbackPort(t)

	for _, port := range []uint16{first, second} {
		if err := keeperProcess.OpenForward(t.Context(), testForwardHost, port); err != nil {
			t.Fatal(err)
		}
	}
	if err := keeperProcess.Close(); err != nil {
		t.Fatal(err)
	}

	for _, port := range []uint16{first, second} {
		connection, err := dialForwarded(t, testForwardHost, port)
		if err == nil {
			_ = connection.Close()
			t.Errorf("port %d still accepts connections after the keeper closed", port)
		}
	}
	if len(keeperProcess.GetForwardPorts()) != 0 {
		t.Errorf("got forwarded ports %v, want none", keeperProcess.GetForwardPorts())
	}
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
