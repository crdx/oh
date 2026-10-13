package agent_test

import (
	"math"
	"testing"

	"crdx.org/oh/pkg/agent"
)

func TestAnEstimatedRequestCostsTheSameWorkloadAtEveryModelPrice(t *testing.T) {
	for name, test := range map[string]struct {
		prices agent.TokenPrices
		want   float64
	}{
		"all four prices": {
			prices: agent.TokenPrices{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75},
			want:   (947*3 + 167530*0.3 + 2667*3.75 + 467*15) / 1000000,
		},
		"automatic cache writes use input": {
			prices: agent.TokenPrices{Input: 1.25, Output: 10, CacheRead: 0.125},
			want:   (947*1.25 + 167530*0.125 + 2667*1.25 + 467*10) / 1000000,
		},
		"no cache rates use ordinary input": {
			prices: agent.TokenPrices{Input: 3, Output: 15},
			want:   (171144*3 + 467*15) / 1000000.0,
		},
		"a cache write rate without a read rate": {
			prices: agent.TokenPrices{Input: 3, Output: 15, CacheWrite: 3.75},
			want:   (168477*3 + 2667*3.75 + 467*15) / 1000000,
		},
		"nothing quoted": {want: 0},
	} {
		t.Run(name, func(t *testing.T) {
			if got := test.prices.EstimatedRequestCost(); math.Abs(got-test.want) > 1e-12 {
				t.Errorf("got %v, want %v", got, test.want)
			}
		})
	}
}

func TestCachePricingCanReverseTheOrderOfTwoModels(t *testing.T) {
	cheapInput := agent.TokenPrices{Input: 1, Output: 10, CacheRead: 0.5}
	cheapCache := agent.TokenPrices{Input: 2, Output: 10, CacheRead: 0.1}
	if cheapCache.EstimatedRequestCost() >= cheapInput.EstimatedRequestCost() {
		t.Error("the model with dearer fresh input should be cheaper for the cache-heavy workload")
	}

	dearWrites := cheapCache
	dearWrites.CacheWrite = 100
	if dearWrites.EstimatedRequestCost() <= cheapInput.EstimatedRequestCost() {
		t.Error("dearer cache writes should count against the estimate")
	}
}
