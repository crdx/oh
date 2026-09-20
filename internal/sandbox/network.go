package sandbox

import "crdx.org/oh/internal/sandbox/loopback"

func applyNetwork() error {
	return loopback.Up()
}
