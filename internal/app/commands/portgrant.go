package commands

import (
	"crdx.org/io/pkg/agent"
)

type HostToSandbox struct {
	Hide       func(port uint16) (agent.Event, error)
	GetCurrent func() []uint16
	GetURL     func(port uint16) string
}

type SandboxToHost struct {
	Expose     func(port uint16) (agent.Event, error)
	Revoke     func(port uint16) (agent.Event, error)
	GetCurrent func() []uint16
}

func (self HostToSandbox) isConfigured() bool {
	return self.Hide != nil && self.GetCurrent != nil && self.GetURL != nil
}

func (self SandboxToHost) isConfigured() bool {
	return self.Expose != nil && self.Revoke != nil && self.GetCurrent != nil
}
