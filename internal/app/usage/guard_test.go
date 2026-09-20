package usage_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/usage"
	"crdx.org/oh/internal/req"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
)

type providerStub struct {
	err          error
	probe        agent.UsageProbe
	probeErr     error
	sendCount    int
	sendStarted  chan struct{}
	probeCount   int
	probeStarted chan struct{}
	releaseProbe chan struct{}
	state        []json.RawMessage
}

func (*providerStub) Configure(string, []tool.Definition) {}

func (*providerStub) AddUserMessage(string) {}

func (*providerStub) AddToolResults([]agent.ToolCallResult) {}

func (self *providerStub) Send(context.Context, agent.Yield) (agent.Reply, error) {
	self.sendCount++
	if self.sendStarted != nil {
		self.sendStarted <- struct{}{}
	}

	return agent.Reply{}, self.err
}

func (self *providerStub) ProbeUsage(context.Context) (agent.UsageProbe, error) {
	self.probeCount++
	if self.probeStarted != nil {
		self.probeStarted <- struct{}{}
	}
	if self.releaseProbe != nil {
		<-self.releaseProbe
	}

	return self.probe, self.probeErr
}

func (self *providerStub) Dump() []json.RawMessage {
	return self.state
}

func (self *providerStub) Load(state []json.RawMessage) {
	self.state = state
}

func stoppedContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	return ctx
}

func guardSettings(path string, modelName string, clock *testClock) usage.GuardSettings {
	return usage.GuardSettings{
		ProviderName: "Codex",
		ModelName:    modelName,
		CachePath:    path,
		Now:          clock.read,
	}
}

func TestAProviderUsageLimitStopsOtherSessionsBeforeTheySend(t *testing.T) {
	path := cachePath(t)
	clock := &testClock{now: testNow}
	window := agent.UsageWindow{
		Duration:  7 * 24 * time.Hour,
		Percent:   100,
		ResetsAt:  testNow.Add(2 * time.Hour),
		IsLimited: true,
	}

	firstLimit := &agent.UsageLimitError{Cause: errors.New("the endpoint refused usage"), Windows: []agent.UsageWindow{window}}
	first := &providerStub{err: firstLimit}
	firstGuard := usage.Guard(stoppedContext(t), first, guardSettings(path, "gpt-5.6-sol", clock))
	if _, err := firstGuard.Send(t.Context(), func(agent.Output) bool { return true }); !errors.Is(err, firstLimit) {
		t.Fatalf("got %v, want the provider's error", err)
	}

	second := &providerStub{}
	secondGuard := usage.Guard(stoppedContext(t), second, guardSettings(path, "gpt-5.6-sol", clock))
	_, err := secondGuard.Send(t.Context(), func(agent.Output) bool { return true })
	if err == nil || !strings.Contains(err.Error(), "Codex usage is limited until 2 Jan at 14:00 UTC") {
		t.Fatalf("got %v", err)
	}
	if second.sendCount != 0 {
		t.Errorf("the second provider was sent %d requests", second.sendCount)
	}
}

func TestAnObservedLimitWaitsBehindTheProbeAndIsThenShared(t *testing.T) {
	path := cachePath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	clock := &testClock{now: testNow}
	release := holdTheLock(t, path)
	isReleased := false
	defer func() {
		if !isReleased {
			release()
		}
	}()

	window := agent.UsageWindow{
		Duration:  time.Hour,
		Percent:   100,
		ResetsAt:  testNow.Add(time.Hour),
		IsLimited: true,
	}
	limit := &agent.UsageLimitError{Cause: errors.New("limited"), Windows: []agent.UsageWindow{window}}
	sendStarted := make(chan struct{})
	first := &providerStub{err: limit, sendStarted: sendStarted}
	result := make(chan error, 1)
	go func() {
		_, err := usage.Guard(stoppedContext(t), first, guardSettings(path, "gpt-5.6-sol", clock)).Send(
			t.Context(), func(agent.Output) bool { return true },
		)
		result <- err
	}()
	<-sendStarted

	timer := time.NewTimer(100 * time.Millisecond)
	select {
	case err := <-result:
		timer.Stop()
		t.Fatalf("the limit returned before the held probe ended: %v", err)
	case <-timer.C:
	}

	release()
	isReleased = true
	select {
	case err := <-result:
		if !errors.Is(err, limit) {
			t.Fatalf("got %v, want the provider limit", err)
		}
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}

	second := &providerStub{}
	_, err := usage.Guard(stoppedContext(t), second, guardSettings(path, "gpt-5.6-sol", clock)).Send(
		t.Context(), func(agent.Output) bool { return true },
	)
	if err == nil || second.sendCount != 0 {
		t.Fatalf("the shared limit was lost: err=%v sends=%d", err, second.sendCount)
	}
}

