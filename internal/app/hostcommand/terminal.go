package hostcommand

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

const (
	terminalDescriptor = 3
	terminalDrain      = 100 * time.Millisecond
)

type terminal struct {
	controller *os.File
	device     *os.File
	read       chan struct{}
}

func openTerminal() (*terminal, error) {
	controller, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}

	device, err := openDevice(controller)
	if err != nil {
		_ = controller.Close()
		return nil, err
	}

	return &terminal{controller: controller, device: device, read: make(chan struct{})}, nil
}

func openDevice(controller *os.File) (*os.File, error) {
	if err := unix.IoctlSetPointerInt(int(controller.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		return nil, err
	}
	number, err := unix.IoctlGetInt(int(controller.Fd()), unix.TIOCGPTN)
	if err != nil {
		return nil, err
	}
	device, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}

	settings, err := unix.IoctlGetTermios(int(device.Fd()), unix.TCGETS)
	if err == nil {
		settings.Oflag &^= unix.OPOST
		err = unix.IoctlSetTermios(int(device.Fd()), unix.TCSETS, settings)
	}
	if err != nil {
		_ = device.Close()
		return nil, err
	}

	return device, nil
}

func (self *terminal) copyTo(output io.Writer) {
	go func() {
		defer close(self.read)
		_, _ = io.Copy(output, self.controller)
	}()
}

func (self *terminal) started() {
	_ = self.device.Close()
}

func (self *terminal) close() {
	_ = self.device.Close()
	if err := self.controller.SetReadDeadline(time.Now().Add(terminalDrain)); errors.Is(err, os.ErrNoDeadline) {
		_ = self.controller.Close()
	}
	<-self.read
	_ = self.controller.Close()
}
