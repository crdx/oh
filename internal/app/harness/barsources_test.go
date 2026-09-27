package harness

import (
	"math"
	"testing"

	"crdx.org/oh/internal/app/metrics"
	"crdx.org/oh/pkg/agent"
)

func TestTheBarReadsCacheUseAndSpendFromTheSession(t *testing.T) {
	held := &App{
		metrics: metrics.New(metrics.Settings{
			ContextWindowTokens: 200_000,
			Prices:              &agent.TokenPrices{Input: 2, Output: 10, CacheRead: 1},
		}),
	}
	held.metrics.Record(agent.Event{
		Kind: agent.ModelMessageEvent,
		Usage: &agent.Usage{
			InputTokens:  1_000_000,
			OutputTokens: 100_000,
			Cache:        &agent.CacheUsage{ReadTokens: 500_000},
		},
	})
	sources := held.getBarSources()

	read, total := sources.GetCacheUsage()
	if read != 500_000 || total != 1_000_000 {
		t.Errorf("got %d read of %d, want half of the million read from cache", read, total)
	}

	spend, isKnown := sources.GetSessionSpend()
	if !isKnown || math.Abs(spend-2.5) > 1e-9 {
		t.Errorf("got %v known=%t, want $2.50 for half a million fresh, half a million cached and a tenth of a million out", spend, isKnown)
	}
}
