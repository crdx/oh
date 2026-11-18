package keeper

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const maxBridgeConnections = 64

var finishedIdleTimeout = time.Minute

type bridge struct {
	cancel   context.CancelFunc
	listener net.Listener
	connect  func(context.Context) (net.Conn, error)

	mutex       sync.Mutex
	connections map[net.Conn]struct{}
	closed      bool
	workers     sync.WaitGroup
	slots       chan struct{}
}

func newBridge(
	ctx context.Context,
	listener net.Listener,
	connect func(context.Context) (net.Conn, error),
) *bridge {
	bridgeContext, cancelBridge := context.WithCancel(ctx)
	crossing := &bridge{
		cancel:      cancelBridge,
		listener:    listener,
		connect:     connect,
		connections: make(map[net.Conn]struct{}),
		slots:       make(chan struct{}, maxBridgeConnections),
	}
	crossing.workers.Add(1)
	go crossing.serve(bridgeContext)
	return crossing
}

func (self *bridge) Close() error {
	self.mutex.Lock()
	if self.closed {
		self.mutex.Unlock()
		return nil
	}
	self.closed = true
	self.cancel()
	self.mutex.Unlock()

	err := self.listener.Close()
	self.mutex.Lock()
	for connection := range self.connections {
		_ = connection.Close()
	}
	self.mutex.Unlock()
	self.workers.Wait()
	return err
}

func (self *bridge) serve(ctx context.Context) {
	defer self.workers.Done()
	for {
		connection, err := self.listener.Accept()
		if err != nil {
			return
		}
		if !self.acquire() {
			_ = connection.Close()
			continue
		}
		if !self.track(connection) {
			self.release()
			_ = connection.Close()
			return
		}
		self.workers.Add(1)
		go self.join(ctx, connection)
	}
}

func (self *bridge) join(ctx context.Context, nearConnection net.Conn) {
	defer self.workers.Done()
	defer self.release()
	defer self.untrack(nearConnection)
	defer func() { _ = nearConnection.Close() }()

	farConnection, err := self.connect(ctx)
	if err != nil {
		return
	}
	if !self.track(farConnection) {
		_ = farConnection.Close()
		return
	}
	defer self.untrack(farConnection)
	defer func() { _ = farConnection.Close() }()

	nearFinished := &atomic.Bool{}
	connectionFinished := make(chan struct{}, 1)
	go func() {
		_, _ = io.Copy(farConnection, nearConnection)
		if tcp, ok := farConnection.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		nearFinished.Store(true)
		_ = farConnection.SetReadDeadline(time.Now().Add(finishedIdleTimeout))
		connectionFinished <- struct{}{}
	}()
	_, _ = io.Copy(nearConnection, &finishedReader{connection: farConnection, nearFinished: nearFinished})
	if tcp, ok := nearConnection.(*net.TCPConn); ok {
		_ = tcp.CloseWrite()
	}
	<-connectionFinished
}

func (self *bridge) acquire() bool {
	select {
	case self.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (self *bridge) release() {
	<-self.slots
}

func (self *bridge) track(connection net.Conn) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.closed {
		return false
	}
	self.connections[connection] = struct{}{}
	return true
}

func (self *bridge) untrack(connection net.Conn) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	delete(self.connections, connection)
}

type finishedReader struct {
	connection   net.Conn
	nearFinished *atomic.Bool
}

func (self *finishedReader) Read(buffer []byte) (int, error) {
	if self.nearFinished.Load() {
		_ = self.connection.SetReadDeadline(time.Now().Add(finishedIdleTimeout))
	}
	return self.connection.Read(buffer)
}
