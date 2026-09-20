package loopback

import (
	"fmt"

	"crdx.org/oh/internal/sandbox/testnamespace"

	"golang.org/x/sys/unix"
)

func Up() error {
	if testnamespace.IsUnmapped() {
		return nil
	}

	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("could not open the loopback control socket: %w", err)
	}
	defer func() { _ = unix.Close(fd) }()

	request, err := unix.NewIfreq("lo")
	if err != nil {
		return fmt.Errorf("could not name the loopback interface: %w", err)
	}

	if err := unix.IoctlIfreq(fd, unix.SIOCGIFFLAGS, request); err != nil {
		return fmt.Errorf("could not read the loopback interface: %w", err)
	}

	request.SetUint16(request.Uint16() | uint16(unix.IFF_UP))
	if err := unix.IoctlIfreq(fd, unix.SIOCSIFFLAGS, request); err != nil {
		return fmt.Errorf("could not bring up the loopback interface: %w", err)
	}

	return nil
}
