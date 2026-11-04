package bar

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/cycle"
	"crdx.org/oh/internal/app/input"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/segment/activeModel"
	"crdx.org/oh/internal/app/segment/activitySpinner"
	"crdx.org/oh/internal/app/segment/approvals"
	"crdx.org/oh/internal/app/segment/cacheUsage"
	"crdx.org/oh/internal/app/segment/contextUsage"
	"crdx.org/oh/internal/app/segment/fastMode"
	"crdx.org/oh/internal/app/segment/forwardedPorts"
	"crdx.org/oh/internal/app/segment/gitBranch"
	"crdx.org/oh/internal/app/segment/gitStatus"
	"crdx.org/oh/internal/app/segment/localTime"
	"crdx.org/oh/internal/app/segment/modeToggle"
	"crdx.org/oh/internal/app/segment/pathGrants"
	"crdx.org/oh/internal/app/segment/scrollOverflow"
	"crdx.org/oh/internal/app/segment/sessionEmoji"
	"crdx.org/oh/internal/app/segment/sessionName"
	"crdx.org/oh/internal/app/segment/sessionSpend"
	"crdx.org/oh/internal/app/segment/stashCount"
	"crdx.org/oh/internal/app/segment/subUsage"
	"crdx.org/oh/internal/app/segment/subagentCounts"
	"crdx.org/oh/internal/app/segment/taskNames"
	"crdx.org/oh/internal/app/segment/turnCount"
	"crdx.org/oh/internal/app/segment/turnTimer"
	"crdx.org/oh/internal/app/segment/workspaceDir"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/subagents"
	"crdx.org/oh/internal/app/turn"
	"crdx.org/oh/internal/app/usage"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/money"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/question"
	"crdx.org/oh/pkg/tasks"
)

const (
	activitySpinnerSegment = "activity-spinner"
	cacheUsageSegment      = "cache-usage"
	contextUsageSegment    = "context-usage"
	modeToggleSegment      = "mode-toggle"
	grantsSegment          = "grants"
	forwardsSegment        = "forwards"
	approvalsSegment       = "approvals"
	workspaceDirSegment    = "workspace-dir"
	activeModelSegment     = "active-model"
	fastModeSegment        = "fast-mode"
	scrollOverflowSegment  = "scroll-overflow"
	sessionNameSegment     = "session-name"
	sessionEmojiSegment    = "session-emoji"
	localTimeSegment       = "local-time"
	turnTimerSegment       = "turn-timer"
	turnCountSegment       = "turn-count"
	gitBranchSegment       = "git-branch"
	gitStatusSegment       = "git-status"
	sessionSpendSegment    = "session-spend"
	subUsageSegment        = "subscription-usage"
	taskNamesSegment       = "tasks"
	subagentCountsSegment  = "subagents"
	stashCountSegment      = "stash-count"
)

type Options struct {
	Workspace             *work.Space
	Session               cycle.Session
	ModelEffortLevels     []string
	IsFast                bool
	IsSimulated           bool
	IsUnconfined          bool
	UsageReporter         agent.UsageReporter
	UsageCachePath        string
	UsagePage             string
	UsageIsSelfRefreshing bool
	UsageGauges           *usage.Gauges
	GetCurrency           func() money.Currency
	SandboxHostname       string
	Sources               Sources
}

type Sources struct {
	IsTurnRunning      func() bool
	IsSessionPersisted func() bool
	GetContextUsage    func() (int, int)
	GetCacheUsage      func() (int, int)
	GetSessionSpend    func() (float64, bool)
	GetGrantedCaps     func() caps.Set
	GetGroupStatus     func() caps.GroupStatus
	GetPathGrants      func() []pathgrant.Grant
	GetForwardedRoutes func() []portgrant.Route
	GetApprovals       func() []question.RememberedAnswer
	IsPrefixPending    func() bool
	GetTurnTiming      func() turn.Timing
	GetTurnCount       func() int
	GetTasks           func() []tasks.Snapshot
	GetSubagents       func() []subagents.Snapshot
	GetStashCount      func() int
}

