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

	"crdx.org/oh/internal/sandbox/loopback"
)

func (self *Keeper) OpenHostToSandbox(ctx context.Context, host string, port uint16) error {
	if self.isClosed.Load() {
		return ErrClosed
	}

	self.portDirectionsMutex.Lock()
	defer self.portDirectionsMutex.Unlock()

	if _, isExposed := self.hostToSandbox[port]; isExposed {
		return fmt.Errorf("port %d is already exposed", port)
	}
	if _, isExposed := self.sandboxToHost[port]; isExposed {
		return fmt.Errorf("port %d already carries traffic from the sandbox to the host", port)
	}

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "tcp", net.JoinHostPort(host, portText(port)))
	if err != nil {
		return fmt.Errorf("could not listen on %s port %d: %w", host, port, err)
	}

	if self.hostToSandbox == nil {
		self.hostToSandbox = make(map[uint16]*bridge)
	}
	self.hostToSandbox[port] = newBridge(ctx, listener, func(ctx context.Context) (net.Conn, error) {
		return self.dialHostToSandbox(ctx, port)
	})

	return nil
}

func (self *Keeper) CloseHostToSandbox(port uint16) error {
	self.portDirectionsMutex.Lock()
	crossing, isExposed := self.hostToSandbox[port]
	delete(self.hostToSandbox, port)
	self.portDirectionsMutex.Unlock()

	if !isExposed {
		return fmt.Errorf("port %d is not exposed", port)
	}

	return crossing.Close()
}

func (self *Keeper) GetHostToSandboxPorts() []uint16 {
	self.portDirectionsMutex.Lock()
	defer self.portDirectionsMutex.Unlock()

	return slices.Sorted(maps.Keys(self.hostToSandbox))
}

func (self *Keeper) closeHostToSandbox() {
	self.portDirectionsMutex.Lock()
	crossings := slices.Collect(maps.Values(self.hostToSandbox))
	clear(self.hostToSandbox)
	self.portDirectionsMutex.Unlock()

	for _, crossing := range crossings {
		_ = crossing.Close()
	}
}

func (self *Keeper) dialHostToSandbox(ctx context.Context, port uint16) (net.Conn, error) {
	if self.isClosed.Load() {
		return nil, ErrClosed
	}

	id := self.nextID.Add(1)
	answers := make(chan arrival, 1)

	self.answersMutex.Lock()
	self.answers[id] = answers
	self.answersMutex.Unlock()

	if err := self.ask(request{Kind: requestHostToSandboxDial, Token: id, Port: port}, nil); err != nil {
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

func (self *service) dialHostToSandbox(instruction request) {
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

	_ = self.sendFiles(reply{Kind: replyHostToSandboxDialled, Token: instruction.Token}, []*os.File{file})
}

func (self *service) refuse(instruction request, err error) {
	_ = self.send(reply{Kind: replyRefused, Token: instruction.Token, Failure: err.Error()})
}

func portText(port uint16) string {
	return strconv.Itoa(int(port))
}
