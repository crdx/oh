package opencodego

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"crdx.org/io/internal/req"
	"crdx.org/io/pkg/agent"
)

const (
	EndpointURL      = "https://opencode.ai/zen/go/v1/chat/completions"
	UsageEndpointURL = "https://opencode.ai/zen/go/v1/usage"
)

const (
	rollingWindow = 5 * time.Hour
	weeklyWindow  = 7 * 24 * time.Hour
	monthlyWindow = 30 * 24 * time.Hour
)

const (
	usageOK             = "ok"
	usageLimitErrorCode = "GoUsageLimitError"
)

type usageLimit struct {
	Status   string  `json:"status"`
	Percent  float64 `json:"percent"`
	ResetsAt string  `json:"resetsAt"`
}

func (self *Client) Send(ctx context.Context, yield agent.Yield) (agent.Reply, error) {
	reply, err := self.Client.Send(ctx, yield)
	window, isLimited := refusedUsageWindow(err, time.Now())
	if !isLimited {
		return reply, err
	}

	return reply, &agent.UsageLimitError{Cause: err, Windows: []agent.UsageWindow{window}}
}

func refusedUsageWindow(err error, now time.Time) (agent.UsageWindow, bool) {
	refusal, isRefusal := errors.AsType[*req.StatusError](err)
	if !isRefusal || refusal.Code != usageLimitErrorCode {
		return agent.UsageWindow{}, false
	}

	var payload struct {
		Metadata struct {
			LimitName string `json:"limitName"`
		} `json:"metadata"`
	}
	if json.Unmarshal([]byte(refusal.Body), &payload) != nil {
		return agent.UsageWindow{}, false
	}

	var duration time.Duration
	switch strings.ToLower(payload.Metadata.LimitName) {
	case "rolling":
		duration = rollingWindow
	case "weekly":
		duration = weeklyWindow
	case "monthly":
		duration = monthlyWindow
	default:
		return agent.UsageWindow{}, false
	}

	resetsAt := time.Time{}
	if wait := refusal.RetryAfter(); wait > 0 {
		resetsAt = now.Add(wait)
	}

	return agent.UsageWindow{
		Duration:  duration,
		Percent:   100,
		ResetsAt:  resetsAt,
		IsLimited: true,
	}, true
}

func (self *Client) IsAvailable() bool {
	return self.UsageURL != ""
}

func (self *Client) UsageWindows(ctx context.Context) ([]agent.UsageWindow, error) {
	probe, err := self.ProbeUsage(ctx)

	return probe.Windows, err
}

func (self *Client) ProbeUsage(ctx context.Context) (agent.UsageProbe, error) {
	if self.UsageURL == "" {
		return agent.UsageProbe{}, nil
	}

	var payload struct {
		Usage struct {
			RollingWindow usageLimit `json:"rolling"`
			Weekly        usageLimit `json:"weekly"`
			Monthly       usageLimit `json:"monthly"`
		} `json:"usage"`
	}

	header := self.headers()
	header.Set("Accept", "application/json")

	responseHeader, err := self.observedRequests().GetWithHeaders(ctx, self.UsageURL, header, &payload)
	if err != nil {
		return agent.UsageProbe{}, err
	}

	var windows []agent.UsageWindow

	for _, reportedWindow := range []struct {
		limit    usageLimit
		duration time.Duration
	}{
		{limit: payload.Usage.RollingWindow, duration: rollingWindow},
		{limit: payload.Usage.Weekly, duration: weeklyWindow},
		{limit: payload.Usage.Monthly, duration: monthlyWindow},
	} {
		if window, ok := usageWindow(reportedWindow.limit, reportedWindow.duration); ok {
			windows = append(windows, window)
		}
	}

	availability := agent.UsageAvailabilityUnknown
	if len(windows) > 0 {
		availability = agent.UsageAvailabilityAllowed
	}
	for _, window := range windows {
		if window.IsLimited {
			availability = agent.UsageAvailabilityLimited
			break
		}
	}

	return agent.UsageProbe{
		Windows:      windows,
		Availability: availability,
		RefreshAfter: req.CacheLifetime(responseHeader),
	}, nil
}

func usageWindow(limit usageLimit, duration time.Duration) (agent.UsageWindow, bool) {
	if limit.Status == "" {
		return agent.UsageWindow{}, false
	}

	window := agent.UsageWindow{
		Duration:  duration,
		Percent:   limit.Percent,
		IsLimited: limit.Status != usageOK,
	}

	if resetsAt, err := time.Parse(time.RFC3339, limit.ResetsAt); err == nil {
		window.ResetsAt = resetsAt
	}

	return window, true
}
