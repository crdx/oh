package subUsage

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"crdx.org/io/internal/app/segment"
	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/app/usage"
	"crdx.org/io/internal/req"
	"crdx.org/io/pkg/agent"
)

type noOptions struct{}

func (noOptions) Read(any) error { return nil }

type scriptedReporter struct {
	windows []agent.UsageWindow
	err     error
	asked   atomic.Int64
}

func (self *scriptedReporter) IsAvailable() bool {
	return true
}

func (self *scriptedReporter) UsageWindows(context.Context) ([]agent.UsageWindow, error) {
	self.asked.Add(1)

	return self.windows, self.err
}

type unavailableReporter struct {
	asked atomic.Int64
}

func (self *unavailableReporter) IsAvailable() bool {
	return false
}

func (self *unavailableReporter) UsageWindows(context.Context) ([]agent.UsageWindow, error) {
	self.asked.Add(1)

	return nil, nil
}

var testNow = time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)

func phaseAt(at time.Time) segment.Phase {
	return segment.Phase{At: at}
}

type testClock struct {
	now time.Time
}

func (self *testClock) read() time.Time {
	return self.now
}

func (self *testClock) set(now time.Time) {
	self.now = now
}

func build(t *testing.T, reporter agent.UsageReporter) segment.Segment {
	t.Helper()

	return buildOnClock(t, reporter, &testClock{now: testNow})
}

func buildOnClock(t *testing.T, reporter agent.UsageReporter, clock *testClock) segment.Segment {
	t.Helper()

	return buildOnModel(t, reporter, "", clock)
}

func buildOnModel(
	t *testing.T, reporter agent.UsageReporter, modelName string, clock *testClock,
) segment.Segment {
	t.Helper()

	built, err := New(Settings{
		Reporter:         reporter,
		ModelName:        modelName,
		IsSelfRefreshing: true,
		Now:              clock.read,
	})(noOptions{})
	if err != nil {
		t.Fatal(err)
	}

	return built
}

func settle(t *testing.T, built segment.Segment) {
	t.Helper()

	renderSettled(t, built)
}

func renderSettled(t *testing.T, built segment.Segment) string {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)

	for {
		text := style.Plain(built.Render(segment.Context{}))
		if text == "usage unavailable" || !strings.HasPrefix(text, "usage ") {
			return text
		}

		if time.Now().After(deadline) {
			t.Fatal("the fetch never landed")
		}

		time.Sleep(time.Millisecond)
	}
}

func TestASharedSnapshotIsDrawnOnTheFirstPaint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage", "codex.json")
	clock := &testClock{now: testNow}

	first := &scriptedReporter{windows: []agent.UsageWindow{{
		Duration: 5 * time.Hour,
		Percent:  40,
		ResetsAt: testNow.Add(150 * time.Minute),
	}}}

	if _, err := usage.Shared(first, path, defaultRate, clock.read).UsageWindows(t.Context()); err != nil {
		t.Fatal(err)
	}

	second := &scriptedReporter{}

	built, err := New(Settings{
		Reporter:         second,
		CachePath:        path,
		IsSelfRefreshing: true,
		Now:              clock.read,
	})(noOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if got := style.Plain(built.Render(segment.Context{})); got != "● 5h 40% ███░┃░░░" {
		t.Errorf("first paint = %q", got)
	}

	if asked := second.asked.Load(); asked != 0 {
		t.Errorf("a fresh snapshot was fetched again %d times", asked)
	}
}

func TestANilReporterSaysUsageIsNotApplicable(t *testing.T) {
	built := build(t, nil)

	if got := style.Plain(built.Render(segment.Context{})); got != "usage n/a" {
		t.Errorf("got %q", got)
	}
}

func TestAnUnavailableReporterIsNotAskedForUsage(t *testing.T) {
	reporter := &unavailableReporter{}
	built := build(t, reporter)

	if got := style.Plain(built.Render(segment.Context{})); got != "usage n/a" {
		t.Errorf("got %q", got)
	}

	if asked := reporter.asked.Load(); asked != 0 {
		t.Errorf("unavailable reporter was asked %d times", asked)
	}
}

