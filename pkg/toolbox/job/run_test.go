package job

import (
	"testing"

	"crdx.org/oh/internal/jobs"
	"crdx.org/oh/pkg/tool"
)

func TestAJobFailureLeavesTheFinalOutputForTheAgentToMeasure(t *testing.T) {
	_, metrics, err := run(t.Context(), jobs.New(nil), nil, nil, nil, Args{Action: actionStatus, Name: "ghost"})
	if err == nil {
		t.Fatal("the unknown job was accepted")
	}
	if metrics != (tool.ToolCallMetrics{}) {
		t.Errorf("got premature metrics %#v, want the agent to measure the final failure", metrics)
	}
}