func TestARefusalBecomesAUsageLimitWhenTheAccountProbeSaysSo(t *testing.T) {
	path := cachePath(t)
	clock := &testClock{now: testNow}
	window := agent.UsageWindow{
		Duration:  time.Hour,
		Percent:   100,
		ResetsAt:  testNow.Add(time.Hour),
		IsLimited: true,
	}
	provider := &providerStub{
		err: &req.StatusError{Status: 429, Message: "slow down"},
		probe: agent.UsageProbe{
			Windows:      []agent.UsageWindow{window},
			Availability: agent.UsageAvailabilityLimited,
		},
	}

	_, err := usage.Guard(stoppedContext(t), provider, guardSettings(path, "gpt-5.6-sol", clock)).Send(
		t.Context(), func(agent.Output) bool { return true },
	)
	if err == nil || !strings.Contains(err.Error(), "Codex usage is limited") {
		t.Fatalf("got %v", err)
	}
	if provider.probeCount != 1 || provider.sendCount != 1 {
		t.Errorf("got %d probes and %d sends", provider.probeCount, provider.sendCount)
	}
}

func TestATemporaryRefusalIsNotProbedRepeatedly(t *testing.T) {
	path := cachePath(t)
	clock := &testClock{now: testNow}
	provider := &providerStub{
		err: &req.StatusError{Status: 429, Message: "slow down"},
		probe: agent.UsageProbe{
			Windows: []agent.UsageWindow{{
				Duration: time.Hour,
				Percent:  40,
			}},
			Availability: agent.UsageAvailabilityAllowed,
		},
	}
	guard := usage.Guard(stoppedContext(t), provider, guardSettings(path, "gpt-5.6-sol", clock))

	for range 2 {
		_, err := guard.Send(t.Context(), func(agent.Output) bool { return true })
		var retriable agent.Retriable
		if !errors.As(err, &retriable) || !retriable.Retriable() {
			t.Fatalf("the temporary refusal was not retriable: %v", err)
		}
	}
	if provider.probeCount != 1 || provider.sendCount != 2 {
		t.Errorf("got %d probes and %d sends", provider.probeCount, provider.sendCount)
	}
}

func TestAProviderUsageLimitIsNotProbedAgainBeforeTheSharedInterval(t *testing.T) {
	path := cachePath(t)
	clock := &testClock{now: testNow}
	window := agent.UsageWindow{
		Duration:  time.Hour,
		Percent:   100,
		ResetsAt:  testNow.Add(time.Hour),
		IsLimited: true,
	}

	first := &providerStub{err: &agent.UsageLimitError{Cause: errors.New("limited"), Windows: []agent.UsageWindow{window}}}
	_, _ = usage.Guard(stoppedContext(t), first, guardSettings(path, "gpt-5.6-sol", clock)).Send(
		t.Context(), func(agent.Output) bool { return true },
	)

	second := &providerStub{probe: agent.UsageProbe{Availability: agent.UsageAvailabilityAllowed}}
	_, err := usage.Guard(stoppedContext(t), second, guardSettings(path, "gpt-5.6-sol", clock)).Send(
		t.Context(), func(agent.Output) bool { return true },
	)
	if err == nil {
		t.Fatal("expected the recorded limit to stand")
	}
	if second.probeCount != 0 || second.sendCount != 0 {
		t.Errorf("got %d probes and %d sends", second.probeCount, second.sendCount)
	}
}

