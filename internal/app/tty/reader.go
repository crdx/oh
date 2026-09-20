package tty

import (
	"errors"
	"io"
	"os"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const readPollInterval = 100 * time.Millisecond

type Reader struct {
	input      *os.File
	wakeRead   *os.File
	wakeWrite  *os.File
	stopSignal chan struct{}
	once       sync.Once
}

func NewReader(input *os.File) *Reader {
	self := &Reader{input: input, stopSignal: make(chan struct{})}

	if wakeRead, wakeWrite, err := os.Pipe(); err == nil {
		self.wakeRead, self.wakeWrite = wakeRead, wakeWrite
	}

	return self
}

func (self *Reader) Stop() {
	self.once.Do(func() {
		close(self.stopSignal)

		if self.wakeWrite != nil {
			_, _ = self.wakeWrite.Write([]byte{0})
		}
	})
}

func (self *Reader) Stopping() <-chan struct{} {
	return self.stopSignal
}

func (self *Reader) Close() {
	if self.wakeWrite != nil {
		_ = self.wakeWrite.Close()
		_ = self.wakeRead.Close()
	}
}

func (self *Reader) Read(buffer []byte) (int, error) {
	pollDescriptors := []unix.PollFd{{Fd: pollDescriptor(self.input), Events: unix.POLLIN}}
	timeout := int(readPollInterval.Milliseconds())

	if self.wakeRead != nil {
		pollDescriptors = append(pollDescriptors, unix.PollFd{Fd: pollDescriptor(self.wakeRead), Events: unix.POLLIN})
		timeout = -1
	}

	for {
		select {
		case <-self.stopSignal:
			return 0, io.EOF
		default:
		}

		ready, err := unix.Poll(pollDescriptors, timeout)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if ready == 0 || pollDescriptors[0].Revents == 0 {
			continue
		}

		return self.input.Read(buffer)
	}
}

func pollDescriptor(file *os.File) int32 {
	return int32(file.Fd()) //nolint:gosec // Unix file descriptors fit PollFd
}