func TestWindowsOnAnEvenBurnRenderPlainPercentages(t *testing.T) {
	built := build(t, &scriptedReporter{windows: []agent.UsageWindow{
		{
			Duration: 5 * time.Hour,
			Percent:  40,
			ResetsAt: testNow.Add(150 * time.Minute),
		},
		{
			Duration: 7 * 24 * time.Hour,
			Percent:  12,
			ResetsAt: testNow.Add(6 * 24 * time.Hour),
		},
	}})

	if got := renderSettled(t, built); got != "● 5h 40% ███░┃░░░ wk 12% ░┃░░░░░░" {
		t.Errorf("got %q", got)
	}
}

func TestAWindowOverPaceIsMarked(t *testing.T) {
	built := build(t, &scriptedReporter{windows: []agent.UsageWindow{{
		Duration: 5 * time.Hour,
		Percent:  28,
		ResetsAt: testNow.Add(4 * time.Hour),
	}}})

	if got := renderSettled(t, built); got != "● 5h 28% █┃░░░░░░" {
		t.Errorf("got %q", got)
	}
}

func TestScopedWindowsFollowTheirOwnMark(t *testing.T) {
	built := buildOnModel(t, &scriptedReporter{windows: []agent.UsageWindow{
		{Duration: 5 * time.Hour, Percent: 40, ResetsAt: testNow.Add(150 * time.Minute)},
		{
			Duration: 5 * time.Hour,
			Percent:  8,
			ResetsAt: testNow.Add(150 * time.Minute),
			Scope:    "gpt-5.3-codex-spark",
		},
		{
			Duration: 7 * 24 * time.Hour,
			Percent:  3,
			ResetsAt: testNow.Add(6 * 24 * time.Hour),
			Scope:    "gpt-5.3-codex-spark",
		},
	}}, "gpt-5.3-codex-spark", &testClock{now: testNow})

	if got := renderSettled(t, built); got != "● 5h 40% ███░┃░░░ ⚡ 5h 8% ░░░░┃░░░ wk 3% ░┃░░░░░░" {
		t.Errorf("got %q", got)
	}
}

func TestAWindowMeteringAnotherModelIsNotDrawn(t *testing.T) {
	built := buildOnModel(t, &scriptedReporter{windows: []agent.UsageWindow{
		{Duration: 5 * time.Hour, Percent: 40, ResetsAt: testNow.Add(150 * time.Minute)},
		{
			Duration: 5 * time.Hour,
			Percent:  8,
			ResetsAt: testNow.Add(150 * time.Minute),
			Scope:    "gpt-5.3-codex-spark",
		},
	}}, "gpt-5.6-sol", &testClock{now: testNow})

	if got := renderSettled(t, built); got != "● 5h 40% ███░┃░░░" {
		t.Errorf("got %q", got)
	}
}

func TestAWindowWithoutAResetShowsItsFigureAnyway(t *testing.T) {
	built := build(t, &scriptedReporter{windows: []agent.UsageWindow{{
		Duration: 7 * 24 * time.Hour,
		Percent:  6,
	}}})

	if got := renderSettled(t, built); got != "● wk 6% ░░░░░░░░" {
		t.Errorf("got %q", got)
	}
}

func TestAnExpiredWindowReadsAsStale(t *testing.T) {
	built := build(t, &scriptedReporter{windows: []agent.UsageWindow{{
		Duration: 5 * time.Hour,
		Percent:  68,
		ResetsAt: testNow.Add(-time.Minute),
	}}})

	if got := renderSettled(t, built); got != "● 5h stale" {
		t.Errorf("got %q", got)
	}
}

func TestAFreshSnapshotIsNotFetchedAgain(t *testing.T) {
	reporter := &scriptedReporter{windows: []agent.UsageWindow{{
		Duration: 5 * time.Hour,
		Percent:  40,
		ResetsAt: testNow.Add(150 * time.Minute),
	}}}

	built := build(t, reporter)

	renderSettled(t, built)
	renderSettled(t, built)

	if asked := reporter.asked.Load(); asked != 1 {
		t.Errorf("expected one fetch, got %d", asked)
	}
}

