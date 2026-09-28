package usage_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/internal/app/usage"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
)

type monitorProvider struct {
	limit      *agent.UsageLimitError
	probe      agent.UsageProbe
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

	if self.probe.Availability != agent.UsageAvailabilityUnknown {
		return self.probe, nil
	}

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

func TestTheMonitorHonoursTheProvidersRefreshInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		now := time.Now()
		limitedWindow := agent.UsageWindow{
			Duration:  2 * time.Hour,
			Percent:   100,
			ResetsAt:  now.Add(2 * time.Hour),
			IsLimited: true,
		}
		provider := &monitorProvider{
			limit: &agent.UsageLimitError{
				Cause:   errors.New("limited"),
				Windows: []agent.UsageWindow{limitedWindow},
			},
			probe: agent.UsageProbe{
				Windows:      []agent.UsageWindow{limitedWindow},
				Availability: agent.UsageAvailabilityLimited,
				RefreshAfter: 30 * time.Minute,
			},
		}
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
			t.Fatalf("got %d probes after the initial interval, want one", got)
		}

		time.Sleep(25 * time.Minute)
		synctest.Wait()
		if got := provider.probeCount.Load(); got != 1 {
			t.Errorf("got %d probes before the provider's interval elapsed, want one", got)
		}
	})
}
