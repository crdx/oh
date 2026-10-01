package keeper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const testForwardHost = "127.0.0.1"

func TestForwardingAPortListensOnceAndSaysSoWhenItIsAlreadyOpen(t *testing.T) {
	keeperProcess := &Keeper{}
	port := freeLoopbackPort(t)

	if err := keeperProcess.OpenForward(t.Context(), testForwardHost, port); err != nil {
		t.Fatalf("could not forward port %d: %v", port, err)
	}
	t.Cleanup(func() { _ = keeperProcess.CloseForward(port) })

	if forwardPorts := keeperProcess.GetForwardPorts(); !slices.Equal(forwardPorts, []uint16{port}) {
		t.Errorf("got forwarded ports %v, want [%d]", forwardPorts, port)
	}
	if err := keeperProcess.OpenForward(t.Context(), testForwardHost, port); err == nil {
		t.Error("forwarding a port twice was allowed")
	} else if !strings.Contains(err.Error(), "already forwarded") {
		t.Errorf("got %v, want a refusal naming the forwarded port", err)
	}
}

func TestRevokingAPortClosesItAndSaysSoWhenItWasNotOpen(t *testing.T) {
	keeperProcess := &Keeper{}
	port := freeLoopbackPort(t)

	if err := keeperProcess.OpenForward(t.Context(), testForwardHost, port); err != nil {
		t.Fatalf("could not forward port %d: %v", port, err)
	}
	if err := keeperProcess.CloseForward(port); err != nil {
		t.Fatalf("could not hide port %d: %v", port, err)
	}
	if forwardPorts := keeperProcess.GetForwardPorts(); len(forwardPorts) != 0 {
		t.Errorf("got forwarded ports %v, want none", forwardPorts)
	}
	if err := keeperProcess.CloseForward(port); err == nil {
		t.Error("revoking a port that was not forwarded was allowed")
	}

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp", net.JoinHostPort(testForwardHost, portText(port)))
	if err != nil {
		t.Errorf("the forwarded listener was not closed: %v", err)
		return
	}
	_ = listener.Close()
}

func TestForwardingIsRefusedOnceTheKeeperIsClosed(t *testing.T) {
	keeperProcess := &Keeper{}
	keeperProcess.isClosed.Store(true)

	if err := keeperProcess.OpenForward(t.Context(), testForwardHost, freeLoopbackPort(t)); !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want the keeper to be closed", err)
	}
}

func TestABridgeJoinsWhatItAcceptsToWhatItConnects(t *testing.T) {
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
		said, _ := io.ReadAll(connection)
		_, _ = connection.Write([]byte("heard " + string(said)))
		_ = connection.Close()
	}()

	near, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback sockets are unavailable: %v", err)
	}

	joined := newBridge(t.Context(), near, func(ctx context.Context) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "tcp", far.Addr().String())
	})
	defer func() { _ = joined.Close() }()

	var dialer net.Dialer
	connection, err := dialer.DialContext(t.Context(), "tcp", near.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write([]byte("spoken")); err != nil {
		t.Fatal(err)
	}
	if tcp, isTCP := connection.(*net.TCPConn); isTCP {
		_ = tcp.CloseWrite()
	}
	answer, err := io.ReadAll(connection)
	_ = connection.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(answer) != "heard spoken" {
		t.Errorf("got %q, want the far side's answer", answer)
	}
}

func TestAConnectionIsTakenFromTheKeepersAnswerOrItsRefusal(t *testing.T) {
	if _, err := connectionFrom(arrival{}, false); !errors.Is(err, ErrClosed) {
		t.Errorf("got %v, want the keeper to be closed", err)
	}

	refusal := arrival{answer: reply{Kind: replyRefused, Failure: "connection refused"}}
	if _, err := connectionFrom(refusal, true); err == nil || err.Error() != "connection refused" {
		t.Errorf("got %v, want the keeper's own failure", err)
	}

	silence := arrival{answer: reply{Kind: replyForwardDialled}}
	if _, err := connectionFrom(silence, true); err == nil {
		t.Error("a reply carrying no connection was accepted")
	}
}