func TestRefreshingASnapshotKeepsTheSettledDisplayStable(t *testing.T) {
	reporter := &scriptedReporter{windows: []agent.UsageWindow{{
		Duration: 5 * time.Hour,
		Percent:  40,
		ResetsAt: testNow.Add(150 * time.Minute),
	}}}
	clock := &testClock{now: testNow}
	built := buildState(t, reporter, clock)

	fetchNow(t, built)
	clock.set(testNow.Add(defaultRate))
	if !built.noteFetchStarting() {
		t.Fatal("refresh did not start")
	}

	want := "● 5h 40% ███░┃░░░"
	if got := style.Plain(built.Render(segment.Context{})); got != want {
		t.Errorf("refresh start = %q, want %q", got, want)
	}

	clock.set(clock.read().Add(spinnerDelay))
	if got := style.Plain(built.Render(segment.Context{})); got != want {
		t.Errorf("long refresh = %q, want %q", got, want)
	}

	if got := built.NextRefresh(phaseAt(clock.read())); !got.Equal(testNow.Add(defaultRate + redrawInterval)) {
		t.Errorf("background refresh = %s", got.Sub(clock.read()))
	}
}

func TestScopedWindowsAloneStillDrawTheLine(t *testing.T) {
	built := buildOnModel(t, &scriptedReporter{windows: []agent.UsageWindow{{
		Duration: 7 * 24 * time.Hour,
		Percent:  3,
		ResetsAt: testNow.Add(6 * 24 * time.Hour),
		Scope:    "opus",
	}}}, "claude-opus-4-6", &testClock{now: testNow})

	if got := renderSettled(t, built); got != "● ⚡ wk 3% ░┃░░░░░░" {
		t.Errorf("got %q", got)
	}
}

func TestASnapshotMeteringOnlyOtherModelsSaysSo(t *testing.T) {
	built := buildOnModel(t, &scriptedReporter{windows: []agent.UsageWindow{{
		Duration: 7 * 24 * time.Hour,
		Percent:  3,
		ResetsAt: testNow.Add(6 * 24 * time.Hour),
		Scope:    "opus",
	}}}, "claude-sonnet-4-6", &testClock{now: testNow})

	if got := renderSettled(t, built); got != "usage unavailable" {
		t.Errorf("got %q", got)
	}
}

func TestAnActiveFetchOnlyShowsASpinnerAfterTheDelay(t *testing.T) {
	clock := &testClock{now: testNow}
	built := buildState(t, &scriptedReporter{}, clock)

	if !built.noteFetchStarting() {
		t.Fatal("initial fetch did not start")
	}

	clock.set(testNow.Add(spinnerDelay - time.Nanosecond))
	if got := style.Plain(built.Render(segment.Context{})); got != "usage pending" {
		t.Errorf("short fetch = %q", got)
	}

	clock.set(testNow.Add(spinnerDelay))
	want := "usage " + built.spinnerFrame()
	if got := style.Plain(built.Render(segment.Context{})); got != want {
		t.Errorf("long fetch = %q, want %q", got, want)
	}

	spinnerRate := 125 * time.Millisecond
	if got := built.NextRefresh(phaseAt(clock.read())); !got.Equal(clock.read().Add(spinnerRate)) {
		t.Errorf("active refresh = %s", got.Sub(clock.read()))
	}
}

func TestAnEmptyReportShowsPendingStatus(t *testing.T) {
	clock := &testClock{now: testNow}
	built := buildState(t, &scriptedReporter{}, clock)

	fetchNow(t, built)

	if got := built.NextRefresh(phaseAt(testNow)); !got.Equal(testNow) {
		t.Errorf("expected a landed fetch to be drawn at once, got it in %s", got.Sub(testNow))
	}

	if got := style.Plain(built.Render(segment.Context{})); got != "usage pending" {
		t.Errorf("got %q", got)
	}

	if got := built.NextRefresh(phaseAt(testNow)); !got.Equal(testNow.Add(redrawInterval)) {
		t.Errorf("pending refresh = %s", got.Sub(testNow))
	}
}

