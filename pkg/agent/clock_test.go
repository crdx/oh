package agent

import (
	"strings"
	"testing"
	"time"
)

const monotonicReading = " m=+"

func TestACacheReadingIsRememberedOnTheWallClockAlone(t *testing.T) {
	assistant := &Agent{cacheLifetime: time.Hour}

	askedAt := time.Now()
	if !strings.Contains(askedAt.String(), monotonicReading) {
		t.Fatalf("got %q, want a time carrying a monotonic reading", askedAt)
	}

	if _, wasRebuilt := assistant.readCache(Usage{Cache: &CacheUsage{ReadTokens: 400_000}}, askedAt); wasRebuilt {
		t.Fatal("got a rebuild on the first reading, want none")
	}

	if remembered := assistant.cache.At.String(); strings.Contains(remembered, monotonicReading) {
		t.Errorf("got %q, want no monotonic reading", remembered)
	}

	if !assistant.cache.At.Equal(askedAt) {
		t.Errorf("got %q, want the moment the request was made", assistant.cache.At)
	}
}

func TestAGapLongerThanTheCacheLifetimeIsReportedAsAnExpiry(t *testing.T) {
	assistant := &Agent{cacheLifetime: time.Hour}

	askedAt := time.Now()
	if _, wasRebuilt := assistant.readCache(Usage{Cache: &CacheUsage{ReadTokens: 400_000}}, askedAt); wasRebuilt {
		t.Fatal("got a rebuild on the first reading, want none")
	}

	notice, wasRebuilt := assistant.readCache(
		Usage{Cache: &CacheUsage{ReadTokens: 1907, WriteTokens: 587_469}},
		askedAt.Add(6*time.Hour),
	)

	if !wasRebuilt {
		t.Fatal("got no rebuild, want one")
	}

	if notice.Name != string(CacheExpired) {
		t.Errorf("got %q, want %q", notice.Name, CacheExpired)
	}

	if notice.Took != 6*time.Hour {
		t.Errorf("got %s, want 6h", notice.Took)
	}
}
