package commands

import (
	"crdx.org/oh/pkg/agent"
)

type Forwards struct {
	Forward    func(port uint16) (agent.Event, error)
	Revoke     func(port uint16) (agent.Event, error)
	GetCurrent func() []uint16
	GetURL     func(port uint16) string
}

func (self Forwards) isConfigured() bool {
	return self.Forward != nil && self.Revoke != nil && self.GetCurrent != nil && self.GetURL != nil
}
