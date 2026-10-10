package harness

import (
	"crdx.org/oh/internal/app/subagents"
	"crdx.org/oh/internal/jobs"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/job"
	"crdx.org/oh/pkg/toolbox/wait"
)

func waitTools(jobManager *jobs.Manager, childManager *subagents.Manager) []tool.Tool {
	var sources []wait.Source
	if jobManager != nil {
		sources = append(sources, job.WaitSource(jobManager))
	}
	if childManager != nil {
		sources = append(sources, childManager.WaitSource())
	}
	if len(sources) == 0 {
		return nil
	}
	return []tool.Tool{wait.New(sources, agent.MessageArrival)}
}
