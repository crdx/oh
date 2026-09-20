package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"slices"
	"strings"
	"time"

	"crdx.org/oh/internal/req"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
)

const (
	defaultProbeInterval = 15 * time.Minute
	maximumProbeBackoff  = time.Hour
	probeJitterDivisor   = 10
)

type StatefulProvider interface {
	agent.Provider
	agent.State
}

type GuardSettings struct {
	ProviderName string
	ModelName    string
	CachePath    string
	Now          func() time.Time
}

type guardedProvider struct {
	provider StatefulProvider
	prober   agent.UsageProber
	settings GuardSettings
	store    cacheStore
	wake     chan struct{}
}

func Guard(ctx context.Context, provider StatefulProvider, settings GuardSettings) StatefulProvider {
	if settings.CachePath == "" {
		return provider
	}
	if settings.Now == nil {
		settings.Now = time.Now
	}

	prober, _ := provider.(agent.UsageProber)
	self := &guardedProvider{
		provider: provider,
		prober:   prober,
		settings: settings,
		store:    newCacheStore(settings.CachePath),
		wake:     make(chan struct{}, 1),
	}

	if ctx.Err() == nil && prober != nil {
		go self.monitor(ctx)
	}

	return self
}

func (self *guardedProvider) Configure(systemPrompt string, tools []tool.Definition) {
	self.provider.Configure(systemPrompt, tools)
}

func (self *guardedProvider) CacheLifetime() time.Duration {
	if reporter, isReported := self.provider.(agent.CacheLifetimeReporter); isReported {
		return reporter.CacheLifetime()
	}

	return 0
}

func (self *guardedProvider) AddUserMessage(text string) {
	self.provider.AddUserMessage(text)
}

func (self *guardedProvider) AddToolResults(toolResults []agent.ToolCallResult) {
	self.provider.AddToolResults(toolResults)
}

func (self *guardedProvider) Dump() []json.RawMessage {
	return self.provider.Dump()
}

func (self *guardedProvider) Load(items []json.RawMessage) {
	self.provider.Load(items)
}

func (self *guardedProvider) Send(ctx context.Context, yield agent.Yield) (agent.Reply, error) {
	storedCache := self.refreshIfDue(ctx)
	if windows := activeLimitedWindows(storedCache.Windows, self.settings.ModelName, self.settings.Now()); len(windows) > 0 {
		return agent.Reply{}, limitedError(self.settings.ProviderName, windows, self.settings.Now())
	}

	reply, err := self.provider.Send(ctx, yield)
	limit, isLimited := errors.AsType[*agent.UsageLimitError](err)
	if !isLimited {
		if learnedLimit := self.limitAfterRefusal(ctx, err); learnedLimit != nil {
			return reply, learnedLimit
		}

		return reply, err
	}

	if storeErr := self.recordLimit(limit.Windows); storeErr != nil {
		return reply, errors.Join(limit, fmt.Errorf("share provider usage: %w", storeErr))
	}
	self.wakeMonitor()

	return reply, limit
}

func (self *guardedProvider) limitAfterRefusal(ctx context.Context, err error) error {
	refusal, isRefusal := errors.AsType[*req.StatusError](err)
	if !isRefusal || refusal.Status != http.StatusTooManyRequests || self.prober == nil {
		return nil
	}

	_, _ = self.store.tryUpdate(func(storedCache *cache) error {
		now := self.settings.Now()
		if storedCache.Probe != nil && now.Before(storedCache.Probe.NextAt) {
			return nil
		}

		self.updateFromProbe(ctx, storedCache, now)

		return nil
	})

	storedCache := self.store.read()
	windows := activeLimitedWindows(storedCache.Windows, self.settings.ModelName, self.settings.Now())
	if len(windows) == 0 {
		return nil
	}

	return limitedError(self.settings.ProviderName, windows, self.settings.Now())
}

func (self *guardedProvider) recordLimit(windows []agent.UsageWindow) error {
	if len(windows) == 0 {
		return nil
	}

	now := self.settings.Now()

	return self.store.update(func(storedCache *cache) error {
		storedCache.Version = cacheFormat
		storedCache.FetchedAt = now
		storedCache.Windows = slices.Clone(windows)
		storedCache.Probe = &probeState{
			AttemptedAt: now,
			NextAt:      now.Add(probeDelay(defaultProbeInterval, self.settings.CachePath, now)),
		}

		return nil
	})
}

func (self *guardedProvider) refreshIfDue(ctx context.Context) cache {
	storedCache := self.store.read()
	if ctx.Err() != nil || self.prober == nil || !probeIsDue(storedCache, self.settings.ModelName, self.settings.Now()) {
		return storedCache
	}

	_, _ = self.store.tryUpdate(func(currentCache *cache) error {
		now := self.settings.Now()
		if !probeIsDue(*currentCache, self.settings.ModelName, now) {
			return nil
		}

		self.updateFromProbe(ctx, currentCache, now)

		return nil
	})

	return self.store.read()
}

