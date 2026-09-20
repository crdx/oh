package usage_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/io/internal/app/usage"
	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/tool"
)

type monitorProvider struct {
	limit      *agent.UsageLimitError
	probeCount atomic.Int64
}

func (*monitorProvider) Configure(string, []tool.Definition) {}

func (*monitorProvider) AddUserMessage(string) {}

func (*monitorProvider) AddToolResults([]agent.ToolCallResult) {}

func (self *monitorProvider) Send(context.Context, agent.Yield) (agent.Reply, error) {
	return agent.Reply{}, self.limit
}

func (self *monitorProvider) ProbeUsage(context.Context) (agent.UsageProbe, error) {
	self.probeCount.Add(1)

	return agent.UsageProbe{
		Windows: []agent.UsageWindow{{
			Duration: time.Hour,
			Percent:  0,
			ResetsAt: time.Now().Add(time.Hour),
		}},
		Availability: agent.UsageAvailabilityAllowed,
	}, nil
}

func (*monitorProvider) Dump() []json.RawMessage {
	return nil
}

func (*monitorProvider) Load([]json.RawMessage) {}

func TestTheMonitorProbesForAnEarlyResetWithoutAnotherTurn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		now := time.Now()
		provider := &monitorProvider{limit: &agent.UsageLimitError{
			Cause: errors.New("limited"),
			Windows: []agent.UsageWindow{{
				Duration:  time.Hour,
				Percent:   100,
				ResetsAt:  now.Add(time.Hour),
				IsLimited: true,
			}},
		}}
		guard := usage.Guard(ctx, provider, usage.GuardSettings{
			ProviderName: "Codex",
			ModelName:    "gpt-5.6-sol",
			CachePath:    cachePath(t),
			Now:          time.Now,
		})

		_, _ = guard.Send(t.Context(), func(agent.Output) bool { return true })
		time.Sleep(17 * time.Minute)
		synctest.Wait()

		if got := provider.probeCount.Load(); got != 1 {
			t.Errorf("got %d probes, want one", got)
		}
	})
}
