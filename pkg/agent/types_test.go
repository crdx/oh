package agent_test

import (
	"testing"

	"crdx.org/io/pkg/agent"
)

func TestEachModelFallsIntoAPriceTier(t *testing.T) {
	for name, test := range map[string]struct {
		prices agent.TokenPrices
		want   agent.PriceTier
	}{
		"nothing quoted":          {prices: agent.TokenPrices{}, want: agent.PriceUnknown},
		"a small local model":     {prices: agent.TokenPrices{Input: 0.25, Output: 2}, want: agent.PriceLow},
		"a fast hosted model":     {prices: agent.TokenPrices{Input: 0.8, Output: 4}, want: agent.PriceLow},
		"a workhorse":             {prices: agent.TokenPrices{Input: 1.25, Output: 10}, want: agent.PriceMedium},
		"a capable model":         {prices: agent.TokenPrices{Input: 3, Output: 15}, want: agent.PriceMedium},
		"the top of the range":    {prices: agent.TokenPrices{Input: 15, Output: 75}, want: agent.PriceHigh},
		"one that beggars belief": {prices: agent.TokenPrices{Input: 150, Output: 600}, want: agent.PriceExtreme},
		"exactly on fair":         {prices: agent.TokenPrices{Input: 2, Output: 2}, want: agent.PriceMedium},
		"exactly on dear":         {prices: agent.TokenPrices{Input: 10, Output: 10}, want: agent.PriceHigh},
		"exactly on silly":        {prices: agent.TokenPrices{Input: 50, Output: 50}, want: agent.PriceExtreme},
		"output alone is dear":    {prices: agent.TokenPrices{Output: 160}, want: agent.PriceHigh},
	} {
		t.Run(name, func(t *testing.T) {
			if got := test.prices.Tier(); got != test.want {
				t.Errorf("got %q (rate %v), want %q", got, test.prices.BlendedRate(), test.want)
			}
		})
	}
}

func TestAnUnknownTierIsNamedByNothing(t *testing.T) {
	if got := agent.PriceUnknown.String(); got != "" {
		t.Errorf("got %q, want nothing", got)
	}
}
