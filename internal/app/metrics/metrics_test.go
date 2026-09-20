package metrics

import (
	"math"
	"testing"

	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/session"
)

func contextUsageAt(events []agent.Event, contextWindowTokens int) (int, int) {
	tracker := New(Settings{ContextWindowTokens: contextWindowTokens})
	tracker.Restore(events, nil)
	return tracker.ContextUsage()
}

func TestContextUsageComesFromTheLatestReportInTheViewedHistory(t *testing.T) {
	events := []agent.Event{
		{Kind: agent.ModelMessageEvent, Usage: &agent.Usage{InputTokens: 5000}},
		{Kind: agent.UserMessageEvent},
		{Kind: agent.ToolCallRequestEvent, Usage: &agent.Usage{InputTokens: 12_000}},
	}

	usedTokens, totalTokens := contextUsageAt(events[:2], 200_000)
	if usedTokens != 5000 || totalTokens != 200_000 {
		t.Errorf("historical prefix gave %d/%d", usedTokens, totalTokens)
	}

	usedTokens, totalTokens = contextUsageAt(events, 200_000)
	if usedTokens != 12_000 || totalTokens != 200_000 {
		t.Errorf("full history gave %d/%d", usedTokens, totalTokens)
	}
}

func TestContextUsageIsUnknownBeforeTheFirstReport(t *testing.T) {
	usedTokens, totalTokens := contextUsageAt([]agent.Event{{Kind: agent.UserMessageEvent}}, 200_000)
	if usedTokens != 0 || totalTokens != 200_000 {
		t.Errorf("got %d/%d", usedTokens, totalTokens)
	}
}

func TestTrackerCountsEveryStartedTurn(t *testing.T) {
	tracker := New(Settings{})
	for range 3 {
		tracker.BeginTurn()
	}

	if got := tracker.TurnCount(); got != 3 {
		t.Errorf("expected three turns, got %d", got)
	}
}

func TestTrackerRestoresCompletedTurnsIndependentlyOfMessages(t *testing.T) {
	tracker := New(Settings{})
	tracker.Restore(
		[]agent.Event{{Kind: agent.UserMessageEvent}, {Kind: agent.UserMessageEvent}},
		[]session.TurnSummary{{}},
	)

	if got := tracker.TurnCount(); got != 1 {
		t.Errorf("expected one turn, got %d", got)
	}
}

func TestTrackerPrefersTheContextTheLastTurnEndedOn(t *testing.T) {
	tracker := New(Settings{})
	tracker.Restore(
		[]agent.Event{{Kind: agent.ModelMessageEvent, Usage: &agent.Usage{InputTokens: 5000}}},
		[]session.TurnSummary{{InputTokens: 12_000}},
	)

	if got, _ := tracker.ContextUsage(); got != 12_000 {
		t.Errorf("expected the context the turn ended on, got %d", got)
	}
}

var testPrices = agent.TokenPrices{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75}

func TestSpendIsUnknownWithoutPrices(t *testing.T) {
	tracker := New(Settings{})
	tracker.Record(agent.Event{Kind: agent.ModelMessageEvent, Usage: &agent.Usage{InputTokens: 1000}})

	if _, isKnown := tracker.Spend(); isKnown {
		t.Error("expected an unpriced model to spend nothing knowable")
	}
}

func TestSpendChargesFreshInputCacheAndOutputSeparately(t *testing.T) {
	prices := testPrices
	tracker := New(Settings{Prices: &prices})

	tracker.Record(agent.Event{
		Kind: agent.ModelMessageEvent,
		Usage: &agent.Usage{
			InputTokens:  1_000_000,
			OutputTokens: 1_000_000,
			Cache:        &agent.CacheUsage{ReadTokens: 500_000, WriteTokens: 100_000},
		},
	})

	spend, isKnown := tracker.Spend()
	if !isKnown {
		t.Fatal("expected a priced model to report its spend")
	}

	want := 0.4*3 + 0.5*0.3 + 0.1*3.75 + 15
	if math.Abs(spend-want) > 1e-9 {
		t.Errorf("expected %v, got %v", want, spend)
	}
}

func TestSpendIgnoresTheCacheRebuildNotice(t *testing.T) {
	prices := testPrices
	tracker := New(Settings{Prices: &prices})

	tracker.Record(agent.Event{
		Kind:  agent.CacheRebuildEvent,
		Usage: &agent.Usage{Cache: &agent.CacheUsage{ReadTokens: 1_000_000, WriteTokens: 1_000_000}},
	})

	if spend, _ := tracker.Spend(); spend != 0 {
		t.Errorf("expected a derived notice to cost nothing, got %v", spend)
	}
}

func TestSpendIsRestoredFromTheWholeHistory(t *testing.T) {
	prices := testPrices
	tracker := New(Settings{Prices: &prices})

	tracker.Restore([]agent.Event{
		{Kind: agent.ModelMessageEvent, Usage: &agent.Usage{InputTokens: 1_000_000}},
		{Kind: agent.ToolCallRequestEvent, Usage: &agent.Usage{OutputTokens: 1_000_000}},
	}, []session.TurnSummary{{InputTokens: 12_000}})

	spend, _ := tracker.Spend()
	if math.Abs(spend-18) > 1e-9 {
		t.Errorf("expected the whole history to be counted, got %v", spend)
	}
}
