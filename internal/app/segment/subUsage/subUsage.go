package subUsage

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"crdx.org/io/internal/app/segment"
	"crdx.org/io/internal/app/spinner"
	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/app/usage"
	"crdx.org/io/internal/util"
	"crdx.org/io/pkg/agent"
)

var _ segment.Refresher = &state{}

const (
	defaultRate      = 5 * time.Minute
	redrawInterval   = 15 * time.Second
	spinnerDelay     = 250 * time.Millisecond
	firstFailureWait = time.Minute
	firstEmptyWait   = redrawInterval
	backoffFactor    = 2
	barCells         = 8
	scopeMark        = "⚡"
	limitedMark      = "✖"
	staleLabel       = "stale"
	failureLabel     = "failed"
	boundlessMark    = "∞"
)

type Settings struct {
	Reporter         agent.UsageReporter
	CachePath        string
	ModelName        string
	IsSelfRefreshing bool
	IsSimulated      bool
	Gauges           *usage.Gauges
	Now              func() time.Time
}

type boundless struct {
	gauges *usage.Gauges
}

func (self boundless) Render(segment.Context) string {
	return style.Simulation(boundlessMark) + " " + self.gauges.Draw(0, nil, usage.PaceEven, barCells)
}

type snapshot struct {
	windows   []agent.UsageWindow
	fetchedAt time.Time
	status    usageStatus
	failure   string
}

type usageStatus int

const (
	usageReady usageStatus = iota
	usageFetching
	usagePending
	usageRetrying
)

type state struct {
	windowTracker    *usage.WindowTracker
	modelName        string
	rate             time.Duration
	isSelfRefreshing bool
	gauges           *usage.Gauges
	now              func() time.Time

	mutex             sync.Mutex
	windows           []agent.UsageWindow
	fetchedAt         time.Time
	retryAt           time.Time
	fetchStartedAt    time.Time
	waitedTime        time.Duration
	failure           string
	status            usageStatus
	statusBeforeFetch usageStatus
	shouldRedraw      bool
}

func New(settings Settings) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		if settings.IsSimulated {
			return boundless{gauges: settings.Gauges}, nil
		}

		var args struct {
			Rate time.Duration `toml:"rate"`
		}

		if err := options.Read(&args); err != nil {
			return nil, err
		}

		if args.Rate < 0 {
			return nil, fmt.Errorf("rate must not be negative (got %s)", args.Rate)
		}

		if args.Rate == 0 {
			args.Rate = defaultRate
		}

		self := &state{
			windowTracker:    usage.NewWindowTracker(settings.Reporter, settings.CachePath, args.Rate, settings.Now),
			modelName:        strings.ToLower(settings.ModelName),
			rate:             args.Rate,
			isSelfRefreshing: settings.IsSelfRefreshing,
			gauges:           settings.Gauges,
			now:              settings.Now,
			status:           usagePending,
		}

		self.refreshFromSnapshot()

		return self, nil
	}
}

func (self *state) NextRefresh(phase segment.Phase) time.Time {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.shouldRedraw {
		return phase.At
	}

	if self.status == usageFetching && len(self.windows) == 0 {
		interval := spinner.Activity.RefreshInterval()

		return phase.At.Truncate(interval).Add(interval)
	}

	return phase.At.Truncate(redrawInterval).Add(redrawInterval)
}

func (self *state) Render(segment.Context) string {
	if !self.windowTracker.IsAvailable() {
		return style.Dim("usage n/a")
	}

	self.refreshFromSnapshot()

	self.mutex.Lock()
	shouldFetch := self.noteFetchStarting()
	current := snapshot{
		windows:   self.windows,
		fetchedAt: self.fetchedAt,
		status:    self.getVisibleStatus(),
		failure:   self.failure,
	}
	self.shouldRedraw = false
	self.mutex.Unlock()

	if shouldFetch {
		go self.fetch()
	}

	return self.draw(current)
}

func (self *state) refreshFromSnapshot() {
	snapshot := self.windowTracker.ReadSnapshot()

	self.mutex.Lock()
	defer self.mutex.Unlock()

	if len(snapshot.Windows) == 0 || !snapshot.FetchedAt.After(self.fetchedAt) {
		return
	}

	self.keepReady(snapshot.Windows, snapshot.FetchedAt)
}

func (self *state) getVisibleStatus() usageStatus {
	if self.status == usageFetching &&
		(len(self.windows) > 0 || self.now().Sub(self.fetchStartedAt) < spinnerDelay) {
		return self.statusBeforeFetch
	}

	return self.status
}

func (self *state) noteFetchStarting() bool {
	now := self.now()

	if self.status == usageFetching || now.Before(self.retryAt) || now.Sub(self.fetchedAt) < self.rate {
		return false
	}

	self.fetchStartedAt = now
	self.statusBeforeFetch = self.status
	self.status = usageFetching

	return true
}

