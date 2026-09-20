package usage

import (
	"context"
	"slices"
	"sync"
	"time"

	"crdx.org/io/pkg/agent"
)

const (
	cacheFormat          = 1
	snapshotPollInterval = 15 * time.Second
)

type sharedReporter struct {
	reporter agent.UsageReporter
	store    cacheStore
	ttl      time.Duration
	now      func() time.Time

	mutex              sync.Mutex
	windows            []agent.UsageWindow
	fetchedAt          time.Time
	nextSnapshotReadAt time.Time
}

type Snapshotter interface {
	agent.UsageReporter

	GetSnapshot() ([]agent.UsageWindow, time.Time)
}

func Shared(
	reporter agent.UsageReporter, path string, ttl time.Duration, now func() time.Time,
) agent.UsageReporter {
	if reporter == nil || path == "" {
		return reporter
	}

	readAt := now()
	self := &sharedReporter{
		reporter:           reporter,
		store:              newCacheStore(path),
		ttl:                ttl,
		now:                now,
		nextSnapshotReadAt: readAt.Truncate(snapshotPollInterval).Add(snapshotPollInterval),
	}

	storedCache := self.store.read()
	self.windows, self.fetchedAt = storedCache.Windows, storedCache.FetchedAt

	return self
}

func (self *sharedReporter) IsAvailable() bool {
	if self.reporter.IsAvailable() {
		return true
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	return len(self.windows) > 0
}

func (self *sharedReporter) GetSnapshot() ([]agent.UsageWindow, time.Time) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.windows, self.fetchedAt
}

func (self *sharedReporter) UsageWindows(ctx context.Context) ([]agent.UsageWindow, error) {
	hasClaimed, err := self.store.tryUpdate(func(storedCache *cache) error {
		if self.isFresh(*storedCache) {
			self.keep(storedCache.Windows, storedCache.FetchedAt)

			return nil
		}

		probe, isProbed, err := self.fetch(ctx, *storedCache)
		if err != nil {
			return err
		}

		windows := mergeWindows(storedCache.Windows, probe, self.now())
		if len(windows) == 0 {
			return nil
		}

		fetchedAt := self.now()

		if isProbed {
			storedCache.Probe = &probeState{
				AttemptedAt: fetchedAt,
				NextAt:      fetchedAt.Add(probeDelay(probeInterval(probe), self.store.path, fetchedAt)),
			}
		}

		storedCache.Version = cacheFormat
		storedCache.FetchedAt = fetchedAt
		storedCache.Windows = windows
		self.keep(windows, fetchedAt)

		return nil
	})
	if err != nil {
		return nil, err
	}

	if !hasClaimed {
		storedCache := self.store.read()
		self.keep(storedCache.Windows, storedCache.FetchedAt)
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.windows, nil
}

func (self *sharedReporter) fetch(
	ctx context.Context, storedCache cache,
) (agent.UsageProbe, bool, error) {
	prober, canProbe := self.reporter.(agent.UsageProber)
	if canProbe && hasActiveLimitedWindow(storedCache.Windows, self.now()) {
		probe, err := prober.ProbeUsage(ctx)

		return probe, true, err
	}

	windows, err := self.reporter.UsageWindows(ctx)

	return agent.UsageProbe{Windows: windows}, false, err
}

func probeInterval(probe agent.UsageProbe) time.Duration {
	if probe.RefreshAfter <= 0 {
		return defaultProbeInterval
	}

	return probe.RefreshAfter
}

func mergeWindows(
	storedWindows []agent.UsageWindow, probe agent.UsageProbe, now time.Time,
) []agent.UsageWindow {
	isSpokenFor := probe.Availability == agent.UsageAvailabilityAllowed

	windows := slices.Clone(probe.Windows)
	if len(windows) == 0 {
		if !isSpokenFor {
			return nil
		}

		windows = slices.Clone(storedWindows)

		for i := range windows {
			windows[i].IsLimited = false
		}

		return windows
	}

	for i, window := range windows {
		windows[i].ResetsAt = resetWithFallback(storedWindows, window, now)

		if isSpokenFor {
			continue
		}

		windows[i].IsLimited = window.IsLimited || wasLimited(storedWindows, window, now)
	}

	return windows
}

func resetWithFallback(
	storedWindows []agent.UsageWindow, window agent.UsageWindow, now time.Time,
) time.Time {
	if !window.ResetsAt.IsZero() {
		return window.ResetsAt
	}

	for _, candidate := range storedWindows {
		if isSameWindow(candidate, window) && candidate.ResetsAt.After(now) {
			return candidate.ResetsAt
		}
	}

	return time.Time{}
}

func wasLimited(storedWindows []agent.UsageWindow, window agent.UsageWindow, now time.Time) bool {
	return slices.ContainsFunc(storedWindows, func(candidate agent.UsageWindow) bool {
		hasNotReset := candidate.ResetsAt.IsZero() || candidate.ResetsAt.After(now)

		return candidate.IsLimited && hasNotReset && isSameWindow(candidate, window)
	})
}

func isSameWindow(left agent.UsageWindow, right agent.UsageWindow) bool {
	return left.Scope == right.Scope && left.Duration == right.Duration
}

func (self *sharedReporter) readSnapshot() ([]agent.UsageWindow, time.Time) {
	self.refreshSnapshot()

	return self.GetSnapshot()
}

func (self *sharedReporter) refreshSnapshot() {
	self.mutex.Lock()
	readAt := self.now()
	if readAt.Before(self.nextSnapshotReadAt) {
		self.mutex.Unlock()

		return
	}
	self.nextSnapshotReadAt = readAt.Truncate(snapshotPollInterval).Add(snapshotPollInterval)
	self.mutex.Unlock()

	storedCache := self.store.read()
	self.keep(storedCache.Windows, storedCache.FetchedAt)
}

func (self *sharedReporter) isFresh(storedCache cache) bool {
	now := self.now()

	if hasActiveLimitedWindow(storedCache.Windows, now) && storedCache.Probe != nil {
		return now.Before(storedCache.Probe.NextAt)
	}

	return !storedCache.FetchedAt.IsZero() && now.Sub(storedCache.FetchedAt) < self.ttl
}

func (self *sharedReporter) keep(windows []agent.UsageWindow, fetchedAt time.Time) {
	if len(windows) == 0 {
		return
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	if !self.fetchedAt.IsZero() && !fetchedAt.After(self.fetchedAt) {
		return
	}

	self.windows, self.fetchedAt = windows, fetchedAt
}