func TestAFastPendingRetryNeverShowsTheSpinner(t *testing.T) {
	clock := &testClock{now: testNow}
	built := buildState(t, &scriptedReporter{}, clock)

	fetchNow(t, built)
	clock.set(testNow.Add(firstEmptyWait))
	if !built.noteFetchStarting() {
		t.Fatal("retry did not start")
	}

	if got := style.Plain(built.Render(segment.Context{})); got != "usage pending" {
		t.Errorf("retry start = %q", got)
	}

	built.fetch()
	if got := style.Plain(built.Render(segment.Context{})); got != "usage pending" {
		t.Errorf("retry completion = %q", got)
	}
}

func TestEmptyReportsBackOffToTheConfiguredRate(t *testing.T) {
	reporter := &scriptedReporter{}
	clock := &testClock{now: testNow}
	built := buildState(t, reporter, clock)

	fetchNow(t, built)

	waits := []time.Duration{
		15 * time.Second,
		30 * time.Second,
		time.Minute,
		2 * time.Minute,
		4 * time.Minute,
		5 * time.Minute,
		5 * time.Minute,
	}

	for _, wait := range waits {
		retryAt := clock.read().Add(wait)
		assertFetchStarts(t, built, clock, retryAt.Add(-time.Nanosecond), false)
		assertFetchStarts(t, built, clock, retryAt, true)
		built.fetch()
	}
}

func TestFailuresBackOffToTheConfiguredRate(t *testing.T) {
	reporter := &scriptedReporter{err: errors.New("the endpoint is sulking")}
	clock := &testClock{now: testNow}
	built := buildState(t, reporter, clock)

	fetchNow(t, built)

	waits := []time.Duration{
		time.Minute,
		2 * time.Minute,
		4 * time.Minute,
		5 * time.Minute,
		5 * time.Minute,
	}

	for _, wait := range waits {
		retryAt := clock.read().Add(wait)
		assertFetchStarts(t, built, clock, retryAt.Add(-time.Nanosecond), false)
		assertFetchStarts(t, built, clock, retryAt, true)
		built.fetch()
	}
}

func TestASuccessResetsTheEmptyReportBackoff(t *testing.T) {
	reporter := &scriptedReporter{}
	clock := &testClock{now: testNow}
	built := buildState(t, reporter, clock)

	fetchNow(t, built)

	firstRetryAt := testNow.Add(firstEmptyWait)
	reporter.windows = []agent.UsageWindow{{
		Duration: 5 * time.Hour,
		Percent:  40,
		ResetsAt: testNow.Add(150 * time.Minute),
	}}
	assertFetchStarts(t, built, clock, firstRetryAt, true)
	built.fetch()

	reporter.windows = nil
	afterSnapshot := firstRetryAt.Add(defaultRate)
	assertFetchStarts(t, built, clock, afterSnapshot, true)
	built.fetch()

	retryAt := afterSnapshot.Add(firstEmptyWait)
	assertFetchStarts(t, built, clock, retryAt.Add(-time.Nanosecond), false)
	assertFetchStarts(t, built, clock, retryAt, true)
}

func buildState(t *testing.T, reporter agent.UsageReporter, clock *testClock) *state {
	t.Helper()

	built := buildOnClock(t, reporter, clock)
	internal, ok := built.(*state)
	if !ok {
		t.Fatalf("built %T, want *state", built)
	}

	return internal
}

func fetchNow(t *testing.T, built *state) {
	t.Helper()

	if !built.noteFetchStarting() {
		t.Fatal("initial fetch did not start")
	}

	built.fetch()
}

func assertFetchStarts(t *testing.T, built *state, clock *testClock, now time.Time, shouldStart bool) {
	t.Helper()

	clock.set(now)

	if got := built.noteFetchStarting(); got != shouldStart {
		t.Errorf("at %s fetch start = %t, want %t", now.Sub(testNow), got, shouldStart)
	}
}