func TestADueProbeDiscoversAnEarlyResetBeforeSending(t *testing.T) {
	path := cachePath(t)
	clock := &testClock{now: testNow}
	limitedWindow := agent.UsageWindow{
		Duration:  time.Hour,
		Percent:   100,
		ResetsAt:  testNow.Add(time.Hour),
		IsLimited: true,
	}

	first := &providerStub{err: &agent.UsageLimitError{Cause: errors.New("limited"), Windows: []agent.UsageWindow{limitedWindow}}}
	_, _ = usage.Guard(stoppedContext(t), first, guardSettings(path, "gpt-5.6-sol", clock)).Send(
		t.Context(), func(agent.Output) bool { return true },
	)

	clock.set(testNow.Add(17 * time.Minute))
	availableWindow := limitedWindow
	availableWindow.Percent = 0
	availableWindow.IsLimited = false
	second := &providerStub{probe: agent.UsageProbe{
		Windows:      []agent.UsageWindow{availableWindow},
		Availability: agent.UsageAvailabilityAllowed,
	}}
	if _, err := usage.Guard(stoppedContext(t), second, guardSettings(path, "gpt-5.6-sol", clock)).Send(
		t.Context(), func(agent.Output) bool { return true },
	); err != nil {
		t.Fatal(err)
	}
	if second.probeCount != 1 || second.sendCount != 1 {
		t.Errorf("got %d probes and %d sends", second.probeCount, second.sendCount)
	}
}

func TestAProviderWideRecoveryDoesNotClearAModelScopedLimit(t *testing.T) {
	path := cachePath(t)
	clock := &testClock{now: testNow}
	scopedLimit := agent.UsageWindow{
		Duration:  time.Hour,
		Percent:   100,
		ResetsAt:  testNow.Add(2 * time.Hour),
		Scope:     "spark",
		IsLimited: true,
	}
	seed := &providerStub{err: &agent.UsageLimitError{Cause: errors.New("limited"), Windows: []agent.UsageWindow{scopedLimit}}}
	_, _ = usage.Guard(stoppedContext(t), seed, guardSettings(path, "gpt-5.3-codex-spark", clock)).Send(
		t.Context(), func(agent.Output) bool { return true },
	)

	clock.set(testNow.Add(17 * time.Minute))
	provider := &providerStub{probe: agent.UsageProbe{
		Windows: []agent.UsageWindow{
			{Duration: time.Hour, Percent: 10, ResetsAt: testNow.Add(time.Hour)},
			scopedLimit,
		},
		Availability: agent.UsageAvailabilityAllowed,
	}}
	_, err := usage.Guard(stoppedContext(t), provider, guardSettings(path, "gpt-5.3-codex-spark", clock)).Send(
		t.Context(), func(agent.Output) bool { return true },
	)
	if err == nil {
		t.Fatal("expected the scoped limit to stand")
	}
	if provider.probeCount != 1 || provider.sendCount != 0 {
		t.Errorf("got %d probes and %d sends", provider.probeCount, provider.sendCount)
	}
}

func TestAProbeHonoursTheProvidersRefreshInterval(t *testing.T) {
	path := cachePath(t)
	clock := &testClock{now: testNow}
	limitedWindow := agent.UsageWindow{
		Duration:  time.Hour,
		Percent:   100,
		ResetsAt:  testNow.Add(2 * time.Hour),
		IsLimited: true,
	}

	first := &providerStub{err: &agent.UsageLimitError{Cause: errors.New("limited"), Windows: []agent.UsageWindow{limitedWindow}}}
	_, _ = usage.Guard(stoppedContext(t), first, guardSettings(path, "gpt-5.6-sol", clock)).Send(
		t.Context(), func(agent.Output) bool { return true },
	)

	clock.set(testNow.Add(17 * time.Minute))
	second := &providerStub{probe: agent.UsageProbe{
		Windows:      []agent.UsageWindow{limitedWindow},
		Availability: agent.UsageAvailabilityLimited,
		RefreshAfter: 30 * time.Minute,
	}}
	_, _ = usage.Guard(stoppedContext(t), second, guardSettings(path, "gpt-5.6-sol", clock)).Send(
		t.Context(), func(agent.Output) bool { return true },
	)
	if second.probeCount != 1 {
		t.Fatalf("got %d probes", second.probeCount)
	}

	clock.set(testNow.Add(40 * time.Minute))
	third := &providerStub{probe: second.probe}
	_, _ = usage.Guard(stoppedContext(t), third, guardSettings(path, "gpt-5.6-sol", clock)).Send(
		t.Context(), func(agent.Output) bool { return true },
	)
	if third.probeCount != 0 {
		t.Errorf("the provider was probed %d times before its interval", third.probeCount)
	}
}

