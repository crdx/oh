package bar

import (
	"fmt"
	"strings"
	"time"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/cycle"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/segment/activeModel"
	"crdx.org/oh/internal/app/segment/activitySpinner"
	"crdx.org/oh/internal/app/segment/cacheUsage"
	"crdx.org/oh/internal/app/segment/contextUsage"
	"crdx.org/oh/internal/app/segment/exposedPorts"
	"crdx.org/oh/internal/app/segment/fastMode"
	"crdx.org/oh/internal/app/segment/gitBranch"
	"crdx.org/oh/internal/app/segment/jobNames"
	"crdx.org/oh/internal/app/segment/localTime"
	"crdx.org/oh/internal/app/segment/modeToggle"
	"crdx.org/oh/internal/app/segment/pathGrants"
	"crdx.org/oh/internal/app/segment/scrollOverflow"
	"crdx.org/oh/internal/app/segment/sessionEmoji"
	"crdx.org/oh/internal/app/segment/sessionName"
	"crdx.org/oh/internal/app/segment/sessionSpend"
	"crdx.org/oh/internal/app/segment/subUsage"
	"crdx.org/oh/internal/app/segment/turnCount"
	"crdx.org/oh/internal/app/segment/turnTimer"
	"crdx.org/oh/internal/app/segment/workspaceDir"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/turn"
	"crdx.org/oh/internal/app/usage"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/jobs"
	"crdx.org/oh/internal/money"
	"crdx.org/oh/pkg/agent"
)

const (
	activitySpinnerSegment = "activity-spinner"
	cacheUsageSegment      = "cache-usage"
	contextUsageSegment    = "context-usage"
	modeToggleSegment      = "mode-toggle"
	pathGrantsSegment      = "path-grants"
	exposedPortsSegment    = "exposed-ports"
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
	sessionSpendSegment    = "session-spend"
	subUsageSegment        = "subscription-usage"
	jobNamesSegment        = "jobs"
)

type Options struct {
	Workspace             *work.Space
	Session               cycle.Session
	ModelEffortLevels     []string
	IsFast                bool
	IsSimulated           bool
	UsageReporter         agent.UsageReporter
	UsageCachePath        string
	UsageIsSelfRefreshing bool
	UsageGauges           *usage.Gauges
	Currency              money.Currency
	SandboxHostname       string
	Sources               Sources
}

type Sources struct {
	IsTurnRunning         func() bool
	IsSessionPersisted    func() bool
	GetContextUsage       func() (int, int)
	GetCacheUsage         func() (int, int)
	GetSessionSpend       func() (float64, bool)
	GetGrantedCaps        func() caps.Set
	GetPathGrants         func() []pathgrant.Grant
	GetHostToSandboxPorts func() []uint16
	GetSandboxToHostPorts func() []uint16
	IsPrefixPending       func() bool
	GetTurnTiming         func() turn.Timing
	GetTurnCount          func() int
	GetJobs               func() []jobs.Snapshot
}

func NewRegistry(options Options) segment.Registry {
	return segment.Registry{
		activitySpinnerSegment: activitySpinner.New(options.Sources.IsTurnRunning, time.Now),
		cacheUsageSegment:      cacheUsage.New(options.Sources.GetCacheUsage),
		contextUsageSegment:    contextUsage.New(options.Sources.GetContextUsage),
		sessionSpendSegment:    sessionSpend.New(options.Sources.GetSessionSpend, options.Currency),
		modeToggleSegment:      modeToggle.New(options.Sources.GetGrantedCaps, options.Sources.IsPrefixPending),
		pathGrantsSegment:      pathGrants.New(options.Sources.GetPathGrants),
		exposedPortsSegment: exposedPorts.New(
			exposedPorts.Routes{
				GetPorts: options.Sources.GetHostToSandboxPorts,
				Hostname: options.SandboxHostname,
			},
			exposedPorts.Routes{
				GetPorts: options.Sources.GetSandboxToHostPorts,
				Hostname: portgrant.LocalHost,
			},
		),
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
		sessionEmojiSegment: sessionEmoji.New(options.Session.Name),
		localTimeSegment:    localTime.New(time.Now),
		turnTimerSegment:    turnTimer.New(options.Sources.GetTurnTiming, options.Sources.IsTurnRunning),
		turnCountSegment:    turnCount.New(options.Sources.GetTurnCount),
		gitBranchSegment:    gitBranch.New(options.Workspace.GetDir()),
		jobNamesSegment:     jobNames.New(options.Sources.GetJobs, time.Now),
		subUsageSegment: subUsage.New(subUsage.Settings{
			Reporter:         options.UsageReporter,
			CachePath:        options.UsageCachePath,
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

func render(layout segment.Layout, position segment.Position, context segment.Context, cells int) string {
	drawnSegments := make([]string, 0, len(layout[position]))
	usedCells := 0

	for _, instance := range layout[position] {
		instance = underlying(instance)
		separatorCells := 0
		if len(drawnSegments) > 0 {
			separatorCells = style.Width(segmentSeparator())
		}

		var text string
		if fitter, isFitter := instance.(segment.Fitter); isFitter && cells >= 0 {
			text = fitter.RenderWithin(context, max(cells-usedCells-separatorCells, 0))
		} else {
			text = instance.Render(context)
		}
		textCells := style.Width(text)
		if textCells == 0 {
			continue
		}
		if cells >= 0 && usedCells+separatorCells+textCells > cells {
			break
		}

		drawnSegments = append(drawnSegments, text)
		usedCells += separatorCells + textCells
	}

	return strings.Join(drawnSegments, segmentSeparator())
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

func (self *Config) RenderWithin(position segment.Position, context segment.Context, cells int) string {
	return RenderWithin(self.layout, position, context, cells)
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
