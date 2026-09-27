package subUsage

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/segment/fit"
	"crdx.org/oh/internal/app/spinner"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/usage"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/pkg/agent"
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
	leadingParts     = 2
)

const (
	limitedWorth = iota
	ordinaryWorth
	staleWorth
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

func (self boundless) Render(context segment.Context) string {
	return self.Ladder(context)[0]
}

func (self boundless) Ladder(segment.Context) []string {
	return fit.Ladder(
		style.Simulation(boundlessMark)+" "+self.gauges.Draw(0, nil, usage.PaceEven, barCells),
		style.Simulation(boundlessMark),
		"",
	)
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

func (self *state) Render(context segment.Context) string {
	return self.Ladder(context)[0]
}

func (self *state) Ladder(segment.Context) []string {
	if !self.windowTracker.IsAvailable() {
		return []string{style.Dim("usage n/a")}
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

func (self *state) draw(current snapshot) []string {
	rows := self.windowRows(current)
	age, mark := self.drawFreshness(current.fetchedAt)
	status := self.drawStatus(current)

	if len(rows) == 0 {
		if status == "" {
			return []string{style.Dim("usage unavailable")}
		}

		return []string{appendUsageStatus("", "usage", status)}
	}

	rungs := make([]string, 0, len(rows)+6)

	for _, step := range steps(len(rows)) {
		rungs = append(rungs, assemble(rows, step, age, mark, status))
	}

	return fit.Ladder(append(rungs, "")...)
}

type shape struct {
	shownRows         int
	leadingLosses     int
	hasAge            bool
	hasTrailingGauges bool
	hasTrailingLabels bool
}

func steps(rowCount int) []shape {
	whole := shape{
		shownRows:         rowCount,
		hasAge:            true,
		hasTrailingGauges: true,
		hasTrailingLabels: true,
	}

	withoutAge := whole
	withoutAge.hasAge = false

	withoutTrailingGauges := withoutAge
	withoutTrailingGauges.hasTrailingGauges = false

	withoutTrailingLabels := withoutTrailingGauges
	withoutTrailingLabels.hasTrailingLabels = false

	steps := []shape{whole, withoutAge, withoutTrailingGauges, withoutTrailingLabels}

	for shownRows := rowCount - 1; shownRows >= 1; shownRows-- {
		fewer := withoutTrailingLabels
		fewer.shownRows = shownRows
		steps = append(steps, fewer)
	}

	alone := withoutTrailingLabels
	alone.shownRows = 1

	for losses := 1; losses <= leadingParts; losses++ {
		lesser := alone
		lesser.leadingLosses = losses
		steps = append(steps, lesser)
	}

	return steps
}

func assemble(rows []windowRow, step shape, age string, mark string, status string) string {
	keptWindows := keptRows(rows, step.shownRows)
	drawnRows := make([]string, 0, len(keptWindows)+1)
	isScopeMarked := false

	for at, row := range keptWindows {
		drawnRow := row.draw(at == 0, step)
		if drawnRow == "" {
			continue
		}

		if row.isScoped && !isScopeMarked {
			drawnRows = append(drawnRows, style.Subtle(scopeMark))
			isScopeMarked = true
		}

		drawnRows = append(drawnRows, drawnRow)
	}

	windows := strings.Join(drawnRows, " ")

	if windows != "" && mark != "" {
		if step.hasAge && age != "" {
			windows = age + " " + mark + " " + windows
		} else {
			windows = mark + " " + windows
		}
	}

	if status == "" {
		return windows
	}

	return appendUsageStatus(windows, "usage", status)
}

func keptRows(rows []windowRow, rowCount int) []windowRow {
	if rowCount >= len(rows) {
		return rows
	}

	order := make([]int, len(rows))
	for at := range order {
		order[at] = at
	}

	slices.SortStableFunc(order, func(left int, right int) int {
		if worth := rows[left].worth() - rows[right].worth(); worth != 0 {
			return worth
		}

		return left - right
	})

	keptOrder := slices.Clone(order[:rowCount])
	slices.Sort(keptOrder)

	keptWindows := make([]windowRow, 0, rowCount)
	for _, at := range keptOrder {
		keptWindows = append(keptWindows, rows[at])
	}

	return keptWindows
}

type windowRow struct {
	label     string
	percent   string
	gauge     string
	mark      string
	isScoped  bool
	isLimited bool
	isStale   bool
}

func (self windowRow) worth() int {
	switch {
	case self.isLimited:
		return limitedWorth
	case self.isStale:
		return staleWorth
	default:
		return ordinaryWorth
	}
}

type shownParts struct {
	hasLabel   bool
	hasPercent bool
	hasGauge   bool
}

func (self windowRow) draw(isLeading bool, step shape) string {
	visible := shownParts{
		hasLabel:   step.hasTrailingLabels,
		hasPercent: true,
		hasGauge:   step.hasTrailingGauges,
	}

	if isLeading {
		visible = self.leading(step.leadingLosses)
	}

	parts := make([]string, 0, 4)

	for _, part := range []struct {
		text    string
		isShown bool
	}{
		{self.label, visible.hasLabel},
		{self.percent, visible.hasPercent},
		{self.gauge, visible.hasGauge},
		{self.mark, true},
	} {
		if part.isShown && part.text != "" {
			parts = append(parts, part.text)
		}
	}

	return strings.Join(parts, " ")
}

func (self windowRow) leading(losses int) shownParts {
	if self.gauge == "" {
		return shownParts{hasLabel: losses < 1, hasPercent: losses < 2}
	}

	return shownParts{hasLabel: losses < 2, hasPercent: losses < 1, hasGauge: true}
}

func (self *state) windowRows(current snapshot) []windowRow {
	var rows, scopedRows []windowRow

	for _, window := range current.windows {
		if !self.governsThisSession(window) {
			continue
		}

		row := self.readWindow(window, current.fetchedAt, self.now())

		if row.isScoped {
			scopedRows = append(scopedRows, row)
		} else {
			rows = append(rows, row)
		}
	}

	return append(rows, scopedRows...)
}

func (self *state) drawStatus(current snapshot) string {
	switch current.status {
	case usageFetching:
		return style.Spinner(self.spinnerFrame())
	case usagePending:
		return style.Subtle("pending")
	case usageRetrying:
		return style.Failure(current.failure)
	case usageReady:
	}

	return ""
}

func (self *state) drawFreshness(fetchedAt time.Time) (string, string) {
	if fetchedAt.IsZero() {
		return "", ""
	}

	age := max(0, self.now().Sub(fetchedAt))

	mark, appearance, isAgeWorthSaying := usage.FreshnessMark(usage.FreshnessWithin(
		age, usage.FreshWithin(self.rate), usage.StaleAfter(self.rate), self.isSelfRefreshing,
	))

	if !isAgeWorthSaying {
		return "", appearance(mark)
	}

	return style.Normal(util.CoarseDuration(age)), appearance(mark)
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

func (self *state) readWindow(
	window agent.UsageWindow, fetchedAt time.Time, now time.Time,
) windowRow {
	label := usage.ShortWindowLabel(window.Duration)
	usedPercent := int(window.Percent + 0.5)
	row := windowRow{isScoped: window.Scope != ""}

	if !window.ResetsAt.IsZero() && !window.ResetsAt.After(now) {
		row.isStale = true
		row.label = style.Dim(label)
		row.percent = style.Dim(staleLabel)

		return row
	}

	if window.IsLimited {
		row.isLimited = true
		row.label = style.Failure(label)
		row.percent = style.Failure(fmt.Sprintf("%d%%", usedPercent))
		row.gauge = self.gauges.Draw(usedPercent, nil, usage.PaceCritical, barCells)
		row.mark = style.Failure(limitedMark)

		return row
	}

	var expectedPercent *int

	pace := usage.PaceEven

	if !window.ResetsAt.IsZero() {
		pacePercent := usage.ExpectedPercent(window, fetchedAt)
		expectedPercent = &pacePercent
		pace = usage.ClassifyPace(usedPercent, pacePercent)
	}

	row.label = style.Dim(label)
	row.percent = usage.PaceStyle(pace)(fmt.Sprintf("%d%%", usedPercent))
	row.gauge = self.gauges.Draw(usedPercent, expectedPercent, pace, barCells)

	return row
}

func failureReason(err error) string {
	if status, isRefusal := usage.FailureStatus(err); isRefusal {
		return strconv.Itoa(status)
	}

	return failureLabel
}
