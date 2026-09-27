package ptytest

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func Open(tb testing.TB) (*os.File, *os.File) {
	tb.Helper()

	controller, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		tb.Skipf("no pseudo-terminal to test against: %v", err)
	}
	tb.Cleanup(func() { _ = controller.Close() })

	if err := unix.IoctlSetPointerInt(int(controller.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		tb.Fatal(err)
	}
	number, err := unix.IoctlGetInt(int(controller.Fd()), unix.TIOCGPTN)
	if err != nil {
		tb.Fatal(err)
	}
	terminal, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = terminal.Close() })

	return controller, terminal
}

func OpenSized(tb testing.TB, columns int, rows int) (*os.File, *os.File) {
	tb.Helper()

	controller, terminal := Open(tb)
	size := &unix.Winsize{Row: uint16(rows), Col: uint16(columns)} //nolint:gosec // a test names a size a terminal can have
	if err := unix.IoctlSetWinsize(int(terminal.Fd()), unix.TIOCSWINSZ, size); err != nil {
		tb.Fatal(err)
	}

	return controller, terminal
}

type Transcript struct {
	mutex       sync.Mutex
	text        bytes.Buffer
	lastWriteAt time.Time
}

func Record(controller *os.File) *Transcript {
	self := &Transcript{lastWriteAt: time.Now()}
	go self.readFrom(controller)

	return self
}

func (self *Transcript) String() string {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.text.String()
}

func (self *Transcript) Len() int {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.text.Len()
}

func (self *Transcript) WaitForGrowth(from int, deadline time.Duration) bool {
	until := time.Now().Add(deadline)
	for time.Now().Before(until) {
		if self.Len() > from {
			return true
		}
		time.Sleep(pollInterval)
	}

	return false
}

func (self *Transcript) Count(text string) int {
	return strings.Count(self.String(), text)
}

func (self *Transcript) WaitForCount(text string, count int, deadline time.Duration) bool {
	until := time.Now().Add(deadline)
	for time.Now().Before(until) {
		if self.Count(text) >= count {
			return true
		}
		time.Sleep(pollInterval)
	}

	return false
}

func (self *Transcript) WaitToSettle(quiet time.Duration) {
	for {
		self.mutex.Lock()
		lastWriteAt := self.lastWriteAt
		self.mutex.Unlock()
		if time.Since(lastWriteAt) >= quiet {
			return
		}
		time.Sleep(quiet / 4)
	}
}

func (self *Transcript) readFrom(controller *os.File) {
	buffer := make([]byte, 4096)
	for {
		count, err := controller.Read(buffer)
		if count > 0 {
			self.mutex.Lock()
			self.text.Write(buffer[:count])
			self.lastWriteAt = time.Now()
			self.mutex.Unlock()
		}
		if err != nil {
			return
		}
	}
}

const pollInterval = 10 * time.Millisecond
