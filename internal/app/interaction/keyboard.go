package interaction

import (
	"os"
	"sync"

	"crdx.org/oh/internal/app/key"
)

type Keyboard struct {
	terminal *os.File
	keys     chan key.Key

	mutex    sync.Mutex
	release  func()
	isClosed bool
}

func NewKeyboard(terminal *os.File) *Keyboard {
	self := &Keyboard{terminal: terminal, keys: make(chan key.Key)}
	self.Resume()

	return self
}

func (self *Keyboard) Keys() <-chan key.Key {
	return self.keys
}

func (self *Keyboard) Release() {
	self.mutex.Lock()
	release := self.release
	self.release = nil
	self.mutex.Unlock()

	if release != nil {
		release()
	}
}

func (self *Keyboard) Resume() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.release != nil || self.isClosed {
		return
	}

	keys, stopReading := Keypresses(self.terminal)
	releaseSignal := make(chan struct{})
	forwarderDone := make(chan struct{})

	go func() {
		defer close(forwarderDone)

		for keypress := range keys {
			select {
			case self.keys <- keypress:
			case <-releaseSignal:
				return
			}
		}

		select {
		case <-releaseSignal:
		default:
			self.close()
		}
	}()

	self.release = func() {
		close(releaseSignal)
		stopReading()
		<-forwarderDone
	}
}

func (self *Keyboard) close() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if !self.isClosed {
		self.isClosed = true
		close(self.keys)
	}
}
