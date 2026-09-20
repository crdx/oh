package responses

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"crdx.org/oh/internal/req"
	"crdx.org/oh/pkg/agent"
)

const (
	turnUsageSuffix  = "/codex/responses"
	accountUsagePath = "/wham/usage"
)

const (
	headerPrefix         = "X-Codex"
	activeLimitHeader    = headerPrefix + "-Active-Limit"
	activeLimitNamespace = "codex_"
	usedSuffix           = "-Used-Percent"
	windowSuffix         = "-Window-Minutes"
	resetAtSuffix        = "-Reset-At"
	resetAfterSuffix     = "-Reset-After-Seconds"
	limitNameSuffix      = "-Limit-Name"

	primaryPart   = "-Primary"
	secondaryPart = "-Secondary"
)

type limitBucket struct {
	prefix   string
	scope    string
	isActive bool
}

type accountUsageLimit struct {
	IsAllowed       *bool               `json:"allowed"`
	PrimaryWindow   *accountUsageWindow `json:"primary_window"`
	SecondaryWindow *accountUsageWindow `json:"secondary_window"`
}

type accountUsageWindow struct {
	UsedPercent       float64 `json:"used_percent"`
	WindowSeconds     int     `json:"limit_window_seconds"`
	ResetAfterSeconds int     `json:"reset_after_seconds"`
	ResetAt           int64   `json:"reset_at"`
}

type additionalAccountUsage struct {
	LimitName       string            `json:"limit_name"`
	NormalModelSlug string            `json:"normal_model_slug"`
	RateLimit       accountUsageLimit `json:"rate_limit"`
}

func (self *Client) ProbeUsage(ctx context.Context) (agent.UsageProbe, error) {
	address, isAvailable := accountUsageAddress(self.URL)
	if !isAvailable {
		return agent.UsageProbe{}, nil
	}

	token, err := self.tokens.Token()
	if err != nil {
		return agent.UsageProbe{}, err
	}

	var payload struct {
		RateLimit            accountUsageLimit        `json:"rate_limit"`
		AdditionalRateLimits []additionalAccountUsage `json:"additional_rate_limits"`
	}

	requestHeader := self.headers(token)
	requestHeader.Del("Openai-Beta")
	requestHeader.Del("Session_id")
	header, err := self.observedRequests().GetWithHeaders(ctx, address, requestHeader, &payload)
	if err != nil {
		return agent.UsageProbe{}, err
	}

	windows := accountUsageWindows(payload.RateLimit, "")
	for _, additionalLimit := range payload.AdditionalRateLimits {
		scope := additionalLimit.NormalModelSlug
		if scope == "" {
			scope = additionalLimit.LimitName
		}
		windows = append(windows, accountUsageWindows(additionalLimit.RateLimit, strings.ToLower(scope))...)
	}

	return agent.UsageProbe{
		Windows:      windows,
		Availability: usageAvailability(payload.RateLimit.IsAllowed),
		RefreshAfter: req.CacheLifetime(header),
	}, nil
}

func accountUsageWindows(limit accountUsageLimit, scope string) []agent.UsageWindow {
	var windows []agent.UsageWindow
	for _, reportedWindow := range []*accountUsageWindow{limit.PrimaryWindow, limit.SecondaryWindow} {
		if reportedWindow == nil || reportedWindow.WindowSeconds <= 0 {
			continue
		}

		resetsAt := time.Time{}
		if reportedWindow.ResetAt > 0 {
			resetsAt = time.Unix(reportedWindow.ResetAt, 0).UTC()
		} else if reportedWindow.ResetAfterSeconds > 0 {
			resetsAt = time.Now().Add(time.Duration(reportedWindow.ResetAfterSeconds) * time.Second)
		}

		windows = append(windows, agent.UsageWindow{
			Duration:  time.Duration(reportedWindow.WindowSeconds) * time.Second,
			Percent:   reportedWindow.UsedPercent,
			ResetsAt:  resetsAt,
			Scope:     scope,
			IsLimited: limit.IsAllowed != nil && !*limit.IsAllowed,
		})
	}

	return windows
}

func usageAvailability(isAllowed *bool) agent.UsageAvailability {
	switch {
	case isAllowed == nil:
		return agent.UsageAvailabilityUnknown
	case *isAllowed:
		return agent.UsageAvailabilityAllowed
	default:
		return agent.UsageAvailabilityLimited
	}
}

func accountUsageAddress(turnAddress string) (string, bool) {
	prefix, found := strings.CutSuffix(turnAddress, turnUsageSuffix)
	if !found {
		return "", false
	}

	return prefix + accountUsagePath, true
}

