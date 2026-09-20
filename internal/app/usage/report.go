package usage

import (
	"cmp"
	"slices"
	"time"

	"crdx.org/oh/pkg/agent"
)

const SchemaVersion = 1

const (
	StatusOK          = "ok"
	StatusUnavailable = "unavailable"
	StatusFailed      = "failed"
)

const (
	SeverityNormal  = "normal"
	SeverityWarning = "warning"
	SeverityError   = "error"
)

const (
	FreshnessFresh   = "fresh"
	FreshnessDue     = "due"
	FreshnessStale   = "stale"
	FreshnessWaiting = "waiting"
)

const (
	StateActive  = "active"
	StateIdle    = "idle"
	StateStale   = "stale"
	StateUnknown = "unknown"
	StateLimited = "limited"
)

const (
	freshBuffer = time.Minute
	staleFactor = 6
)

const unavailableMessage = "no usage data available"

type Report struct {
	SchemaVersion int        `json:"schema_version"`
	Providers     []Snapshot `json:"providers"`
}

type Provider struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type Snapshot struct {
	Provider            Provider `json:"provider"`
	Status              string   `json:"status"`
	Message             string   `json:"message,omitempty"`
	MeasuredAt          string   `json:"measured_at,omitempty"`
	AgeSeconds          *int     `json:"age_seconds"`
	RefreshAfterSeconds int      `json:"refresh_after_seconds"`
	FreshWithinSeconds  int      `json:"fresh_within_seconds"`
	StaleAfterSeconds   int      `json:"stale_after_seconds"`
	IsSelfRefreshing    bool     `json:"is_self_refreshing"`
	Freshness           string   `json:"freshness"`
	Limits              []Limit  `json:"limits"`
}

type Limit struct {
	ID               string  `json:"id"`
	Label            string  `json:"label"`
	ShortLabel       string  `json:"short_label"`
	Scope            string  `json:"scope,omitempty"`
	WindowSeconds    int     `json:"window_seconds"`
	UsedPercent      int     `json:"used_percent"`
	ExpectedPercent  *int    `json:"expected_percent"`
	PaceDelta        *int    `json:"pace_delta"`
	ResetsAt         *string `json:"resets_at"`
	RemainingSeconds *int    `json:"remaining_seconds"`
	IsActive         bool    `json:"is_active"`
	IsLimited        bool    `json:"is_limited,omitempty"`
	State            string  `json:"state"`
	Severity         string  `json:"severity"`
}

func (self Snapshot) MeasuredTime() (time.Time, bool) {
	if self.MeasuredAt == "" {
		return time.Time{}, false
	}

	measuredAt, err := time.Parse(time.RFC3339, self.MeasuredAt)
	if err != nil {
		return time.Time{}, false
	}

	return measuredAt, true
}

func (self Limit) ResetTime() (time.Time, bool) {
	if self.ResetsAt == nil {
		return time.Time{}, false
	}

	resetsAt, err := time.Parse(time.RFC3339, *self.ResetsAt)
	if err != nil {
		return time.Time{}, false
	}

	return resetsAt, true
}

func (self Snapshot) FreshnessAt(age time.Duration) string {
	return FreshnessWithin(
		age,
		time.Duration(self.FreshWithinSeconds)*time.Second,
		time.Duration(self.StaleAfterSeconds)*time.Second,
		self.IsSelfRefreshing,
	)
}

func FreshnessWithin(
	age time.Duration, freshWithin time.Duration, staleAfter time.Duration, isSelfRefreshing bool,
) string {
	switch {
	case freshWithin <= 0 || age <= freshWithin:
		return FreshnessFresh
	case age <= staleAfter:
		return FreshnessDue
	case isSelfRefreshing:
		return FreshnessStale
	default:
		return FreshnessWaiting
	}
}

func FreshWithin(refreshAfter time.Duration) time.Duration {
	return refreshAfter + freshBuffer
}

func StaleAfter(refreshAfter time.Duration) time.Duration {
	return staleFactor * refreshAfter
}

func (self Limit) StateAt(now time.Time) string {
	if resetsAt, hasReset := self.ResetTime(); hasReset && !resetsAt.After(now) {
		return StateStale
	}

	switch {
	case self.IsLimited:
		return StateLimited
	case !self.IsActive && self.UsedPercent == 0:
		return StateIdle
	case !self.IsActive:
		return StateUnknown
	default:
		return StateActive
	}
}

func (self Limit) IsSessionLimit() bool {
	return self.ShortLabel == DurationLabel(sessionLength)
}