func NewRegistry(options Options) segment.Registry {
	return segment.Registry{
		activitySpinnerSegment: activitySpinner.New(options.Sources.IsTurnRunning, time.Now),
		cacheUsageSegment:      cacheUsage.New(options.Sources.GetCacheUsage),
		contextUsageSegment:    contextUsage.New(options.Sources.GetContextUsage),
		sessionSpendSegment:    sessionSpend.New(options.Sources.GetSessionSpend, options.GetCurrency),
		modeToggleSegment:      modeToggleFor(options),
		grantsSegment:          pathGrants.New(options.Sources.GetPathGrants),
		forwardsSegment: forwardedPorts.New(forwardedPorts.Routes{
			GetRoutes: options.Sources.GetForwardedRoutes,
			GetTasks:  options.Sources.GetTasks,
			Hostname:  options.SandboxHostname,
		}),
		approvalsSegment:    approvals.New(options.Sources.GetApprovals, time.Now),
		workspaceDirSegment: workspaceDir.New(options.Workspace),
		activeModelSegment: activeModel.New(activeModel.Settings{
			Name:         options.Session.Model,
			Effort:       options.Session.Effort,
			EffortLevels: options.ModelEffortLevels,
			IsFast:       options.IsFast,
			IsSimulated:  options.IsSimulated,
		}),
		fastModeSegment:       fastMode.New(options.IsFast),
		scrollOverflowSegment: scrollOverflow.New,
		sessionNameSegment: sessionName.New(
			options.Session.Name,
			options.Session.Directory,
			options.Sources.IsSessionPersisted,
		),
		sessionEmojiSegment:   sessionEmoji.New(options.Session.Name),
		localTimeSegment:      localTime.New(time.Now),
		turnTimerSegment:      turnTimer.New(options.Sources.GetTurnTiming, options.Sources.IsTurnRunning),
		turnCountSegment:      turnCount.New(options.Sources.GetTurnCount),
		gitBranchSegment:      gitBranch.New(options.Workspace.GetDir()),
		gitStatusSegment:      gitStatus.New(options.Workspace.GetDir(), sandbox.ArgvFor(options.IsUnconfined)),
		taskNamesSegment:      taskNames.New(options.Session.Name, options.Sources.GetTasks, options.Sources.GetForwardedRoutes, time.Now),
		subagentCountsSegment: subagentCounts.New(options.Session.Name, options.Sources.GetSubagents, time.Now),
		stashCountSegment:     stashCount.New(options.Sources.GetStashCount),
		subUsageSegment: subUsage.New(subUsage.Settings{
			Reporter:         options.UsageReporter,
			CachePath:        options.UsageCachePath,
			Page:             options.UsagePage,
			ModelName:        options.Session.Model,
			IsSelfRefreshing: options.UsageIsSelfRefreshing,
			IsSimulated:      options.IsSimulated,
			Gauges:           options.UsageGauges,
			Now:              time.Now,
		}),
	}
}

func segmentSeparator() string {
	return " " + style.Subtle("─") + " "
}

func Render(layout segment.Layout, position segment.Position, context segment.Context) string {
	return render(layout, position, context, -1)
}

func RenderWithin(layout segment.Layout, position segment.Position, context segment.Context, cells int) string {
	return render(layout, position, context, max(cells, 0))
}

type fittingPosition struct {
	ladders    [][]string
	rungs      []int
	priorities []segment.Priority
}

func newFittingPosition(instances []segment.Segment, context segment.Context) fittingPosition {
	position := fittingPosition{
		ladders:    make([][]string, len(instances)),
		rungs:      make([]int, len(instances)),
		priorities: make([]segment.Priority, len(instances)),
	}
	for at, instance := range instances {
		position.ladders[at] = segment.LadderOf(underlying(instance), context)
		if namedInstance, ok := instance.(segment.Instance); ok {
			position.priorities[at] = namedInstance.Priority
		}
	}
	return position
}

func render(layout segment.Layout, position segment.Position, context segment.Context, cells int) string {
	fittingPosition := newFittingPosition(layout[position], context)
	for _, priority := range []segment.Priority{segment.Normal, segment.Important} {
		for cells >= 0 && style.Width(fittingPosition.text()) > cells && fittingPosition.stepDown(priority) {
		}
	}
	return fittingPosition.text()
}

func (self fittingPosition) stepDown(priority segment.Priority) bool {
	for at := range slices.Backward(self.rungs) {
		if self.priorities[at] == priority && self.rungs[at] < lastDrawnRung(self.ladders[at]) {
			self.rungs[at]++
			return true
		}
	}
	for at, ladder := range self.ladders {
		if self.priorities[at] == priority && ladder[len(ladder)-1] == "" && self.rungs[at] < len(ladder)-1 {
			self.rungs[at] = len(ladder) - 1
			return true
		}
	}
	for at := range slices.Backward(self.rungs) {
		ladder := self.ladders[at]
		if self.priorities[at] == priority && ladder[len(ladder)-1] != "" && self.rungs[at] < len(ladder) {
			self.rungs[at] = len(ladder)
			return true
		}
	}
	return false
}

func lastDrawnRung(ladder []string) int {
	if last := len(ladder) - 1; last > 0 && ladder[last] == "" {
		return last - 1
	}
	return len(ladder) - 1
}