func (self *Client) IsAvailable() bool {
	if _, isAvailable := accountUsageAddress(self.URL); isAvailable {
		return true
	}

	self.usageMutex.Lock()
	defer self.usageMutex.Unlock()

	return self.usageWindows != nil
}

func (self *Client) UsageWindows(ctx context.Context) ([]agent.UsageWindow, error) {
	if windows := self.reportedUsageWindows(); windows != nil {
		return windows, nil
	}

	probe, err := self.ProbeUsage(ctx)

	return probe.Windows, err
}

func (self *Client) reportedUsageWindows() []agent.UsageWindow {
	self.usageMutex.Lock()
	defer self.usageMutex.Unlock()

	return slices.Clone(self.usageWindows)
}

func (self *Client) recordUsageWindows(
	header http.Header, now time.Time, isLimited bool,
) []agent.UsageWindow {
	windows := usageWindows(header, now, isLimited)
	if windows == nil {
		return nil
	}

	self.usageMutex.Lock()
	defer self.usageMutex.Unlock()

	self.usageWindows = windows

	return slices.Clone(windows)
}

func usageWindows(header http.Header, now time.Time, isLimited bool) []agent.UsageWindow {
	var windows []agent.UsageWindow

	for _, bucket := range limitBuckets(header) {
		for _, part := range []string{primaryPart, secondaryPart} {
			if window, ok := usageWindow(header, bucket, part, now); ok {
				window.IsLimited = isLimited && bucket.isActive
				windows = append(windows, window)
			}
		}
	}

	return windows
}

func limitBuckets(header http.Header) []limitBucket {
	activeLimit := activeLimitName(header)
	var namedBuckets []string

	for name := range header {
		prefix, found := strings.CutSuffix(http.CanonicalHeaderKey(name), primaryPart+usedSuffix)
		if found && strings.HasPrefix(prefix, headerPrefix+"-") {
			namedBuckets = append(namedBuckets, prefix)
		}
	}

	slices.Sort(namedBuckets)

	var buckets []limitBucket

	if !activeLimitNamesBucket(activeLimit, namedBuckets) {
		buckets = append(buckets, limitBucket{prefix: headerPrefix, isActive: true})
	}

	for _, prefix := range namedBuckets {
		buckets = append(buckets, limitBucket{
			prefix:   prefix,
			scope:    scopeName(header, prefix),
			isActive: strings.EqualFold(activeLimit, bucketName(prefix)),
		})
	}

	return buckets
}

func activeLimitName(header http.Header) string {
	activeLimit := strings.ToLower(strings.TrimSpace(header.Get(activeLimitHeader)))

	return strings.TrimPrefix(activeLimit, activeLimitNamespace)
}

func activeLimitNamesBucket(activeLimit string, prefixes []string) bool {
	return slices.ContainsFunc(prefixes, func(prefix string) bool {
		return strings.EqualFold(activeLimit, bucketName(prefix))
	})
}

func bucketName(prefix string) string {
	return strings.TrimPrefix(prefix, headerPrefix+"-")
}

func scopeName(header http.Header, prefix string) string {
	if name := strings.TrimSpace(header.Get(prefix + limitNameSuffix)); name != "" {
		return strings.ToLower(name)
	}

	return strings.ToLower(strings.TrimPrefix(prefix, headerPrefix+"-"))
}

func usageWindow(
	header http.Header, bucket limitBucket, part string, now time.Time,
) (agent.UsageWindow, bool) {
	usedPercentage, err := strconv.ParseFloat(header.Get(bucket.prefix+part+usedSuffix), 64)
	if err != nil {
		return agent.UsageWindow{}, false
	}

	minutes, err := strconv.Atoi(header.Get(bucket.prefix + part + windowSuffix))
	if err != nil || minutes <= 0 {
		return agent.UsageWindow{}, false
	}

	return agent.UsageWindow{
		Duration: time.Duration(minutes) * time.Minute,
		Percent:  usedPercentage,
		ResetsAt: resetTime(header, bucket.prefix+part, now),
		Scope:    bucket.scope,
	}, true
}

func resetTime(header http.Header, prefix string, now time.Time) time.Time {
	if at, err := strconv.ParseInt(header.Get(prefix+resetAtSuffix), 10, 64); err == nil && at > 0 {
		return time.Unix(at, 0).UTC()
	}

	seconds, err := strconv.Atoi(header.Get(prefix + resetAfterSuffix))
	if err != nil || seconds <= 0 {
		return time.Time{}
	}

	return now.Add(time.Duration(seconds) * time.Second)
}