func buildSnapshot(
	source Source, outcome result, refreshAfter time.Duration, now time.Time,
) Snapshot {
	snapshot := Snapshot{
		Provider:            Provider{ID: source.Provider, Label: source.Label},
		Status:              StatusOK,
		RefreshAfterSeconds: int(refreshAfter.Seconds()),
		FreshWithinSeconds:  int((refreshAfter + freshBuffer).Seconds()),
		StaleAfterSeconds:   int((staleFactor * refreshAfter).Seconds()),
		IsSelfRefreshing:    source.IsSelfRefreshing,
		Freshness:           FreshnessFresh,
		Limits:              []Limit{},
	}

	if !outcome.measuredAt.IsZero() {
		age := max(0, now.Sub(outcome.measuredAt))
		ageSeconds := int(age.Seconds())

		snapshot.MeasuredAt = outcome.measuredAt.UTC().Format(time.RFC3339)
		snapshot.AgeSeconds = &ageSeconds
		snapshot.Freshness = snapshot.FreshnessAt(age)
	}

	snapshot.Limits = buildLimits(outcome.windows, source, outcome.measuredAt, now)

	switch {
	case outcome.failure != "":
		snapshot.Status = StatusFailed
		snapshot.Message = outcome.failure
	case len(snapshot.Limits) == 0:
		snapshot.Status = StatusUnavailable
		snapshot.Message = unavailableMessage
	}

	return snapshot
}

func buildLimits(
	windows []agent.UsageWindow, source Source, measuredAt time.Time, now time.Time,
) []Limit {
	windows = slices.Clone(windows)

	slices.SortStableFunc(windows, func(left agent.UsageWindow, right agent.UsageWindow) int {
		return cmp.Or(
			cmp.Compare(scopeOrder(left), scopeOrder(right)),
			cmp.Compare(left.Scope, right.Scope),
			cmp.Compare(left.Duration, right.Duration),
		)
	})

	limits := make([]Limit, 0, len(windows))
	for _, window := range windows {
		if isUnspentScope(window) {
			continue
		}

		limits = append(limits, buildLimit(window, measuredAt, now))
	}

	if source.HasIdleSessionWindow && len(limits) > 0 && !hasSessionLimit(limits) {
		limits = append([]Limit{idleSessionLimit()}, limits...)
	}

	return limits
}

func buildLimit(window agent.UsageWindow, measuredAt time.Time, now time.Time) Limit {
	limit := Limit{
		ID:            limitID(window),
		Label:         WindowLabel(window),
		ShortLabel:    shortLabel(window),
		Scope:         window.Scope,
		WindowSeconds: int(window.Duration.Seconds()),
		UsedPercent:   int(window.Percent + 0.5),
		IsActive:      !window.ResetsAt.IsZero(),
		State:         StateActive,
		Severity:      SeverityNormal,
	}

	if limit.IsActive {
		resetsAt := window.ResetsAt.UTC().Format(time.RFC3339)
		limit.ResetsAt = &resetsAt

		remainingSeconds := max(0, int(window.ResetsAt.Sub(now).Seconds()))
		limit.RemainingSeconds = &remainingSeconds

		expectedPercent := ExpectedPercent(window, measuredAt)
		paceDelta := limit.UsedPercent - expectedPercent

		limit.ExpectedPercent = &expectedPercent
		limit.PaceDelta = &paceDelta
		limit.Severity = severity(ClassifyPace(limit.UsedPercent, expectedPercent))
	}

	limit.IsLimited = window.IsLimited
	limit.State = limit.StateAt(now)
	if limit.State == StateLimited {
		limit.Severity = SeverityError
	}

	return limit
}

func idleSessionLimit() Limit {
	window := agent.UsageWindow{Duration: sessionLength}

	return Limit{
		ID:            limitID(window),
		Label:         WindowLabel(window),
		ShortLabel:    shortLabel(window),
		WindowSeconds: int(window.Duration.Seconds()),
		State:         StateIdle,
		Severity:      SeverityNormal,
	}
}

func hasSessionLimit(limits []Limit) bool {
	return slices.ContainsFunc(limits, func(candidate Limit) bool {
		return candidate.IsSessionLimit()
	})
}

func limitID(window agent.UsageWindow) string {
	if window.Scope == "" {
		return DurationLabel(window.Duration)
	}

	return "scope/" + window.Scope + "/" + DurationLabel(window.Duration)
}

func shortLabel(window agent.UsageWindow) string {
	if window.Scope == "" {
		return ShortWindowLabel(window.Duration)
	}

	return ScopeLabel(window.Scope) + " " + ShortWindowLabel(window.Duration)
}

func isUnspentScope(window agent.UsageWindow) bool {
	return window.Scope != "" && window.Percent < 1
}

func scopeOrder(window agent.UsageWindow) int {
	if window.Scope == "" {
		return 0
	}

	return 1
}

func severity(pace Pace) string {
	switch pace {
	case PaceAhead:
		return SeverityWarning
	case PaceCritical:
		return SeverityError
	case PaceEven:
	}

	return SeverityNormal
}