func TestAFailedFetchShowsThatItFailed(t *testing.T) {
	clock := &testClock{now: testNow}
	built := buildState(t, &scriptedReporter{err: errors.New("the endpoint is sulking")}, clock)

	fetchNow(t, built)

	if got := style.Plain(built.Render(segment.Context{})); got != "usage failed" {
		t.Errorf("got %q", got)
	}
}

func TestAFailedRefreshKeepsTheLastSnapshot(t *testing.T) {
	reporter := &scriptedReporter{windows: []agent.UsageWindow{{
		Duration: 5 * time.Hour,
		Percent:  40,
		ResetsAt: testNow.Add(150 * time.Minute),
	}}}
	clock := &testClock{now: testNow}
	built := buildState(t, reporter, clock)

	fetchNow(t, built)
	reporter.err = errors.New("the endpoint is sulking")
	clock.set(testNow.Add(defaultRate))
	fetchNow(t, built)

	if got := style.Plain(built.Render(segment.Context{})); got != "● 5h 40% ███░┃░░░ failed" {
		t.Errorf("got %q", got)
	}
}

func TestARefusedFetchShowsTheStatusCode(t *testing.T) {
	clock := &testClock{now: testNow}
	refusal := &req.StatusError{Status: 401, Message: "the key is not yours"}
	built := buildState(t, &scriptedReporter{err: refusal}, clock)

	fetchNow(t, built)

	if got := style.Plain(built.Render(segment.Context{})); got != "usage 401" {
		t.Errorf("got %q", got)
	}
}

func TestASpentWindowIsMarked(t *testing.T) {
	built := build(t, &scriptedReporter{windows: []agent.UsageWindow{{
		Duration:  5 * time.Hour,
		Percent:   100,
		ResetsAt:  testNow.Add(time.Hour),
		IsLimited: true,
	}}})

	if got := renderSettled(t, built); got != "● 5h 100% ████████ ✖" {
		t.Errorf("got %q", got)
	}
}

func TestAWindowIsColouredByHowItsBurnComparesWithItsPace(t *testing.T) {
	for _, test := range []struct {
		name      string
		percent   float64
		elapsed   time.Duration
		duration  time.Duration
		wantStyle style.Style
	}{
		{name: "barely started", percent: 9, elapsed: 0, duration: 5 * time.Hour, wantStyle: usage.PaceStyle(usage.PaceEven)},
		{
			name:      "spent in step with the window",
			percent:   50,
			elapsed:   150 * time.Minute,
			duration:  5 * time.Hour,
			wantStyle: usage.PaceStyle(usage.PaceEven),
		},
		{
			name:      "a shade ahead",
			percent:   28,
			elapsed:   time.Hour,
			duration:  5 * time.Hour,
			wantStyle: usage.PaceStyle(usage.PaceAhead),
		},
		{
			name:      "half as much again as the pace",
			percent:   30,
			elapsed:   time.Hour,
			duration:  5 * time.Hour,
			wantStyle: usage.PaceStyle(usage.PaceCritical),
		},
		{
			name:      "near the limit however it got there",
			percent:   90,
			elapsed:   4 * time.Hour,
			duration:  5 * time.Hour,
			wantStyle: usage.PaceStyle(usage.PaceCritical),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			clock := &testClock{now: testNow}
			built := buildOnClock(t, &scriptedReporter{windows: []agent.UsageWindow{{
				Duration: test.duration,
				Percent:  test.percent,
				ResetsAt: testNow.Add(test.duration - test.elapsed),
			}}}, clock)

			settle(t, built)

			drawn := built.Render(segment.Context{})
			want := test.wantStyle(fmt.Sprintf("%d%%", int(test.percent+0.5)))

			if !strings.Contains(drawn, want) {
				t.Errorf("drew %q, want it to hold %q", drawn, want)
			}
		})
	}
}

func TestAFigureIsRoundedToTheNearestWhole(t *testing.T) {
	built := build(t, &scriptedReporter{windows: []agent.UsageWindow{{
		Duration: 5 * time.Hour,
		Percent:  39.6,
		ResetsAt: testNow.Add(time.Hour),
	}}})

	if got := renderSettled(t, built); got != "● 5h 40% ███░░░┃░" {
		t.Errorf("got %q", got)
	}
}

