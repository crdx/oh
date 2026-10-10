package job

import (
	"crdx.org/oh/internal/jobs"
	"crdx.org/oh/pkg/toolbox/wait"
)

type waitSource struct {
	manager *jobs.Manager
}

func WaitSource(manager *jobs.Manager) wait.Source {
	return waitSource{manager: manager}
}

func (waitSource) Kind() string { return "job" }

func (waitSource) Mention(name string) string { return name }

func (self waitSource) Knows(name string) bool {
	_, err := self.manager.Status(name)
	return err == nil
}

func (self waitSource) Hold(name string) (<-chan struct{}, func(), error) {
	return self.manager.Hold(name)
}

func (self waitSource) Ended(name string) string {
	output, snapshot, err := self.manager.Ended(name)
	return report(name, output, snapshot, err)
}

func (self waitSource) Running(name string) string {
	output, snapshot, err := self.manager.Output(name)
	return report(name, output, snapshot, err)
}

func report(name string, output string, snapshot jobs.Snapshot, err error) string {
	if err != nil {
		return name + ": " + err.Error()
	}
	return jobs.Report(snapshot.DescribeWith(output), output, snapshot.DroppedBytes)
}
