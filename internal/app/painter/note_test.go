package painter

import (
	"testing"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/conditions"
	"crdx.org/oh/internal/app/environment"
	"crdx.org/oh/internal/app/hostcommand"
	"crdx.org/oh/internal/app/jobrecord"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/subagentrecord"
	"crdx.org/oh/internal/app/toolset"
	"crdx.org/oh/internal/app/turn"
	"crdx.org/oh/pkg/agent"
)

func TestEveryHarnessNoticeIsToldAsTheKindOfNoteItIs(t *testing.T) {
	for kind, want := range map[agent.Kind]agent.NoteKind{
		caps.ModeChange:                    agent.EnvironmentNote,
		conditions.Change:                  agent.EnvironmentNote,
		environment.Change:                 agent.EnvironmentNote,
		toolset.AvailabilityChange:         agent.EnvironmentNote,
		pathgrant.Change:                   agent.EnvironmentNote,
		portgrant.ForwardChange:            agent.EnvironmentNote,
		caps.JobStop:                       agent.JobNote,
		jobrecord.Ended:                    agent.JobNote,
		jobrecord.EndedWithSession:         agent.JobNote,
		hostcommand.Ran:                    agent.HostCommandNote,
		turn.HarnessPoke:                   agent.PokeNote,
		subagentrecord.ReportsDelivered:    agent.SubagentNote,
		subagentrecord.AccessWithdrawnStop: agent.SubagentNote,
		agent.UserMessageEvent:             "",
		agent.ModelMessageEvent:            "",
		agent.ToolCallResultEvent:          "",
		agent.InterruptionEvent:            "",
		agent.FailureEvent:                 "",
		agent.SilentTurnEvent:              "",
		agent.StartupEvent:                 "",
		agent.CacheRebuildEvent:            "",
		agent.PrefixRewriteEvent:           "",
		agent.ModelReasoningEvent:          "",
		agent.ToolCallRequestEvent:         "",
		agent.StateChangeEvent:             "",
		agent.RetryingEvent:                "",
	} {
		if got := HarnessNoteKind(kind); got != want {
			t.Errorf("%s: got %q, want %q", kind, got, want)
		}
	}
}