func freeLoopbackPort(t *testing.T) uint16 {
	t.Helper()

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback sockets are unavailable: %v", err)
	}
	port := addressPort(t, listener.Addr())
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func TestHostLoopbackDialUsesALiteralLoopbackAddress(t *testing.T) {
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("IPv4 loopback is unavailable: %v", err)
	}
	defer func() { _ = listener.Close() }()

	accepted := make(chan net.Conn, 1)
	go func() {
		connection, _ := listener.Accept()
		accepted <- connection
	}()

	port := addressPort(t, listener.Addr())
	connection, err := dialLoopback(t.Context(), port)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()

	hostConnection := <-accepted
	if hostConnection == nil {
		t.Fatal("the host listener did not accept the connection")
	}
	_ = hostConnection.Close()

	host, _, err := net.SplitHostPort(connection.RemoteAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	if address := net.ParseIP(host); address == nil || !address.IsLoopback() {
		t.Errorf("dial reached non-loopback address %s", connection.RemoteAddr())
	}
}

func addressPort(t *testing.T, address net.Addr) uint16 {
	t.Helper()

	_, portText, err := net.SplitHostPort(address.String())
	if err != nil {
		t.Fatal(err)
	}
	var port uint16
	if _, err := fmt.Sscan(portText, &port); err != nil {
		t.Fatal(err)
	}
	return port
}

func TestABridgeBoundsItsConnections(t *testing.T) {
	joined := &bridge{slots: make(chan struct{}, maxBridgeConnections)}
	for range maxBridgeConnections {
		if !joined.acquire() {
			t.Fatal("a connection below the limit was refused")
		}
	}
	if joined.acquire() {
		t.Fatal("a connection beyond the limit was accepted")
	}
	joined.release()
	if !joined.acquire() {
		t.Fatal("a released connection slot was not reusable")
	}
}

func TestCancellingAHostLoopbackDialStopsIt(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := dialLoopback(ctx, 1)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want cancellation", err)
	}
}

func passedFiles(t *testing.T, count int) ([]byte, []int) {
	t.Helper()

	var descriptors []int
	for range count {
		file, err := os.Open(os.DevNull)
		if err != nil {
			t.Fatal(err)
		}
		descriptor, err := unix.Dup(int(file.Fd()))
		if err != nil {
			t.Fatal(err)
		}
		_ = file.Close()
		descriptors = append(descriptors, descriptor)
	}

	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = unix.Close(pair[0])
		_ = unix.Close(pair[1])
	}()

	if len(descriptors) > 0 {
		if err := unix.Sendmsg(pair[0], []byte("x"), unix.UnixRights(descriptors...), nil, 0); err != nil {
			t.Fatal(err)
		}
	}
	for _, descriptor := range descriptors {
		_ = unix.Close(descriptor)
	}
	if len(descriptors) == 0 {
		return nil, nil
	}

	message := make([]byte, 1)
	control := make([]byte, unix.CmsgSpace(4*count))
	length, controlLength, flags, _, err := unix.Recvmsg(pair[1], message, control, 0)
	if err != nil {
		t.Fatal(err)
	}
	if length != len(message) || flags&unix.MSG_CTRUNC != 0 {
		t.Fatalf("got %d bytes and flags %d, want the whole message and every descriptor", length, flags)
	}
	messages, err := unix.ParseSocketControlMessage(control[:controlLength])
	if err != nil {
		t.Fatal(err)
	}
	var received []int
	for _, controlMessage := range messages {
		rights, err := unix.ParseUnixRights(&controlMessage)
		if err != nil {
			t.Fatal(err)
		}
		received = append(received, rights...)
	}

	return control[:controlLength], received
}

func isOpenDescriptor(descriptor int) bool {
	_, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFD, 0)
	return err == nil
}

func TestTheFirstDescriptorPassedIsKeptAndTheRestAreClosed(t *testing.T) {
	control, descriptors := passedFiles(t, 3)
	if len(descriptors) != 3 {
		t.Fatalf("got %d descriptors, want 3", len(descriptors))
	}

	got := firstFile(control, "forwarded-connection")
	if got == nil {
		t.Fatal("no file was taken from the passed descriptors")
	}
	defer func() { _ = got.Close() }()

	if got.Name() != "forwarded-connection" {
		t.Errorf("got file name %q", got.Name())
	}
	if int(got.Fd()) != descriptors[0] {
		t.Errorf("got descriptor %d, want the first, %d", got.Fd(), descriptors[0])
	}
	for _, extra := range descriptors[1:] {
		if isOpenDescriptor(extra) {
			t.Errorf("descriptor %d was left open", extra)
			_ = unix.Close(extra)
		}
	}
}

func TestNoFileIsTakenWhereNoneWasPassed(t *testing.T) {
	if got := firstFile(nil, "forwarded-connection"); got != nil {
		t.Errorf("got %v from no control message", got)
	}
	if got := firstFile([]byte("not a control message"), "forwarded-connection"); got != nil {
		t.Errorf("got %v from a malformed control message", got)
	}
}

