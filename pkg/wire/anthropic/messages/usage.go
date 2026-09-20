package messages

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"crdx.org/oh/internal/req"
	"crdx.org/oh/pkg/agent"
)

const (
	turnSuffix  = "/v1/messages"
	usageSuffix = "/api/oauth/usage"
)

const (
	sessionWindow = 5 * time.Hour
	weeklyWindow  = 7 * 24 * time.Hour
)

const (
	unifiedLimitHeader = "Anthropic-Ratelimit-Unified"
	limitResetSuffix   = "-Reset"
	limitStatusSuffix  = "-Status"
	utilisationSuffix  = "-Utili" + "zation"
	limitRejected      = "rejected"
)

type usageLimit struct {
	Group    string  `json:"group"`
	Percent  float64 `json:"percent"`
	ResetsAt string  `json:"resets_at"`
	Scope    *struct {
		Model *struct {
			DisplayName *string `json:"display_name"`
		} `json:"model"`
	} `json:"scope"`
}

func (self *Client) IsAvailable() bool {
	_, isAvailable := usageAddress(self.URL)

	return isAvailable
}

func (self *Client) UsageWindows(ctx context.Context) ([]agent.UsageWindow, error) {
	probe, err := self.ProbeUsage(ctx)

	return probe.Windows, err
}

func (self *Client) ProbeUsage(ctx context.Context) (agent.UsageProbe, error) {
	address, isAvailable := usageAddress(self.URL)
	if !isAvailable {
		return agent.UsageProbe{}, nil
	}

	token, err := self.tokens.Token()
	if err != nil {
		return agent.UsageProbe{}, err
	}

	var payload struct {
		Limits []usageLimit `json:"limits"`
	}

	header, err := self.observedRequests().GetWithHeaders(ctx, address, self.headers(token), &payload)
	if err != nil {
		return agent.UsageProbe{}, err
	}

	windows := usageWindows(payload.Limits)

	return agent.UsageProbe{
		Windows:      windows,
		Availability: probedUsageAvailability(windows),
		RefreshAfter: req.CacheLifetime(header),
	}, nil
}

func usageWindows(limits []usageLimit) []agent.UsageWindow {
	var unscopedWindows, scopedWindows []agent.UsageWindow

	for _, limit := range limits {
		resetsAt, err := time.Parse(time.RFC3339, limit.ResetsAt)
		if err != nil {
			continue
		}

		scopeName := ""
		if limit.Scope != nil && limit.Scope.Model != nil && limit.Scope.Model.DisplayName != nil {
			scopeName = strings.ToLower(*limit.Scope.Model.DisplayName)
		}

		switch {
		case limit.Group == "session":
			unscopedWindows = append(unscopedWindows, agent.UsageWindow{
				Duration: sessionWindow,
				Percent:  limit.Percent,
				ResetsAt: resetsAt,
			})

		case limit.Group == "weekly" && limit.Scope == nil:
			unscopedWindows = append(unscopedWindows, agent.UsageWindow{
				Duration: weeklyWindow,
				Percent:  limit.Percent,
				ResetsAt: resetsAt,
			})

		case limit.Group == "weekly" && scopeName != "":
			scopedWindows = append(scopedWindows, agent.UsageWindow{
				Duration: weeklyWindow,
				Percent:  limit.Percent,
				ResetsAt: resetsAt,
				Scope:    scopeName,
			})
		}
	}

	return append(unscopedWindows, scopedWindows...)
}

func probedUsageAvailability(windows []agent.UsageWindow) agent.UsageAvailability {
	if len(windows) == 0 {
		return agent.UsageAvailabilityUnknown
	}

	for _, window := range windows {
		if window.IsLimited {
			return agent.UsageAvailabilityLimited
		}
		if window.Percent >= 100 {
			return agent.UsageAvailabilityUnknown
		}
	}

	return agent.UsageAvailabilityAllowed
}

func responseUsageWindows(header http.Header) []agent.UsageWindow {
	var windows []agent.UsageWindow

	for _, reportedWindow := range []struct {
		suffix   string
		duration time.Duration
	}{
		{suffix: "-5h", duration: sessionWindow},
		{suffix: "-7d", duration: weeklyWindow},
	} {
		prefix := unifiedLimitHeader + reportedWindow.suffix
		utilisation, err := strconv.ParseFloat(header.Get(prefix+utilisationSuffix), 64)
		if err != nil {
			continue
		}

		resetSeconds, err := strconv.ParseInt(header.Get(prefix+limitResetSuffix), 10, 64)
		if err != nil || resetSeconds <= 0 {
			continue
		}

		windows = append(windows, agent.UsageWindow{
			Duration:  reportedWindow.duration,
			Percent:   utilisation * 100,
			ResetsAt:  time.Unix(resetSeconds, 0).UTC(),
			IsLimited: strings.EqualFold(header.Get(prefix+limitStatusSuffix), limitRejected),
		})
	}

	return windows
}

func isUsageLimited(header http.Header) bool {
	return strings.EqualFold(
		strings.TrimSpace(header.Get(unifiedLimitHeader+limitStatusSuffix)),
		limitRejected,
	)
}

func usageAddress(turnAddress string) (string, bool) {
	prefix, found := strings.CutSuffix(turnAddress, turnSuffix)
	if !found {
		return "", false
	}

	return prefix + usageSuffix, true
}