func TestOnlyOneSessionProbesAProviderAtATime(t *testing.T) {
	path := cachePath(t)
	clock := &testClock{now: testNow}
	limitedWindow := agent.UsageWindow{
		Duration:  2 * time.Hour,
		Percent:   100,
		ResetsAt:  testNow.Add(2 * time.Hour),
		IsLimited: true,
	}

	seed := &providerStub{err: &agent.UsageLimitError{Cause: errors.New("limited"), Windows: []agent.UsageWindow{limitedWindow}}}
	_, _ = usage.Guard(stoppedContext(t), seed, guardSettings(path, "gpt-5.6-sol", clock)).Send(
		t.Context(), func(agent.Output) bool { return true },
	)
	clock.set(testNow.Add(17 * time.Minute))

	probeStarted := make(chan struct{}, 1)
	releaseProbe := make(chan struct{})
	first := &providerStub{
		probe:        agent.UsageProbe{Windows: []agent.UsageWindow{limitedWindow}, Availability: agent.UsageAvailabilityLimited},
		probeStarted: probeStarted,
		releaseProbe: releaseProbe,
	}
	firstDone := make(chan struct{})
	go func() {
		_, _ = usage.Guard(stoppedContext(t), first, guardSettings(path, "gpt-5.6-sol", clock)).Send(
			t.Context(), func(agent.Output) bool { return true },
		)
		close(firstDone)
	}()
	<-probeStarted

	second := &providerStub{probe: first.probe}
	_, secondErr := usage.Guard(stoppedContext(t), second, guardSettings(path, "gpt-5.6-sol", clock)).Send(
		t.Context(), func(agent.Output) bool { return true },
	)
	if secondErr == nil {
		t.Fatal("expected the last shared limit to stand while the probe runs")
	}
	if second.probeCount != 0 || second.sendCount != 0 {
		t.Errorf("the second session made %d probes and %d sends", second.probeCount, second.sendCount)
	}

	close(releaseProbe)
	<-firstDone
	if first.probeCount != 1 {
		t.Errorf("the first session made %d probes", first.probeCount)
	}
}

func TestAFailedProbeHonoursRetryAfterAcrossSessions(t *testing.T) {
	path := cachePath(t)
	clock := &testClock{now: testNow}
	limitedWindow := agent.UsageWindow{
		Duration:  2 * time.Hour,
		Percent:   100,
		ResetsAt:  testNow.Add(2 * time.Hour),
		IsLimited: true,
	}

	first := &providerStub{err: &agent.UsageLimitError{Cause: errors.New("limited"), Windows: []agent.UsageWindow{limitedWindow}}}
	_, _ = usage.Guard(stoppedContext(t), first, guardSettings(path, "gpt-5.6-sol", clock)).Send(
		t.Context(), func(agent.Output) bool { return true },
	)

	clock.set(testNow.Add(17 * time.Minute))
	second := &providerStub{probeErr: &req.StatusError{Status: 429, Wait: 40 * time.Minute}}
	_, _ = usage.Guard(stoppedContext(t), second, guardSettings(path, "gpt-5.6-sol", clock)).Send(
		t.Context(), func(agent.Output) bool { return true },
	)
	if second.probeCount != 1 {
		t.Fatalf("got %d probes", second.probeCount)
	}

	clock.set(testNow.Add(50 * time.Minute))
	third := &providerStub{probeErr: second.probeErr}
	_, _ = usage.Guard(stoppedContext(t), third, guardSettings(path, "gpt-5.6-sol", clock)).Send(
		t.Context(), func(agent.Output) bool { return true },
	)
	if third.probeCount != 0 {
		t.Errorf("the provider was probed %d times before Retry-After", third.probeCount)
	}
}

