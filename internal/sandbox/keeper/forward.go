package keeper

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"slices"
	"strconv"
	"time"

	"crdx.org/oh/internal/sandbox/loopback"
)

func (self *Keeper) OpenForward(ctx context.Context, host string, port uint16) error {
	if self.isClosed.Load() {
		return ErrClosed
	}

	self.forwardsMutex.Lock()
	defer self.forwardsMutex.Unlock()

	if _, isForwarded := self.forwards[port]; isForwarded {
		return fmt.Errorf("port %d is already forwarded", port)
	}

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "tcp", net.JoinHostPort(host, portText(port)))
	if err != nil {
		return fmt.Errorf("could not listen on %s port %d: %w", host, port, err)
	}

	if self.forwards == nil {
		self.forwards = make(map[uint16]*bridge)
	}
	self.forwards[port] = newBridge(ctx, listener, func(ctx context.Context) (net.Conn, error) {
		return self.dialForward(ctx, port)
	})

	return nil
}

func (self *Keeper) CloseForward(port uint16) error {
	self.forwardsMutex.Lock()
	crossing, isForwarded := self.forwards[port]
	delete(self.forwards, port)
	self.forwardsMutex.Unlock()

	if !isForwarded {
		return fmt.Errorf("port %d is not forwarded", port)
	}

	return crossing.Close()
}

func (self *Keeper) GetForwardPorts() []uint16 {
	self.forwardsMutex.Lock()
	defer self.forwardsMutex.Unlock()

	return slices.Sorted(maps.Keys(self.forwards))
}

func (self *Keeper) closeForwards() {
	self.forwardsMutex.Lock()
	crossings := slices.Collect(maps.Values(self.forwards))
	clear(self.forwards)
	self.forwardsMutex.Unlock()

	for _, crossing := range crossings {
		_ = crossing.Close()
	}
}

func (self *Keeper) dialForward(ctx context.Context, port uint16) (net.Conn, error) {
	if self.isClosed.Load() {
		return nil, ErrClosed
	}

	id := self.nextID.Add(1)
	answers := make(chan arrival, 1)

	self.answersMutex.Lock()
	self.answers[id] = answers
	self.answersMutex.Unlock()

	if err := self.ask(request{Kind: requestForwardDial, Token: id, Port: port}, nil); err != nil {
		self.forget(id)
		return nil, err
	}

	select {
	case packet, isOpen := <-answers:
		self.forget(id)
		return connectionFrom(packet, isOpen)
	case <-ctx.Done():
		self.forget(id)
		return nil, ctx.Err()
	}
}

func connectionFrom(packet arrival, isOpen bool) (net.Conn, error) {
	if !isOpen {
		return nil, ErrClosed
	}
	if packet.handover == nil {
		if packet.answer.Failure != "" {
			return nil, errors.New(packet.answer.Failure)
		}
		return nil, errors.New("the keeper passed no connection")
	}

	defer func() { _ = packet.handover.Close() }()

	return net.FileConn(packet.handover)
}

func (self *service) dialForward(instruction request) {
	self.loopbackOnce.Do(func() { self.loopbackErr = loopback.Up() })
	if self.loopbackErr != nil {
		self.refuse(instruction, self.loopbackErr)
		return
	}

	connection, err := dialLoopback(context.Background(), instruction.Port)
	if err != nil {
		self.refuse(instruction, err)
		return
	}
	defer func() { _ = connection.Close() }()

	tcp, isTCP := connection.(*net.TCPConn)
	if !isTCP {
		self.refuse(instruction, errors.New("the sandbox connection is not TCP"))
		return
	}

	file, err := tcp.File()
	if err != nil {
		self.refuse(instruction, err)
		return
	}
	defer func() { _ = file.Close() }()

	_ = self.sendFile(reply{Kind: replyForwardDialled, Token: instruction.Token}, file)
}

func (self *service) refuse(instruction request, err error) {
	_ = self.send(reply{Kind: replyRefused, Token: instruction.Token, Failure: err.Error()})
}

func portText(port uint16) string {
	return strconv.Itoa(int(port))
}

const loopbackDialTimeout = 5 * time.Second

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
		dialer := net.Dialer{Timeout: loopbackDialTimeout}
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