func (self *state) fetch() {
	snapshot, err := self.windowTracker.Fetch(context.Background())

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.shouldRedraw = true

	if err != nil {
		self.status = usageRetrying
		self.failure = failureReason(err)
		self.waitLonger(firstFailureWait)

		return
	}

	if len(snapshot.Windows) == 0 {
		self.status = usagePending
		self.waitLonger(firstEmptyWait)

		return
	}

	self.keepReady(snapshot.Windows, snapshot.FetchedAt)
}

func (self *state) keepReady(windows []agent.UsageWindow, fetchedAt time.Time) {
	self.status = usageReady
	self.failure = ""
	self.retryAt = time.Time{}
	self.waitedTime = 0

	if fetchedAt.Before(self.fetchedAt) {
		return
	}

	self.windows = windows
	self.fetchedAt = fetchedAt
}

func (self *state) waitLonger(first time.Duration) {
	self.waitedTime = min(max(self.waitedTime*backoffFactor, first), self.rate)
	self.retryAt = self.now().Add(self.waitedTime)
}

func (self *state) draw(current snapshot) string {
	var parts, scopedParts []string

	for _, window := range current.windows {
		if !self.governsThisSession(window) {
			continue
		}

		text := self.drawWindow(window, current.fetchedAt, self.now())

		if window.Scope == "" {
			parts = append(parts, text)
		} else {
			scopedParts = append(scopedParts, text)
		}
	}

	if len(scopedParts) > 0 {
		parts = append(parts, style.Subtle(scopeMark))
		parts = append(parts, scopedParts...)
	}

	windows := strings.Join(parts, " ")

	if windows != "" && !current.fetchedAt.IsZero() {
		windows = self.drawFreshness(current.fetchedAt) + " " + windows
	}

	switch current.status {
	case usageFetching:
		return appendUsageStatus(windows, "usage", style.Spinner(self.spinnerFrame()))
	case usagePending:
		return appendUsageStatus(windows, "usage", style.Subtle("pending"))
	case usageRetrying:
		return appendUsageStatus(windows, "usage", style.Failure(current.failure))
	case usageReady:
	}

	if windows == "" {
		return style.Dim("usage unavailable")
	}

	return windows
}

func (self *state) drawFreshness(fetchedAt time.Time) string {
	age := max(0, self.now().Sub(fetchedAt))

	mark, appearance, isAgeWorthSaying := usage.FreshnessMark(usage.FreshnessWithin(
		age, usage.FreshWithin(self.rate), usage.StaleAfter(self.rate), self.isSelfRefreshing,
	))

	if !isAgeWorthSaying {
		return appearance(mark)
	}

	return style.Normal(util.CoarseDuration(age)) + " " + appearance(mark)
}

func (self *state) governsThisSession(window agent.UsageWindow) bool {
	return window.Scope == "" || strings.Contains(self.modelName, window.Scope)
}

func (self *state) spinnerFrame() string {
	interval := spinner.Activity.RefreshInterval()
	frameIndex := int(self.now().UnixNano() / interval.Nanoseconds())

	return spinner.Activity.Frame(frameIndex)
}

func appendUsageStatus(usage string, emptyLabel string, status string) string {
	if usage == "" {
		return emptyLabel + " " + status
	}

	return usage + " " + status
}

func (self *state) drawWindow(
	window agent.UsageWindow, fetchedAt time.Time, now time.Time,
) string {
	label := usage.ShortWindowLabel(window.Duration)
	usedPercent := int(window.Percent + 0.5)

	if !window.ResetsAt.IsZero() && !window.ResetsAt.After(now) {
		return style.Dim(label + " " + staleLabel)
	}

	if window.IsLimited {
		return style.Failure(fmt.Sprintf("%s %d%%", label, usedPercent)) +
			" " +
			self.gauges.Draw(usedPercent, nil, usage.PaceCritical, barCells) +
			" " +
			style.Failure(limitedMark)
	}

	var expectedPercent *int

	pace := usage.PaceEven

	if !window.ResetsAt.IsZero() {
		pacePercent := usage.ExpectedPercent(window, fetchedAt)
		expectedPercent = &pacePercent
		pace = usage.ClassifyPace(usedPercent, pacePercent)
	}

	return style.Dim(label) +
		" " +
		usage.PaceStyle(pace)(fmt.Sprintf("%d%%", usedPercent)) +
		" " +
		self.gauges.Draw(usedPercent, expectedPercent, pace, barCells)
}

func failureReason(err error) string {
	if status, isRefusal := usage.FailureStatus(err); isRefusal {
		return strconv.Itoa(status)
	}

	return failureLabel
}