func TestAScopeIsMatchedAgainstTheModelHoweverItIsWritten(t *testing.T) {
	for _, test := range []struct {
		name      string
		modelName string
		scope     string
		want      bool
	}{
		{name: "the model the bucket names", modelName: "gpt-5.3-codex-spark", scope: "gpt-5.3-codex-spark", want: true},
		{name: "a family within the model name", modelName: "claude-opus-4-6", scope: "opus", want: true},
		{name: "a model of another family", modelName: "claude-sonnet-4-6", scope: "opus"},
		{name: "a model written in capitals", modelName: "GPT-5.3-Codex-Spark", scope: "gpt-5.3-codex-spark", want: true},
		{name: "no model at all", modelName: "", scope: "opus"},
		{name: "a window governing everything", modelName: "anything", scope: "", want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			built := buildOnModel(t, &scriptedReporter{}, test.modelName, &testClock{now: testNow})

			internal, ok := built.(*state)
			if !ok {
				t.Fatalf("built %T, want *state", built)
			}

			if got := internal.governsThisSession(agent.UsageWindow{Scope: test.scope}); got != test.want {
				t.Errorf("got %t, want %t", got, test.want)
			}
		})
	}
}

func TestTheSegmentPollsOnWhetherATurnIsRunningOrNot(t *testing.T) {
	built := build(t, &scriptedReporter{})

	refresher, ok := built.(segment.Refresher)
	if !ok {
		t.Fatal("expected the segment to say when it next changes")
	}

	for _, isRunning := range []bool{false, true} {
		phase := segment.Phase{At: testNow, IsRunning: isRunning}

		if got := refresher.NextRefresh(phase); !got.Equal(testNow.Add(redrawInterval)) {
			t.Errorf("refresh while running=%t = %s", isRunning, got.Sub(testNow))
		}
	}
}

func TestAnOptionTheLayoutGotWrongIsRefused(t *testing.T) {
	if _, err := New(Settings{Now: testNowRead})(rateOptions{rate: -time.Second}); err == nil {
		t.Error("expected a rate shorter than nothing to be refused")
	}
}

type rateOptions struct {
	rate time.Duration
}

func (self rateOptions) Read(into any) error {
	args, ok := into.(*struct {
		Rate time.Duration `toml:"rate"`
	})
	if !ok {
		return nil
	}

	args.Rate = self.rate

	return nil
}

func testNowRead() time.Time {
	return testNow
}

func TestAWindowNotYetStartedIsMeasuredFromNothingSpent(t *testing.T) {
	built := build(t, &scriptedReporter{windows: []agent.UsageWindow{{
		Duration: 5 * time.Hour,
		Percent:  20,
		ResetsAt: testNow.Add(10 * time.Hour),
	}}})

	if got := renderSettled(t, built); got != "● 5h 20% ┃░░░░░░░" {
		t.Errorf("got %q", got)
	}
}

func TestAnUnreadableOptionIsHandedBack(t *testing.T) {
	if _, err := New(Settings{Now: testNowRead})(refusedOptions{}); err == nil {
		t.Error("expected the unreadable option handed back")
	}
}

type refusedOptions struct{}

func (refusedOptions) Read(any) error {
	return errors.New("the layout wrote something else")
}

func TestASimulationHasNoLimitToReport(t *testing.T) {
	built, err := New(Settings{IsSimulated: true, Reporter: &scriptedReporter{}})(noOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := style.Plain(built.Render(segment.Context{})), "∞ ░░░░░░░░"; got != want {
		t.Errorf("a simulation drew %q, want %q", got, want)
	}
}

func TestASimulationAsksNobodyAboutUsage(t *testing.T) {
	reporter := &scriptedReporter{}

	built, err := New(Settings{IsSimulated: true, Reporter: reporter})(noOptions{})
	if err != nil {
		t.Fatal(err)
	}

	built.Render(segment.Context{})
	built.Render(segment.Context{})

	if reporter.asked.Load() != 0 {
		t.Errorf("a simulation asked for usage %d times", reporter.asked.Load())
	}
}