func (self fittingPosition) text() string {
	texts := make([]string, 0, len(self.ladders))
	for at, ladder := range self.ladders {
		if self.rungs[at] >= len(ladder) {
			continue
		}
		if text := ladder[self.rungs[at]]; style.Width(text) > 0 {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, segmentSeparator())
}

func RenderRuler(layout segment.Layout, positions [3]segment.Position, context segment.Context, cells int) input.Ruler {
	left := newFittingPosition(layout[positions[0]], context)
	center := newFittingPosition(layout[positions[1]], context)
	right := newFittingPosition(layout[positions[2]], context)
	ruler := func() input.Ruler {
		return input.Ruler{Left: left.text(), Center: center.text(), Right: right.text()}
	}
	for _, position := range []fittingPosition{left, center, right} {
		for at, ladder := range position.ladders {
			position.rungs[at] = len(ladder)
		}
	}
	for _, priority := range []segment.Priority{segment.Important, segment.Normal} {
		for _, position := range []fittingPosition{right, left, center} {
			for at, importance := range position.priorities {
				if importance == priority {
					position.rungs[at] = 0
				}
			}
			for !ruler().Fits(cells) && position.stepDown(priority) {
			}
		}
	}
	return ruler()
}

func underlying(instance segment.Segment) segment.Segment {
	if namedInstance, isNamed := instance.(segment.Instance); isNamed {
		return namedInstance.Segment
	}
	return instance
}

type Config struct {
	registry segment.Registry
	layout   segment.Layout
}

func NewConfiguration(registry segment.Registry, layout segment.Layout) Config {
	return Config{registry: registry, layout: layout}
}

func (self *Config) GetRegistry() segment.Registry {
	return self.registry
}

func (self *Config) ReplaceLayout(layout segment.Layout) {
	self.layout = layout
}

func (self *Config) RenderInfo(context segment.Context) (string, error) {
	type infoRow struct {
		name  string
		value string
	}

	var rows []infoRow
	var emptyNames []string
	nameCells := 0
	for _, name := range self.registry.Available() {
		if !isInfoSegment(name) {
			continue
		}
		instance, err := self.getInfoSegment(name)
		if err != nil {
			return "", err
		}
		value := instance.Render(context)
		if value == "" {
			emptyNames = append(emptyNames, name)
			continue
		}
		rows = append(rows, infoRow{name: name, value: value})
		nameCells = max(nameCells, style.Width(name))
	}
	if len(emptyNames) > 0 {
		const emptyName = "(empty)"
		rows = append(rows, infoRow{name: emptyName, value: style.Subtle(strings.Join(emptyNames, ", "))})
		nameCells = max(nameCells, style.Width(emptyName))
	}

	drawnRows := make([]string, 0, len(rows))
	for _, row := range rows {
		padding := strings.Repeat(" ", nameCells-style.Width(row.name)+2)
		drawnRows = append(drawnRows, style.Info(row.name)+padding+row.value)
	}
	return strings.Join(drawnRows, "\n"), nil
}

func (self *Config) Render(position segment.Position, context segment.Context) string {
	return Render(self.layout, position, context)
}

func (self *Config) RenderRuler(positions [3]segment.Position, context segment.Context, cells int, overrides segment.Layout) input.Ruler {
	layout := make(segment.Layout, len(positions))
	for _, position := range positions {
		instances, hasOverride := overrides[position]
		if !hasOverride {
			instances = self.layout[position]
		}
		layout[position] = instances
	}
	return RenderRuler(layout, positions, context, cells)
}

func (self *Config) NextRefresh(phase segment.Phase) time.Time {
	return self.layout.NextRefresh(phase)
}

func (self *Config) getInfoSegment(name string) (segment.Segment, error) {
	for _, position := range segment.Positions {
		for _, instance := range self.layout[position] {
			namedInstance, isNamed := instance.(segment.Instance)
			if isNamed && namedInstance.Name == name {
				return namedInstance.Segment, nil
			}
		}
	}

	instance, err := self.registry[name](infoOptions{})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return instance, nil
}

func isInfoSegment(name string) bool {
	return name != activitySpinnerSegment && name != scrollOverflowSegment
}

type infoOptions struct{}

func (infoOptions) Read(any) error {
	return nil
}

func modeToggleFor(options Options) segment.Factory {
	if options.IsUnconfined {
		return modeToggle.NewUnconfined(
			options.Sources.GetGrantedCaps,
			options.Sources.IsPrefixPending,
			options.Sources.GetGroupStatus,
		)
	}

	return modeToggle.New(
		options.Sources.GetGrantedCaps,
		options.Sources.IsPrefixPending,
		options.Sources.GetGroupStatus,
	)
}
