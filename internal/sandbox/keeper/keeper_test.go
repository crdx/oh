package keeper

import (
	"syscall"
	"testing"
)

func TestTheKeeperOwnsThePrivateNamespaces(t *testing.T) {
	for name, flag := range map[string]uintptr{
		"user":    syscall.CLONE_NEWUSER,
		"network": syscall.CLONE_NEWNET,
		"ipc":     syscall.CLONE_NEWIPC,
		"uts":     syscall.CLONE_NEWUTS,
	} {
		if Attributes().Cloneflags&flag == 0 {
			t.Errorf("expected a private %s namespace", name)
		}
	}
}
