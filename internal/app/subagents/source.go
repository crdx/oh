package subagents

import (
	"strings"

	"crdx.org/oh/pkg/toolbox/subagent"
	"crdx.org/oh/pkg/toolbox/wait"
)

type waitSource struct {
	manager *Manager
}

func (self *Manager) WaitSource() wait.Source {
	return waitSource{manager: self}
}

func (waitSource) Kind() string { return subagent.Name }

func (waitSource) Mention(name string) string { return subagent.Mention(name) }

func (self waitSource) Knows(name string) bool {
	_, _, err := self.manager.snapshots([]string{name})
	return err == nil
}

func (self waitSource) Hold(name string) (<-chan struct{}, func(), error) {
	_, overs, err := self.manager.snapshots([]string{name})
	if err != nil {
		return nil, nil, err
	}
	return overs[0], func() {}, nil
}

func (self waitSource) Ended(name string) string {
	return self.read(name)
}

func (self waitSource) Running(name string) string {
	return self.read(name)
}

func (self waitSource) read(name string) string {
	text, err := self.manager.Output([]string{name})
	if err != nil {
		return name + ": " + err.Error()
	}
	return strings.TrimRight(text, "\n")
}