func (self *guardedProvider) updateFromProbe(ctx context.Context, storedCache *cache, now time.Time) {
	probe, err := self.prober.ProbeUsage(ctx)
	if ctx.Err() != nil {
		return
	}
	storedCache.Version = cacheFormat
	if err != nil {
		failures := 1
		if storedCache.Probe != nil {
			failures = storedCache.Probe.Failures + 1
		}
		wait := failedProbeDelay(err, failures)
		storedCache.Probe = &probeState{
			AttemptedAt: now,
			NextAt:      now.Add(probeDelay(wait, self.settings.CachePath, now)),
			Failures:    failures,
			Failure:     err.Error(),
		}

		return
	}

	storedCache.Probe = &probeState{
		AttemptedAt: now,
		NextAt:      now.Add(probeDelay(probeInterval(probe), self.settings.CachePath, now)),
	}
	applyProbe(storedCache, probe, now)
}

func (self *guardedProvider) monitor(ctx context.Context) {
	ticker := time.NewTicker(snapshotPollInterval)
	defer ticker.Stop()

	self.refreshIfDue(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-self.wake:
			self.refreshIfDue(ctx)
		case <-ticker.C:
			self.refreshIfDue(ctx)
		}
	}
}

func (self *guardedProvider) wakeMonitor() {
	select {
	case self.wake <- struct{}{}:
	default:
	}
}

func applyProbe(storedCache *cache, probe agent.UsageProbe, now time.Time) {
	if probe.Availability == agent.UsageAvailabilityUnknown {
		return
	}

	if windows := mergeWindows(storedCache.Windows, probe, now); len(windows) > 0 {
		storedCache.Windows = windows
	}

	storedCache.FetchedAt = now
}

func probeIsDue(storedCache cache, modelName string, now time.Time) bool {
	if len(activeLimitedWindows(storedCache.Windows, modelName, now)) == 0 {
		return false
	}

	return storedCache.Probe == nil || !now.Before(storedCache.Probe.NextAt)
}

func failedProbeDelay(err error, failures int) time.Duration {
	wait := defaultProbeInterval
	for range min(failures-1, 2) {
		wait *= 2
	}
	wait = min(wait, maximumProbeBackoff)

	if retriable, isRetriable := errors.AsType[agent.Retriable](err); isRetriable {
		wait = max(wait, retriable.RetryAfter())
	}

	return wait
}

func probeDelay(wait time.Duration, path string, at time.Time) time.Duration {
	spread := wait / probeJitterDivisor
	if spread <= 0 {
		return wait
	}

	hash := fnv.New64a()
	_, _ = hash.Write([]byte(path))
	_, _ = hash.Write([]byte(at.UTC().Format(time.RFC3339Nano)))

	jitter := time.Duration(int64(hash.Sum64()>>1) % int64(spread+1))

	return wait + jitter
}

func hasActiveLimitedWindow(windows []agent.UsageWindow, now time.Time) bool {
	for _, window := range windows {
		hasNotReset := window.ResetsAt.IsZero() || window.ResetsAt.After(now)
		if window.IsLimited && hasNotReset {
			return true
		}
	}

	return false
}

func IsSelectionAvailable(path string, modelName string, now time.Time) bool {
	if path == "" {
		return true
	}

	return len(activeLimitedWindows(newCacheStore(path).read().Windows, modelName, now)) == 0
}

func activeLimitedWindows(
	windows []agent.UsageWindow, modelName string, now time.Time,
) []agent.UsageWindow {
	modelName = strings.ToLower(modelName)
	limitedWindows := make([]agent.UsageWindow, 0, len(windows))

	for _, window := range windows {
		isApplicable := window.Scope == "" || strings.Contains(modelName, strings.ToLower(window.Scope))
		hasNotReset := window.ResetsAt.IsZero() || window.ResetsAt.After(now)
		if window.IsLimited && isApplicable && hasNotReset {
			limitedWindows = append(limitedWindows, window)
		}
	}

	return limitedWindows
}

func limitedError(providerName string, windows []agent.UsageWindow, now time.Time) error {
	message := providerName + " usage is limited"
	var latestReset time.Time
	for _, window := range windows {
		if window.ResetsAt.After(latestReset) {
			latestReset = window.ResetsAt
		}
	}
	if !latestReset.IsZero() {
		message += " until " + latestReset.In(now.Location()).Format("2 Jan at 15:04 MST")
	}

	return &agent.UsageLimitError{Cause: errors.New(message), Windows: windows}
}