func TestAFileIsClosedWhenThereIsNothingToClose(t *testing.T) {
	closeFile(nil)

	file, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	closeFile(file)
	if isOpenDescriptor(int(file.Fd())) {
		t.Error("the file was left open")
	}
}

const outsideHost = "127.0.0.2"

func joinedKeeperAndService(t *testing.T) *Keeper {
	t.Helper()

	descriptors, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	nearControl, err := connection(os.NewFile(uintptr(descriptors[0]), "near"))
	if err != nil {
		t.Fatal(err)
	}
	farControl, err := connection(os.NewFile(uintptr(descriptors[1]), "far"))
	if err != nil {
		t.Fatal(err)
	}

	sandboxSide := &service{control: farControl, commands: make(map[uint64]*exec.Cmd)}
	sandboxSide.loopbackOnce.Do(func() {})
	go sandboxSide.accept()

	keeperProcess := &Keeper{process: &exec.Cmd{}, control: nearControl, answers: make(map[uint64]chan arrival)}
	keeperProcess.readers.Add(1)
	go keeperProcess.receive()
	t.Cleanup(func() {
		_ = keeperProcess.Close()
		_ = farControl.Close()
	})

	return keeperProcess
}

func requireOutsideHost(t *testing.T) {
	t.Helper()

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp", net.JoinHostPort(outsideHost, "0"))
	if err != nil {
		t.Skipf("%s is unavailable: %v", outsideHost, err)
	}
	_ = listener.Close()
}

func TestAForwardedPortCarriesTrafficBothWaysToWhatListensBehindIt(t *testing.T) {
	requireOutsideHost(t)
	keeperProcess := joinedKeeperAndService(t)
	port := freeLoopbackPort(t)

	var listenConfig net.ListenConfig
	behind, err := listenConfig.Listen(t.Context(), "tcp", net.JoinHostPort(testForwardHost, strconv.Itoa(int(port))))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = behind.Close() }()
	go func() {
		for {
			accepted, err := behind.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = accepted.Close() }()
				_, _ = io.Copy(accepted, accepted)
			}()
		}
	}()

	if err := keeperProcess.OpenForward(t.Context(), outsideHost, port); err != nil {
		t.Fatal(err)
	}

	connection, err := dialForwarded(t, outsideHost, port)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	if err := connection.SetDeadline(time.Now().Add(realKeeperWait)); err != nil {
		t.Fatal(err)
	}

	for _, message := range []string{"ping", "and again"} {
		if _, err := connection.Write([]byte(message)); err != nil {
			t.Fatal(err)
		}
		echoed := make([]byte, len(message))
		if _, err := io.ReadFull(connection, echoed); err != nil {
			t.Fatal(err)
		}
		if string(echoed) != message {
			t.Errorf("got %q back, want %q", echoed, message)
		}
	}
}

func TestAForwardedPortWithNothingBehindItClosesTheConnectionAtOnce(t *testing.T) {
	requireOutsideHost(t)
	keeperProcess := joinedKeeperAndService(t)
	port := freeLoopbackPort(t)

	if err := keeperProcess.OpenForward(t.Context(), outsideHost, port); err != nil {
		t.Fatal(err)
	}

	connection, err := dialForwarded(t, outsideHost, port)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	if err := connection.SetReadDeadline(time.Now().Add(realKeeperWait)); err != nil {
		t.Fatal(err)
	}

	if _, err := connection.Read(make([]byte, 1)); err == nil || os.IsTimeout(err) {
		t.Errorf("got %v, want the connection closed", err)
	}
}

func TestAPortStopsCarryingTrafficOnceItIsRevoked(t *testing.T) {
	requireOutsideHost(t)
	keeperProcess := joinedKeeperAndService(t)
	port := freeLoopbackPort(t)

	if err := keeperProcess.OpenForward(t.Context(), outsideHost, port); err != nil {
		t.Fatal(err)
	}
	if err := keeperProcess.CloseForward(port); err != nil {
		t.Fatal(err)
	}

	if connection, err := dialForwarded(t, outsideHost, port); err == nil {
		_ = connection.Close()
		t.Error("a revoked port still accepts connections")
	}
	if err := keeperProcess.OpenForward(t.Context(), outsideHost, port); err != nil {
		t.Errorf("a revoked port could not be forwarded again: %v", err)
	}
}