func TestARecordedUsageLimitStopsBlockingAfterItsReset(t *testing.T) {
	path := cachePath(t)
	clock := &testClock{now: testNow}
	window := agent.UsageWindow{
		Duration:  time.Hour,
		Percent:   100,
		ResetsAt:  testNow.Add(time.Hour),
		IsLimited: true,
	}

	first := &providerStub{err: &agent.UsageLimitError{Cause: errors.New("limited"), Windows: []agent.UsageWindow{window}}}
	_, _ = usage.Guard(stoppedContext(t), first, guardSettings(path, "gpt-5.6-sol", clock)).Send(
		t.Context(), func(agent.Output) bool { return true },
	)

	clock.set(testNow.Add(time.Hour))
	second := &providerStub{}
	if _, err := usage.Guard(stoppedContext(t), second, guardSettings(path, "gpt-5.6-sol", clock)).Send(
		t.Context(), func(agent.Output) bool { return true },
	); err != nil {
		t.Fatal(err)
	}
	if second.sendCount != 1 {
		t.Errorf("the provider was sent %d requests", second.sendCount)
	}
}

func TestAModelScopedLimitLeavesOtherModelsAvailable(t *testing.T) {
	path := cachePath(t)
	clock := &testClock{now: testNow}
	window := agent.UsageWindow{
		Duration:  time.Hour,
		Percent:   100,
		ResetsAt:  testNow.Add(time.Hour),
		Scope:     "spark",
		IsLimited: true,
	}

	first := &providerStub{err: &agent.UsageLimitError{Cause: errors.New("limited"), Windows: []agent.UsageWindow{window}}}
	_, _ = usage.Guard(stoppedContext(t), first, guardSettings(path, "gpt-5.3-codex-spark", clock)).Send(
		t.Context(), func(agent.Output) bool { return true },
	)

	second := &providerStub{}
	if _, err := usage.Guard(stoppedContext(t), second, guardSettings(path, "gpt-5.6-sol", clock)).Send(
		t.Context(), func(agent.Output) bool { return true },
	); err != nil {
		t.Fatal(err)
	}
	if second.sendCount != 1 {
		t.Errorf("the provider was sent %d requests", second.sendCount)
	}
}

func TestARecoveryProbeTakesTheFreshReset(t *testing.T) {
	path := cachePath(t)
	clock := &testClock{now: testNow}
	storedResetsAt := testNow.Add(2 * time.Hour)

	seedLimit(t, path, clock, agent.UsageWindow{
		Duration:  7 * 24 * time.Hour,
		Percent:   100,
		ResetsAt:  storedResetsAt,
		IsLimited: true,
	})

	clock.set(testNow.Add(17 * time.Minute))

	freshResetsAt := testNow.Add(3 * time.Hour)
	provider := &providerStub{probe: agent.UsageProbe{
		Windows: []agent.UsageWindow{{
			Duration: 7 * 24 * time.Hour,
			Percent:  99,
			ResetsAt: freshResetsAt,
		}},
		Availability: agent.UsageAvailabilityAllowed,
	}}

	if _, err := usage.Guard(stoppedContext(t), provider, guardSettings(path, "gpt-5.6-sol", clock)).Send(
		t.Context(), func(agent.Output) bool { return true },
	); err != nil {
		t.Fatal(err)
	}

	got, err := usage.Shared(
		&scriptedReporter{isAvailable: true}, path, rate, clock.read,
	).UsageWindows(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || got[0].Percent != 99 || got[0].IsLimited {
		t.Fatalf("got %+v", got)
	}

	if !got[0].ResetsAt.Equal(freshResetsAt) {
		t.Errorf("the reset reads %s, want %s", got[0].ResetsAt, freshResetsAt)
	}
}
