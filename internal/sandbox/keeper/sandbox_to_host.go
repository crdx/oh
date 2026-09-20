package keeper

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"slices"
	"strconv"
	"time"

	"crdx.org/oh/internal/sandbox/loopback"

	"golang.org/x/sys/unix"
)

const sandboxToHostDialTimeout = 5 * time.Second

func openSandboxToHostListeners(ports []uint16) ([]*os.File, error) {
	if len(ports) == 0 {
		return nil, nil
	}
	if err := loopback.Up(); err != nil {
		return nil, err
	}

	files := make([]*os.File, 0, len(ports))
	var listenConfig net.ListenConfig
	for _, port := range ports {
		listener, err := listenConfig.Listen(context.Background(), "tcp", ":"+strconv.Itoa(int(port)))
		if err != nil {
			closeFiles(files)
			return nil, fmt.Errorf("could not forward host loopback port %d: %w", port, err)
		}
		tcp, ok := listener.(*net.TCPListener)
		if !ok {
			_ = listener.Close()
			closeFiles(files)
			return nil, errors.New("loopback listener is not TCP")
		}
		file, err := tcp.File()
		_ = listener.Close()
		if err != nil {
			closeFiles(files)
			return nil, fmt.Errorf("could not pass host loopback port %d: %w", port, err)
		}
		files = append(files, file)
	}
	return files, nil
}

func closeFiles(files []*os.File) {
	for _, file := range files {
		_ = file.Close()
	}
}

func newSandboxToHostBridge(ctx context.Context, file *os.File, port uint16) (*bridge, error) {
	listener, err := net.FileListener(file)
	if err != nil {
		return nil, err
	}
	return newBridge(ctx, listener, func(ctx context.Context) (net.Conn, error) {
		return dialLoopback(ctx, port)
	}), nil
}

func dialLoopback(ctx context.Context, port uint16) (net.Conn, error) {
	writtenPort := strconv.Itoa(int(port))
	var failures []error
	for _, target := range []struct {
		network string
		host    string
	}{
		{network: "tcp4", host: "127.0.0.1"},
		{network: "tcp6", host: "::1"},
	} {
		dialer := net.Dialer{Timeout: sandboxToHostDialTimeout}
		connection, err := dialer.DialContext(
			ctx, target.network, net.JoinHostPort(target.host, writtenPort),
		)
		if err == nil {
			return connection, nil
		}
		failures = append(failures, err)
	}
	return nil, errors.Join(failures...)
}

func (self *Keeper) OpenSandboxToHost(ctx context.Context, port uint16) error {
	if self.isClosed.Load() {
		return ErrClosed
	}

	self.portDirectionsMutex.Lock()
	defer self.portDirectionsMutex.Unlock()

	if _, isForwarded := self.sandboxToHost[port]; isForwarded {
		return fmt.Errorf("host loopback port %d is already exposed", port)
	}
	if _, isExposed := self.hostToSandbox[port]; isExposed {
		return fmt.Errorf("port %d already carries traffic from the host to the sandbox", port)
	}

	file, err := self.listenSandboxToHost(ctx, port)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	crossing, err := newSandboxToHostBridge(ctx, file, port)
	if err != nil {
		return fmt.Errorf("could not bridge host loopback port %d: %w", port, err)
	}
	if self.sandboxToHost == nil {
		self.sandboxToHost = make(map[uint16]*bridge)
	}
	self.sandboxToHost[port] = crossing

	return nil
}

func (self *Keeper) CloseSandboxToHost(port uint16) error {
	self.portDirectionsMutex.Lock()
	crossing, isForwarded := self.sandboxToHost[port]
	delete(self.sandboxToHost, port)
	self.portDirectionsMutex.Unlock()

	if !isForwarded {
		return fmt.Errorf("host loopback port %d is not exposed", port)
	}

	return crossing.Close()
}

func (self *Keeper) GetSandboxToHostPorts() []uint16 {
	self.portDirectionsMutex.Lock()
	defer self.portDirectionsMutex.Unlock()

	return slices.Sorted(maps.Keys(self.sandboxToHost))
}

func (self *Keeper) closeSandboxToHost() {
	self.portDirectionsMutex.Lock()
	crossings := slices.Collect(maps.Values(self.sandboxToHost))
	clear(self.sandboxToHost)
	self.portDirectionsMutex.Unlock()

	for _, crossing := range crossings {
		_ = crossing.Close()
	}
}

func (self *Keeper) listenSandboxToHost(ctx context.Context, port uint16) (*os.File, error) {
	id := self.nextID.Add(1)
	answers := make(chan arrival, 1)

	self.answersMutex.Lock()
	self.answers[id] = answers
	self.answersMutex.Unlock()

	if err := self.ask(request{Kind: requestSandboxToHostListen, Token: id, Port: port}, nil); err != nil {
		self.forget(id)
		return nil, err
	}

	select {
	case packet, isOpen := <-answers:
		self.forget(id)
		if !isOpen {
			return nil, ErrClosed
		}
		if packet.handover == nil {
			if packet.answer.Failure != "" {
				return nil, errors.New(packet.answer.Failure)
			}
			return nil, errors.New("the keeper passed no listener")
		}
		return packet.handover, nil
	case <-ctx.Done():
		self.forget(id)
		return nil, ctx.Err()
	}
}

func (self *service) listenSandboxToHost(instruction request) {
	listeners, err := openSandboxToHostListeners([]uint16{instruction.Port})
	if err != nil {
		self.refuse(instruction, err)
		return
	}
	defer closeFiles(listeners)

	_ = self.sendFiles(reply{Kind: replySandboxToHostListening, Token: instruction.Token}, listeners)
}

func parseFiles(control []byte) []*os.File {
	messages, err := unix.ParseSocketControlMessage(control)
	if err != nil {
		return nil
	}
	var files []*os.File
	for _, message := range messages {
		descriptors, err := unix.ParseUnixRights(&message)
		if err != nil {
			continue
		}
		for _, descriptor := range descriptors {
			unix.CloseOnExec(descriptor)
			files = append(files, os.NewFile(uintptr(descriptor), "loopback-forward"))
		}
	}
	return files
}
